// Package graph holds the vocabulary of the dependency graph: the identity of a
// Dependency whose publication may make Dependents stale, and the parsing and
// validation of the Dependent lists that Dependencies declare. It is shared by
// ingestion (webhook, awsecr), batching, and resolution so none of those packages
// has to import another's implementation.
package graph

import (
	"strings"
	"unicode"
)

// Kind distinguishes what sort of thing published, which decides how its
// Dependents are declared and therefore how they are read at Flush time.
type Kind string

const (
	// KindGitHub is a GitHub repository that published a tag. Its Dependents are
	// declared in a renovate.trigger.json Trigger declaration on its default
	// branch.
	KindGitHub Kind = "github"
	// KindECR is an Amazon ECR repository that received an image push. Its
	// Dependents are declared in a Dependents tag on the repository itself.
	KindECR Kind = "ecr"
)

// Dependency identifies one thing whose publication may have made its Dependents
// stale. Name is Kind-specific: an "owner/name" repository for KindGitHub, a
// repository ARN for KindECR. The struct is comparable so it can key the Batch's
// deduplication set directly.
type Dependency struct {
	Kind Kind
	Name string
}

// String renders a Dependency for logs, e.g. "github:org/lib".
func (d Dependency) String() string {
	return string(d.Kind) + ":" + d.Name
}

// MarshalText makes a Dependency log as "github:org/lib" rather than as an
// object. slog's JSON handler marshals attribute values with encoding/json,
// which ignores Stringer but honours TextMarshaler.
func (d Dependency) MarshalText() ([]byte, error) {
	return []byte(d.String()), nil
}

// ValidDependent reports whether s is a well-formed "owner/name" repository.
func ValidDependent(s string) bool {
	owner, name, ok := strings.Cut(s, "/")
	return ok && owner != "" && name != "" && !strings.Contains(name, "/")
}

// ParseDependents splits a Dependents declaration into entries. Comma is the
// documented separator, but whitespace is accepted too: AWS's cross-service
// guidance on allowed tag-value characters does not list ",", so a declaration
// may end up space-separated in practice. FieldsFunc trims and drops empties for
// free, so a trailing comma is silence rather than a warning.
//
// It deliberately does not reject malformed entries: the Resolver already warns
// about and skips those, and keeping that in one place keeps the log behaviour
// identical across Kinds.
func ParseDependents(declaration string) []string {
	return strings.FieldsFunc(declaration, func(r rune) bool {
		return r == ',' || unicode.IsSpace(r)
	})
}
