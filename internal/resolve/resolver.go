// Package resolve expands a batch of Dependencies into the deduplicated set of
// Dependents to run Renovate on, by reading each Dependency's declaration of its
// Dependents. Per-Dependency failures degrade independently so one broken
// declaration never sinks the whole batch.
package resolve

import (
	"context"
	"log/slog"

	"github.com/ilia/renovate-trigger/internal/graph"
)

// DependentsFetcher reads a Dependency's declaration of its Dependents. There is
// one implementation per graph.Kind: the GitHub App client reads a
// renovate.trigger.json Trigger declaration, the ECR tag reader reads a Dependents
// tag. Tests supply fakes.
type DependentsFetcher interface {
	FetchDependents(ctx context.Context, name string) (deps []string, found bool, err error)
}

// Resolver expands Dependencies into their deduplicated Dependents.
type Resolver struct {
	fetchers map[graph.Kind]DependentsFetcher
	logger   *slog.Logger
}

// New returns a Resolver that reads each Dependency's declaration via the fetcher
// registered for its Kind.
func New(fetchers map[graph.Kind]DependentsFetcher, logger *slog.Logger) *Resolver {
	return &Resolver{fetchers: fetchers, logger: logger}
}

// Resolve reads each Dependency's declaration and returns the deduplicated union
// of their Dependents. A Dependency whose declaration is missing (opt-out), errors,
// is unreadable, or whose Kind has no registered fetcher is dropped; invalid
// Dependent entries are skipped. The returned slice is empty when nothing
// resolves, signalling "create no run".
func (r *Resolver) Resolve(ctx context.Context, deps []graph.Dependency) ([]string, error) {
	seen := make(map[string]struct{})
	var dependents []string

	for _, dep := range deps {
		fetcher, ok := r.fetchers[dep.Kind]
		if !ok {
			// Defensive: an ingestion path exists for a Kind with no fetcher
			// registered. Dropping rather than failing keeps the batch alive.
			r.logger.Warn("no fetcher registered for dependency kind, dropping", "dependency", dep.String())
			continue
		}

		declared, found, err := fetcher.FetchDependents(ctx, dep.Name)
		if err != nil {
			r.logger.Warn("failed to read dependents declaration, dropping dependency", "dependency", dep.String(), "error", err)
			continue
		}
		if !found {
			r.logger.Debug("no dependents declaration, dependency opted out", "dependency", dep.String())
			continue
		}

		added := 0
		for _, d := range declared {
			if !graph.ValidDependent(d) {
				r.logger.Warn("invalid dependent entry, skipping", "dependency", dep.String(), "dependent", d)
				continue
			}
			if _, ok := seen[d]; ok {
				continue
			}
			seen[d] = struct{}{}
			dependents = append(dependents, d)
			added++
		}
		if added == 0 {
			r.logger.Debug("dependents declaration listed no usable dependents", "dependency", dep.String())
		}
	}

	return dependents, nil
}
