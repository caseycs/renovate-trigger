package awsecr

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/ilia/renovate-trigger/internal/graph"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

const testQueueURL = "https://sqs.us-west-2.amazonaws.com/123456789012/renovate-trigger-ecr-image-push"

// fakeQueue serves a scripted sequence of receive results and records deletes.
// Each receive consumes the next scripted entry; once exhausted it returns no
// messages, so Run cannot spin on a stale script.
type fakeQueue struct {
	mu         sync.Mutex
	receives   []receiveResult
	calls      int
	deleted    []string
	deleteErr  error
	lastInput  sqs.ReceiveMessageInput
	attrsErr   error
	attrsCalls int
}

type receiveResult struct {
	messages []sqstypes.Message
	err      error
}

func (q *fakeQueue) ReceiveMessage(_ context.Context, in *sqs.ReceiveMessageInput, _ ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.lastInput = *in
	i := q.calls
	q.calls++
	if i >= len(q.receives) {
		return &sqs.ReceiveMessageOutput{}, nil
	}
	r := q.receives[i]
	if r.err != nil {
		return nil, r.err
	}
	return &sqs.ReceiveMessageOutput{Messages: r.messages}, nil
}

func (q *fakeQueue) DeleteMessage(_ context.Context, in *sqs.DeleteMessageInput, _ ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.deleteErr != nil {
		return nil, q.deleteErr
	}
	q.deleted = append(q.deleted, aws.ToString(in.ReceiptHandle))
	return &sqs.DeleteMessageOutput{}, nil
}

func (q *fakeQueue) GetQueueAttributes(_ context.Context, _ *sqs.GetQueueAttributesInput, _ ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.attrsCalls++
	if q.attrsErr != nil {
		return nil, q.attrsErr
	}
	return &sqs.GetQueueAttributesOutput{}, nil
}

func (q *fakeQueue) deletedHandles() []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]string(nil), q.deleted...)
}

type fakeBatch struct {
	mu    sync.Mutex
	added []graph.Dependency
}

func (b *fakeBatch) Add(dep graph.Dependency) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.added = append(b.added, dep)
}

func (b *fakeBatch) addedDeps() []graph.Dependency {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]graph.Dependency(nil), b.added...)
}

func message(handle string, body []byte) sqstypes.Message {
	return sqstypes.Message{ReceiptHandle: aws.String(handle), Body: aws.String(string(body))}
}

func TestPollOnceAddsPushedRepositoryAndDeletes(t *testing.T) {
	q := &fakeQueue{receives: []receiveResult{{
		messages: []sqstypes.Message{message("h1", pushEvent(t, nil))},
	}}}
	b := &fakeBatch{}

	if err := NewConsumer(q, testQueueURL, b, testLogger()).pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce error: %v", err)
	}

	added := b.addedDeps()
	want := graph.Dependency{
		Kind: graph.KindECR,
		Name: "arn:aws:ecr:us-west-2:123456789012:repository/my-repository-name",
	}
	if len(added) != 1 || added[0] != want {
		t.Fatalf("added = %v, want [%v]", added, want)
	}
	if h := q.deletedHandles(); len(h) != 1 || h[0] != "h1" {
		t.Errorf("deleted = %v, want [h1]", h)
	}
}

// A well-formed event we do not act on is acknowledged, so it does not burn
// receive attempts and eventually land in the dead-letter queue.
func TestPollOnceDeletesFilteredEventWithoutAdding(t *testing.T) {
	body := pushEvent(t, func(e *imageActionEvent) { e.Detail.ActionType = "DELETE" })
	q := &fakeQueue{receives: []receiveResult{{
		messages: []sqstypes.Message{message("h1", body)},
	}}}
	b := &fakeBatch{}

	if err := NewConsumer(q, testQueueURL, b, testLogger()).pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce error: %v", err)
	}

	if added := b.addedDeps(); len(added) != 0 {
		t.Errorf("added = %v, want empty", added)
	}
	if h := q.deletedHandles(); len(h) != 1 {
		t.Errorf("deleted = %v, want the filtered message acknowledged", h)
	}
}

// An unparseable body is deliberately left on the queue: it will never become
// parseable, so the redrive policy should surface it in the dead-letter queue
// rather than have us hide it by deleting.
func TestPollOnceLeavesUnparseableMessageForTheDeadLetterQueue(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	q := &fakeQueue{receives: []receiveResult{{
		messages: []sqstypes.Message{message("h1", []byte("{not json"))},
	}}}
	b := &fakeBatch{}

	if err := NewConsumer(q, testQueueURL, b, logger).pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce error: %v", err)
	}

	if added := b.addedDeps(); len(added) != 0 {
		t.Errorf("added = %v, want empty", added)
	}
	if h := q.deletedHandles(); len(h) != 0 {
		t.Errorf("deleted = %v, want the message left on the queue", h)
	}
	if !strings.Contains(buf.String(), "failed to parse ecr event") {
		t.Errorf("expected a parse warning, got: %s", buf.String())
	}
}

