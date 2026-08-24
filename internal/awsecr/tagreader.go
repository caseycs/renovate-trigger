package awsecr

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	ecrtypes "github.com/aws/aws-sdk-go-v2/service/ecr/types"

	"github.com/ilia/renovate-trigger/internal/graph"
)

// requestTimeout bounds a single tag read, mirroring the GitHub client's
// per-request budget.
const requestTimeout = 10 * time.Second

// tagLister is the subset of the ECR API used to read repository tags. It is
// satisfied by *ecr.Client; tests supply a fake.
type tagLister interface {
	ListTagsForResource(ctx context.Context, in *ecr.ListTagsForResourceInput,
		optFns ...func(*ecr.Options)) (*ecr.ListTagsForResourceOutput, error)
}

// TagReader reads a Dependency's Dependents from an ECR repository's AWS resource
// tags. It is the ECR analogue of ghapp.Client: the presence of the Dependents tag
// is the repository's opt-in.
type TagReader struct {
	ecr    tagLister
	tagKey string
}

// NewTagReader returns a TagReader that reads dependents from the tagKey tag.
func NewTagReader(api tagLister, tagKey string) *TagReader {
	return &TagReader{ecr: api, tagKey: tagKey}
}

// FetchDependents reads the Dependents tag from the ECR repository named by arn.
// found is false when the tag is absent (opt-out) or the repository no longer
// exists; any other failure — notably AccessDenied — is an error, so a broken IAM
// policy is loud instead of looking like every repository opting out.
func (t *TagReader) FetchDependents(ctx context.Context, arn string) (deps []string, found bool, err error) {
	region, err := regionFromARN(arn)
	if err != nil {
		return nil, false, err
	}

	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()

	// Override the region per call rather than holding one client per region: the
	// ARN already says where the repository lives, and a Dependency in another
	// region must not be read against this client's default endpoint.
	out, err := t.ecr.ListTagsForResource(ctx, &ecr.ListTagsForResourceInput{
		ResourceArn: aws.String(arn),
	}, func(o *ecr.Options) { o.Region = region })
	if err != nil {
		var notFound *ecrtypes.RepositoryNotFoundException
		if errors.As(err, &notFound) {
			// The repository was deleted between the push and this read. Treat it
			// as an opt-out rather than an error; there is nothing to resolve.
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("listing tags for %s: %w", arn, err)
	}

	for _, tag := range out.Tags {
		// AWS tag keys are case-sensitive, so match exactly.
		if tag.Key != nil && *tag.Key == t.tagKey {
			return graph.ParseDependents(aws.ToString(tag.Value)), true, nil
		}
	}
	return nil, false, nil
}

// regionFromARN pulls the region out of an ECR repository ARN
// (arn:partition:ecr:region:account:repository/name), so a malformed ARN fails
// before an API call rather than after one.
func regionFromARN(arn string) (string, error) {
	parts := strings.SplitN(arn, ":", 6)
	if len(parts) < 6 || parts[0] != "arn" || parts[2] != "ecr" || parts[3] == "" {
		return "", fmt.Errorf("not an ECR repository ARN: %q", arn)
	}
	return parts[3], nil
}
