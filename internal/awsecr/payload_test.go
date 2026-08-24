package awsecr

import (
	"encoding/json"
	"testing"

	"github.com/ilia/renovate-trigger/internal/graph"
)

// pushEvent builds the EventBridge envelope AWS documents for a completed image
// push. Events are constructed as Go values and marshalled, matching how the
// webhook tests build their payloads — there are no fixture files in this repo.
func pushEvent(t *testing.T, mutate func(*imageActionEvent)) []byte {
	t.Helper()
	e := imageActionEvent{
		Source:     "aws.ecr",
		DetailType: "ECR Image Action",
		Account:    "123456789012",
		Region:     "us-west-2",
		Resources:  []string{},
	}
	e.Detail.Result = "SUCCESS"
	e.Detail.ActionType = "PUSH"
	e.Detail.RepositoryName = "my-repository-name"
	e.Detail.ImageDigest = "sha256:7f5b2640fe6fb4f46592dfd3410c4a79dac4f89e4782432e0378abcd1234"
	e.Detail.ImageTag = "latest"
	if mutate != nil {
		mutate(&e)
	}
	body, err := json.Marshal(e)
	if err != nil {
		t.Fatalf("marshalling event: %v", err)
	}
	return body
}

func parseEvent(t *testing.T, body []byte) imageActionEvent {
	t.Helper()
	var e imageActionEvent
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("unmarshalling event: %v", err)
	}
	return e
}

func TestDependencyAcceptsSuccessfulPush(t *testing.T) {
	e := parseEvent(t, pushEvent(t, nil))

	dep, _, ok := e.dependency()
	if !ok {
		t.Fatal("expected a successful push to be accepted")
	}
	want := graph.Dependency{
		Kind: graph.KindECR,
		Name: "arn:aws:ecr:us-west-2:123456789012:repository/my-repository-name",
	}
	if dep != want {
		t.Errorf("dependency = %v, want %v", dep, want)
	}
}

// An untagged (digest-only) push is still new content, and it is how the untagged
// child manifests of a multi-arch push arrive.
func TestDependencyAcceptsUntaggedPush(t *testing.T) {
	e := parseEvent(t, pushEvent(t, func(e *imageActionEvent) { e.Detail.ImageTag = "" }))

	if _, _, ok := e.dependency(); !ok {
		t.Error("expected an untagged push to be accepted")
	}
}

func TestDependencyRejects(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*imageActionEvent)
		reason string
	}{
		{"delete action", func(e *imageActionEvent) { e.Detail.ActionType = "DELETE" }, "action-type is not PUSH"},
		{"storage class change", func(e *imageActionEvent) { e.Detail.ActionType = "UPDATE_STORAGE_CLASS" }, "action-type is not PUSH"},
		{"failed push", func(e *imageActionEvent) { e.Detail.Result = "FAILURE" }, "result is not SUCCESS"},
		{"foreign source", func(e *imageActionEvent) { e.Source = "aws.s3" }, "source is not aws.ecr"},
		{"replication action", func(e *imageActionEvent) { e.DetailType = "ECR Replication Action" }, "detail-type is not ECR Image Action"},
		{"no repository name", func(e *imageActionEvent) { e.Detail.RepositoryName = "" }, "repository-name is empty"},
		{"no account", func(e *imageActionEvent) { e.Account = "" }, "cannot determine repository ARN"},
		{"no region", func(e *imageActionEvent) { e.Region = "" }, "cannot determine repository ARN"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := parseEvent(t, pushEvent(t, tt.mutate))
			dep, reason, ok := e.dependency()
			if ok {
				t.Fatalf("expected rejection, got %v", dep)
			}
			if reason != tt.reason {
				t.Errorf("reason = %q, want %q", reason, tt.reason)
			}
		})
	}
}

// An ARN AWS gives us beats one we assemble, should resources ever be populated.
func TestDependencyPrefersSuppliedResourceARN(t *testing.T) {
	supplied := "arn:aws:ecr:eu-west-1:999988887777:repository/supplied"
	e := parseEvent(t, pushEvent(t, func(e *imageActionEvent) {
		e.Resources = []string{supplied}
	}))

	dep, _, ok := e.dependency()
	if !ok {
		t.Fatal("expected the push to be accepted")
	}
	if dep.Name != supplied {
		t.Errorf("dependency name = %q, want %q", dep.Name, supplied)
	}
}

func TestPartitionForRegion(t *testing.T) {
	tests := map[string]string{
		"us-west-2":      "aws",
		"eu-central-1":   "aws",
		"cn-north-1":     "aws-cn",
		"us-gov-west-1":  "aws-us-gov",
		"us-iso-east-1":  "aws-iso",
		"us-isob-east-1": "aws-iso-b",
	}
	for region, want := range tests {
		if got := partitionForRegion(region); got != want {
			t.Errorf("partitionForRegion(%q) = %q, want %q", region, got, want)
		}
	}
}

// The ARN must carry the right partition, or every API call against it fails.
func TestDependencyARNPartitionFollowsRegion(t *testing.T) {
	e := parseEvent(t, pushEvent(t, func(e *imageActionEvent) { e.Region = "cn-north-1" }))

	dep, _, ok := e.dependency()
	if !ok {
		t.Fatal("expected the push to be accepted")
	}
	want := "arn:aws-cn:ecr:cn-north-1:123456789012:repository/my-repository-name"
	if dep.Name != want {
		t.Errorf("dependency name = %q, want %q", dep.Name, want)
	}
}
