// Package resolve expands a batch of source (dependency) repositories into the
// deduplicated set of dependent repositories to run Renovate on, by reading each
// dependency's trigger declaration. Per-source failures degrade independently so
// one broken declaration never sinks the whole batch.
package resolve

import (
	"context"
	"log/slog"
	"strings"
)

// triggerFetcher reads a repository's trigger declaration. It is satisfied by
// the GitHub App client; tests supply a fake.
type triggerFetcher interface {
	FetchTriggerFile(ctx context.Context, repo string) (deps []string, found bool, err error)
}

// Resolver expands source (dependency) repos into their deduplicated dependents.
type Resolver struct {
	fetcher triggerFetcher
	logger  *slog.Logger
}

// New returns a Resolver that reads trigger declarations via fetcher.
func New(fetcher triggerFetcher, logger *slog.Logger) *Resolver {
	return &Resolver{fetcher: fetcher, logger: logger}
}

// Resolve reads each source's trigger declaration and returns the deduplicated
// union of their dependents. A source whose declaration is missing (opt-out),
// errors, or is unreadable is dropped; invalid dependent entries are skipped.
// The returned slice is empty when nothing resolves, signalling "create no run".
func (r *Resolver) Resolve(ctx context.Context, sources []string) ([]string, error) {
	seen := make(map[string]struct{})
	var dependents []string

	for _, source := range sources {
		deps, found, err := r.fetcher.FetchTriggerFile(ctx, source)
		if err != nil {
			r.logger.Warn("failed to read trigger declaration, dropping source", "source", source, "error", err)
			continue
		}
		if !found {
			r.logger.Debug("no trigger declaration, source opted out", "source", source)
			continue
		}

		added := 0
		for _, dep := range deps {
			if !validRepo(dep) {
				r.logger.Warn("invalid dependent entry, skipping", "source", source, "dependent", dep)
				continue
			}
			if _, ok := seen[dep]; ok {
				continue
			}
			seen[dep] = struct{}{}
			dependents = append(dependents, dep)
			added++
		}
		if added == 0 {
			r.logger.Debug("trigger declaration listed no usable dependents", "source", source)
		}
	}

	return dependents, nil
}

// validRepo reports whether s is a well-formed "owner/name" repository.
func validRepo(s string) bool {
	owner, name, ok := strings.Cut(s, "/")
	return ok && owner != "" && name != "" && !strings.Contains(name, "/")
}
