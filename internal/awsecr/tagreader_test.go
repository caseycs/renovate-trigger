package awsecr

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"
)

const testTagKey = "renovate-trigger/dependents"

// fakeTagLister returns a scripted tag set and records what it was asked for,
// including the region the per-call option override selected.
type fakeTagLister struct {
	tags      []ecrtypes.Tag
	err       error
	gotARN    string
	gotRegion string
	calls     int
}

func (f *fakeTagLister) ListTagsForResource(_ context.Context, in *ecr.ListTagsForResourceInput,
	optFns ...func(*ecr.Options)) (*ecr.ListTagsForResourceOutput, error) {
	f.calls++
	f.gotARN = aws.ToString(in.ResourceArn)

	var opts ecr.Options
	for _, fn := range optFns {
		fn(&opts)
	}
	f.gotRegion = opts.Region

	if f.err != nil {
		return nil, f.err
	}
	return &ecr.ListTagsForResourceOutput{Tags: f.tags}, nil
}

func tag(k, v string) ecrtypes.Tag {
	return ecrtypes.Tag{Key: aws.String(k), Value: aws.String(v)}
}

const testARN = "arn:aws:ecr:us-west-2:123456789012:repository/api"

func TestFetchDependentsReturnsDeclaredDependents(t *testing.T) {
	f := &fakeTagLister{tags: []ecrtypes.Tag{
		tag("Environment", "prod"),
		tag(testTagKey, " org/app-a , org/app-b "),
	}}

	deps, found, err := NewTagReader(f, testTagKey).FetchDependents(context.Background(), testARN)
	if err != nil {
		t.Fatalf("FetchDependents error: %v", err)
	}
	if !found {
		t.Fatal("found = false, want true")
	}
	want := []string{"org/app-a", "org/app-b"}
	if len(deps) != len(want) {
		t.Fatalf("deps = %v, want %v", deps, want)
	}
	for i := range want {
		if deps[i] != want[i] {
			t.Fatalf("deps = %v, want %v", deps, want)
		}
	}
	if f.gotARN != testARN {
		t.Errorf("ARN = %q, want %q", f.gotARN, testARN)
	}
}

// A repository with no Dependents tag is a normal opt-out, not an error.
func TestFetchDependentsTagAbsentIsOptOut(t *testing.T) {
	f := &fakeTagLister{tags: []ecrtypes.Tag{tag("Environment", "prod")}}

	deps, found, err := NewTagReader(f, testTagKey).FetchDependents(context.Background(), testARN)
	if err != nil {
		t.Fatalf("FetchDependents error: %v", err)
	}
	if found {
		t.Error("found = true, want false")
	}
	if len(deps) != 0 {
		t.Errorf("deps = %v, want empty", deps)
	}
}

// AWS tag keys are case-sensitive, so a near-miss key must not match.
func TestFetchDependentsTagKeyIsCaseSensitive(t *testing.T) {
	f := &fakeTagLister{tags: []ecrtypes.Tag{tag("Renovate-Trigger/Dependents", "org/app-a")}}

	_, found, err := NewTagReader(f, testTagKey).FetchDependents(context.Background(), testARN)
	if err != nil {
		t.Fatalf("FetchDependents error: %v", err)
	}
	if found {
		t.Error("found = true, want false for a differently-cased key")
	}
}

func TestFetchDependentsHonoursCustomTagKey(t *testing.T) {
	f := &fakeTagLister{tags: []ecrtypes.Tag{tag("nobi.life/dependents", "org/app-a")}}

	deps, found, err := NewTagReader(f, "nobi.life/dependents").FetchDependents(context.Background(), testARN)
	if err != nil {
		t.Fatalf("FetchDependents error: %v", err)
	}
	if !found || len(deps) != 1 || deps[0] != "org/app-a" {
		t.Errorf("deps = %v, found = %v, want [org/app-a], true", deps, found)
	}
}

// An empty tag value is a present declaration listing nothing — found, but with no
// dependents. The Resolver logs that at DEBUG.
func TestFetchDependentsEmptyValueIsFoundWithNoDependents(t *testing.T) {
	f := &fakeTagLister{tags: []ecrtypes.Tag{tag(testTagKey, "")}}

	deps, found, err := NewTagReader(f, testTagKey).FetchDependents(context.Background(), testARN)
	if err != nil {
		t.Fatalf("FetchDependents error: %v", err)
	}
	if !found {
		t.Error("found = false, want true")
	}
	if len(deps) != 0 {
		t.Errorf("deps = %v, want empty", deps)
	}
}

// A repository deleted between the push and the read has nothing to resolve.
func TestFetchDependentsRepositoryNotFoundIsOptOut(t *testing.T) {
	f := &fakeTagLister{err: &ecrtypes.RepositoryNotFoundException{}}

	_, found, err := NewTagReader(f, testTagKey).FetchDependents(context.Background(), testARN)
	if err != nil {
		t.Fatalf("FetchDependents error: %v", err)
	}
	if found {
		t.Error("found = true, want false")
	}
}

// A denied read must be an error, not a silent global opt-out — otherwise a broken
// IAM policy looks exactly like nobody having opted in.
func TestFetchDependentsOtherErrorIsError(t *testing.T) {
	f := &fakeTagLister{err: errors.New("AccessDeniedException: not authorized")}

	if _, _, err := NewTagReader(f, testTagKey).FetchDependents(context.Background(), testARN); err == nil {
		t.Error("expected an error for a denied read")
	}
}

// The read must target the repository's own region, not the client's default.
func TestFetchDependentsTargetsTheARNRegion(t *testing.T) {
	f := &fakeTagLister{tags: []ecrtypes.Tag{tag(testTagKey, "org/app-a")}}
	arn := "arn:aws:ecr:eu-west-1:123456789012:repository/api"

	if _, _, err := NewTagReader(f, testTagKey).FetchDependents(context.Background(), arn); err != nil {
		t.Fatalf("FetchDependents error: %v", err)
	}
	if f.gotRegion != "eu-west-1" {
		t.Errorf("region = %q, want eu-west-1", f.gotRegion)
	}
}

func TestFetchDependentsRejectsMalformedARNWithoutCallingAWS(t *testing.T) {
	for _, arn := range []string{"", "not-an-arn", "arn:aws:s3:::bucket", "arn:aws:ecr::123456789012:repository/api"} {
		f := &fakeTagLister{}
		if _, _, err := NewTagReader(f, testTagKey).FetchDependents(context.Background(), arn); err == nil {
			t.Errorf("FetchDependents(%q) expected an error", arn)
		}
		if f.calls != 0 {
			t.Errorf("FetchDependents(%q) made %d API calls, want 0", arn, f.calls)
		}
	}
}