func TestPollOnceUsesLongPollParameters(t *testing.T) {
	q := &fakeQueue{}

	if err := NewConsumer(q, testQueueURL, &fakeBatch{}, testLogger()).pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce error: %v", err)
	}

	in := q.lastInput
	if aws.ToString(in.QueueUrl) != testQueueURL {
		t.Errorf("QueueUrl = %q, want %q", aws.ToString(in.QueueUrl), testQueueURL)
	}
	if in.WaitTimeSeconds != receiveWaitSeconds {
		t.Errorf("WaitTimeSeconds = %d, want %d", in.WaitTimeSeconds, receiveWaitSeconds)
	}
	if in.MaxNumberOfMessages != maxMessages {
		t.Errorf("MaxNumberOfMessages = %d, want %d", in.MaxNumberOfMessages, maxMessages)
	}
	if in.VisibilityTimeout != visibilityTimeout {
		t.Errorf("VisibilityTimeout = %d, want %d", in.VisibilityTimeout, visibilityTimeout)
	}
}

// A multi-tag push emits one event per tag. Both are added as equal Dependencies;
// collapsing them is the Batch's job, and this test documents that boundary.
func TestPollOnceMultiTagPushYieldsEqualDependencies(t *testing.T) {
	v1 := pushEvent(t, func(e *imageActionEvent) { e.Detail.ImageTag = "v1.2.3" })
	latest := pushEvent(t, func(e *imageActionEvent) { e.Detail.ImageTag = "latest" })
	q := &fakeQueue{receives: []receiveResult{{
		messages: []sqstypes.Message{message("h1", v1), message("h2", latest)},
	}}}
	b := &fakeBatch{}

	if err := NewConsumer(q, testQueueURL, b, testLogger()).pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce error: %v", err)
	}

	added := b.addedDeps()
	if len(added) != 2 {
		t.Fatalf("added = %v, want two entries", added)
	}
	if added[0] != added[1] {
		t.Errorf("added = %v, want two equal Dependencies", added)
	}
}

func TestPollOnceReturnsReceiveError(t *testing.T) {
	q := &fakeQueue{receives: []receiveResult{{err: errors.New("throttled")}}}

	if err := NewConsumer(q, testQueueURL, &fakeBatch{}, testLogger()).pollOnce(context.Background()); err == nil {
		t.Error("expected the receive error to propagate")
	}
}

// A delete failure is not fatal: the Dependency is already batched, and the
// redelivered message dedupes.
func TestPollOnceDeleteFailureIsNonFatal(t *testing.T) {
	q := &fakeQueue{
		receives:  []receiveResult{{messages: []sqstypes.Message{message("h1", pushEvent(t, nil))}}},
		deleteErr: errors.New("timeout"),
	}
	b := &fakeBatch{}

	if err := NewConsumer(q, testQueueURL, b, testLogger()).pollOnce(context.Background()); err != nil {
		t.Fatalf("pollOnce error: %v", err)
	}
	if added := b.addedDeps(); len(added) != 1 {
		t.Errorf("added = %v, want the dependency batched despite the delete failure", added)
	}
}

func TestRunBacksOffAndRecovers(t *testing.T) {
	q := &fakeQueue{receives: []receiveResult{
		{err: errors.New("throttled")},
		{err: errors.New("throttled")},
		{messages: []sqstypes.Message{message("h1", pushEvent(t, nil))}},
	}}
	b := &fakeBatch{}

	c := NewConsumer(q, testQueueURL, b, testLogger())
	c.backoffMin = time.Millisecond
	c.backoffMax = 2 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Run(ctx)
	}()

	deadline := time.After(2 * time.Second)
	for {
		if len(b.addedDeps()) == 1 {
			break
		}
		select {
		case <-deadline:
			cancel()
			<-done
			t.Fatal("timed out waiting for the consumer to recover after backoff")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	cancel()
	<-done
}

func TestRunWarnsAfterRepeatedReceiveFailures(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	receives := make([]receiveResult, receiveWarnThreshold)
	for i := range receives {
		receives[i] = receiveResult{err: errors.New("throttled")}
	}
	q := &fakeQueue{receives: receives}

	c := NewConsumer(q, testQueueURL, &fakeBatch{}, logger)
	c.backoffMin = time.Microsecond
	c.backoffMax = time.Microsecond

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	c.Run(ctx)

	if !strings.Contains(buf.String(), "repeated failures receiving ecr events") {
		t.Errorf("expected the repeated-failure warning, got: %s", buf.String())
	}
}

func TestRunReturnsOnContextCancel(t *testing.T) {
	q := &fakeQueue{}
	c := NewConsumer(q, testQueueURL, &fakeBatch{}, testLogger())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.Run(ctx)
	}()

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return promptly after cancellation")
	}
}

func TestVerify(t *testing.T) {
	q := &fakeQueue{}
	if err := NewConsumer(q, testQueueURL, &fakeBatch{}, testLogger()).Verify(context.Background()); err != nil {
		t.Fatalf("Verify error: %v", err)
	}
	if q.attrsCalls != 1 {
		t.Errorf("attrsCalls = %d, want 1", q.attrsCalls)
	}

	bad := &fakeQueue{attrsErr: errors.New("AccessDenied")}
	if err := NewConsumer(bad, testQueueURL, &fakeBatch{}, testLogger()).Verify(context.Background()); err == nil {
		t.Error("expected Verify to fail when the queue is unreachable")
	}
}
