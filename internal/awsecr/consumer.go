// Package awsecr ingests ECR image-push events from an SQS queue fed by an
// EventBridge rule, and reads a Dependency's Dependents from an ECR repository's
// AWS resource tags. It is the AWS half of the two things package webhook and
// package ghapp do for GitHub: an ingress that resolves nothing, and a reader of
// Dependents declarations.
package awsecr

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/ilia/renovate-trigger/internal/graph"
)

const (
	// receiveWaitSeconds is SQS's maximum long-poll wait. One consumer polling at
	// 20s makes roughly 4,300 calls a day, which stays inside the free tier;
	// anything shorter is a cost bug rather than a latency win.
	receiveWaitSeconds = 20
	// maxMessages is SQS's per-receive maximum.
	maxMessages = 10
	// visibilityTimeout is set explicitly so behaviour does not depend on the
	// queue's own configuration. Handling a message is parse-then-Add, so a short
	// lease is correct.
	visibilityTimeout = 30
	// receiveWarnThreshold is the number of consecutive receive failures after
	// which the consumer warns, mirroring the collector's postpone warning.
	receiveWarnThreshold = 10

	defaultBackoffMin = time.Second
	defaultBackoffMax = 30 * time.Second
)

// queueAPI is the subset of the SQS API the consumer uses. It is satisfied by
// *sqs.Client; tests supply a fake.
type queueAPI interface {
	ReceiveMessage(ctx context.Context, in *sqs.ReceiveMessageInput, optFns ...func(*sqs.Options)) (*sqs.ReceiveMessageOutput, error)
	DeleteMessage(ctx context.Context, in *sqs.DeleteMessageInput, optFns ...func(*sqs.Options)) (*sqs.DeleteMessageOutput, error)
	GetQueueAttributes(ctx context.Context, in *sqs.GetQueueAttributesInput, optFns ...func(*sqs.Options)) (*sqs.GetQueueAttributesOutput, error)
}

// DependencyAdder puts a Dependency into the current Batch. It is the consumer's
// only outward dependency, mirroring webhook.BatchAdder.
type DependencyAdder interface {
	Add(dep graph.Dependency)
}

// Consumer long-polls an SQS queue of ECR image-push events and adds each pushed
// repository to the Batch.
type Consumer struct {
	queue    queueAPI
	queueURL string
	batch    DependencyAdder
	logger   *slog.Logger

	// Backoff bounds are fields rather than constants so tests can drive the
	// retry path without waiting on real time.
	backoffMin time.Duration
	backoffMax time.Duration

	failures int
}

// NewConsumer returns a Consumer reading from queueURL.
func NewConsumer(api queueAPI, queueURL string, batch DependencyAdder, logger *slog.Logger) *Consumer {
	return &Consumer{
		queue:      api,
		queueURL:   queueURL,
		batch:      batch,
		logger:     logger,
		backoffMin: defaultBackoffMin,
		backoffMax: defaultBackoffMax,
	}
}

// Verify asserts the queue is reachable with our credentials so a wrong queue URL
// or a missing IAM policy crash-loops the pod at boot, rather than leaving it
// deployed and silently ingesting nothing. Mirrors k8s.JobCreator.Verify.
func (c *Consumer) Verify(ctx context.Context) error {
	_, err := c.queue.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(c.queueURL),
		AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameQueueArn},
	})
	if err != nil {
		return fmt.Errorf("reading attributes of queue %s: %w", c.queueURL, err)
	}
	return nil
}

// Run long-polls until ctx is cancelled. Receive failures are retried with capped
// exponential backoff indefinitely and never returned: a broken ECR path must not
// take GitHub ingestion down with it.
func (c *Consumer) Run(ctx context.Context) {
	c.logger.Info("ecr consumer started", "queue_url", c.queueURL)

	for {
		if ctx.Err() != nil {
			c.logger.Info("ecr consumer stopped")
			return
		}

		if err := c.pollOnce(ctx); err != nil {
			if ctx.Err() != nil {
				// The cancellation aborted an in-flight receive; not a failure.
				c.logger.Info("ecr consumer stopped")
				return
			}
			c.failures++
			wait := c.backoff()
			if c.failures == receiveWarnThreshold {
				c.logger.Warn("repeated failures receiving ecr events", "failures", c.failures, "error", err)
			} else {
				c.logger.Info("failed to receive ecr events, backing off", "error", err, "backoff", wait)
			}
			if !sleepCtx(ctx, wait) {
				c.logger.Info("ecr consumer stopped")
				return
			}
			continue
		}
		c.failures = 0
	}
}

// backoff returns the wait after the current number of consecutive failures,
// doubling from backoffMin and capped at backoffMax.
func (c *Consumer) backoff() time.Duration {
	wait := c.backoffMin
	for i := 1; i < c.failures && wait < c.backoffMax; i++ {
		wait *= 2
	}
	if wait > c.backoffMax {
		wait = c.backoffMax
	}
	return wait
}

// sleepCtx waits for d, reporting false if ctx was cancelled first.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// pollOnce performs one receive-and-handle cycle. It is the directly-callable seam
// the tests drive, so no test waits on a real long poll.
func (c *Consumer) pollOnce(ctx context.Context) error {
	out, err := c.queue.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
		QueueUrl:            aws.String(c.queueURL),
		MaxNumberOfMessages: maxMessages,
		WaitTimeSeconds:     receiveWaitSeconds,
		VisibilityTimeout:   visibilityTimeout,
	})
	if err != nil {
		return err
	}
	for _, m := range out.Messages {
		c.handleMessage(ctx, m)
	}
	return nil
}

// handleMessage adds a successful image push to the Batch and acknowledges the
// message.
//
// The Dependency is added and the message deleted immediately, rather than held
// until Flush: at-most-once matches the pipeline being lossy by design (ADR-0002),
// and holding receipt handles across a Window — and across unbounded postpones —
// would outlive the visibility timeout and cause redelivery anyway. The source
// CronJob schedule is the backstop.
func (c *Consumer) handleMessage(ctx context.Context, m sqstypes.Message) {
	var event imageActionEvent
	if err := json.Unmarshal([]byte(aws.ToString(m.Body)), &event); err != nil {
		// Deliberately NOT deleted. A body we cannot parse will never become
		// parseable, so letting the redrive policy move it to the dead-letter
		// queue after maxReceiveCount makes it visible and inspectable, where
		// deleting it here would hide the problem entirely.
		c.logger.Warn("failed to parse ecr event, leaving for the dead-letter queue", "error", err)
		return
	}

	dep, reason, ok := event.dependency()
	if !ok {
		// A well-formed event we simply do not act on: acknowledge it so it does
		// not consume receive attempts.
		c.logger.Debug("ecr event ignored", "reason", reason, "repository", event.Detail.RepositoryName)
		c.deleteMessage(ctx, m)
		return
	}

	c.logger.Info("ecr image push accepted",
		"repository", event.Detail.RepositoryName,
		"image_tag", event.Detail.ImageTag,
		"dependency", dep)
	c.batch.Add(dep)
	c.deleteMessage(ctx, m)
}

// deleteMessage acknowledges a handled message. A failure is warned about but not
// retried: the message reappears after the visibility timeout and produces a
// duplicate Add, which the Batch deduplicates.
func (c *Consumer) deleteMessage(ctx context.Context, m sqstypes.Message) {
	if _, err := c.queue.DeleteMessage(ctx, &sqs.DeleteMessageInput{
		QueueUrl:      aws.String(c.queueURL),
		ReceiptHandle: m.ReceiptHandle,
	}); err != nil {
		c.logger.Warn("failed to delete handled ecr event", "error", err)
	}
}
