package awsecr

import (
	"strings"

	"github.com/ilia/renovate-trigger/internal/graph"
)

// imageActionEvent is the EventBridge envelope for an "ECR Image Action" event.
// Only the fields we filter or identify on are declared, matching the deliberately
// tiny shape of webhook.CreateEvent.
//
// Note that resources is empty for this detail-type, so the repository ARN has to
// be constructed from account, region, and repository-name.
type imageActionEvent struct {
	Source     string   `json:"source"`
	DetailType string   `json:"detail-type"`
	Account    string   `json:"account"`
	Region     string   `json:"region"`
	Resources  []string `json:"resources"`
	Detail     struct {
		Result         string `json:"result"`
		ActionType     string `json:"action-type"`
		RepositoryName string `json:"repository-name"`
		ImageDigest    string `json:"image-digest"`
		ImageTag       string `json:"image-tag"`
	} `json:"detail"`
}

// dependency reports the pushed repository as a Dependency. ok is false when the
// event is not a successful ECR image push, with reason naming the filter that
// rejected it so the caller can log it.
//
// The image tag is deliberately not required: an untagged (digest-only) push is
// still new content, and a multi-arch push emits an event for the tagged manifest
// list plus events for its untagged children. All of them name the same
// repository, so the Batch collapses them into one entry — exactly as the GitHub
// side treats the concrete tag name as irrelevant.
func (e imageActionEvent) dependency() (dep graph.Dependency, reason string, ok bool) {
	switch {
	case e.Source != "aws.ecr":
		return graph.Dependency{}, "source is not aws.ecr", false
	case e.DetailType != "ECR Image Action":
		// DELETE and UPDATE_STORAGE_CLASS share this detail-type, but replication
		// and referrer actions have their own and are not image pushes.
		return graph.Dependency{}, "detail-type is not ECR Image Action", false
	case !strings.EqualFold(e.Detail.ActionType, "PUSH"):
		return graph.Dependency{}, "action-type is not PUSH", false
	case !strings.EqualFold(e.Detail.Result, "SUCCESS"):
		return graph.Dependency{}, "result is not SUCCESS", false
	case e.Detail.RepositoryName == "":
		return graph.Dependency{}, "repository-name is empty", false
	}

	arn, ok := e.repositoryARN()
	if !ok {
		return graph.Dependency{}, "cannot determine repository ARN", false
	}
	return graph.Dependency{Kind: graph.KindECR, Name: arn}, "", true
}

// repositoryARN returns the ARN of the pushed repository, preferring one AWS
// supplied over one we construct.
func (e imageActionEvent) repositoryARN() (string, bool) {
	// resources is documented as empty for this detail-type, but prefer it if AWS
	// ever starts populating it — an ARN they give us beats one we assemble.
	for _, r := range e.Resources {
		if strings.HasPrefix(r, "arn:") && strings.Contains(r, ":ecr:") && strings.Contains(r, ":repository/") {
			return r, true
		}
	}

	if e.Account == "" || e.Region == "" {
		return "", false
	}
	return "arn:" + partitionForRegion(e.Region) + ":ecr:" + e.Region + ":" + e.Account +
		":repository/" + e.Detail.RepositoryName, true
}

// partitionForRegion derives the ARN partition from a region name. Hardcoding
// "aws" would silently break every GovCloud, China, and isolated-region
// deployment, and the mapping is short enough not to be worth the risk.
func partitionForRegion(region string) string {
	switch {
	case strings.HasPrefix(region, "cn-"):
		return "aws-cn"
	case strings.HasPrefix(region, "us-gov-"):
		return "aws-us-gov"
	// Check the more specific "isob" prefix first: "us-isob-east-1" also has the
	// "us-iso" prefix.
	case strings.HasPrefix(region, "us-isob-"):
		return "aws-iso-b"
	case strings.HasPrefix(region, "us-iso-"):
		return "aws-iso"
	default:
		return "aws"
	}
}
