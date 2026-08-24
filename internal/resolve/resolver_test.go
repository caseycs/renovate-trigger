package resolve

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"sort"
	"strings"
	"testing"

	"github.com/ilia/renovate-trigger/internal/graph"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

type fakeResult struct {
	deps  []string
	found bool
	err   error
}

// fakeFetcher returns a scripted result per Dependency name; an unlisted name is
// treated as not-found.
type fakeFetcher struct {
	results map[string]fakeResult
}

func (f fakeFetcher) FetchDependents(_ context.Context, name string) ([]string, bool, error) {
	r, ok := f.results[name]
	if !ok {
		return nil, false, nil
	}
	return r.deps, r.found, r.err
}

// resolveSorted resolves GitHub Dependencies, which is the case every test below
// predates the introduction of Kinds; cross-Kind behaviour has its own tests.
func resolveSorted(t *testing.T, results map[string]fakeResult, names ...string) []string {
	t.Helper()
	deps := make([]graph.Dependency, 0, len(names))
	for _, n := range names {
		deps = append(deps, graph.Dependency{Kind: graph.KindGitHub, Name: n})
	}
	r := New(map[graph.Kind]DependentsFetcher{graph.KindGitHub: fakeFetcher{results: results}}, testLogger())
	got, err := r.Resolve(context.Background(), deps)
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	sort.Strings(got)
	return got
}

func TestResolveUnionAndDedup(t *testing.T) {
	got := resolveSorted(t, map[string]fakeResult{
		"org/lib-a": {deps: []string{"org/app-1", "org/app-2"}, found: true},
		"org/lib-b": {deps: []string{"org/app-2", "org/app-3"}, found: true},
	}, "org/lib-a", "org/lib-b")

	want := []string{"org/app-1", "org/app-2", "org/app-3"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestResolveNotFoundOptOut(t *testing.T) {
	got := resolveSorted(t, map[string]fakeResult{
		"org/lib-a": {deps: []string{"org/app-1"}, found: true},
		// org/lib-b is unlisted -> not found -> opt-out
	}, "org/lib-a", "org/lib-b")

	if len(got) != 1 || got[0] != "org/app-1" {
		t.Errorf("got %v, want [org/app-1]", got)
	}
}

func TestResolveFetchErrorDropsSourceOnly(t *testing.T) {
	got := resolveSorted(t, map[string]fakeResult{
		"org/lib-a": {err: errors.New("boom")},
		"org/lib-b": {deps: []string{"org/app-2"}, found: true},
	}, "org/lib-a", "org/lib-b")

	if len(got) != 1 || got[0] != "org/app-2" {
		t.Errorf("got %v, want [org/app-2] (broken source dropped, other kept)", got)
	}
}

func TestResolveSkipsInvalidEntriesKeepsValid(t *testing.T) {
	got := resolveSorted(t, map[string]fakeResult{
		"org/lib": {deps: []string{"noslash", "org/app-1", "a/b/c", "", "org/app-2"}, found: true},
	}, "org/lib")

	want := []string{"org/app-1", "org/app-2"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestResolveEmptyUnionWhenNothingResolves(t *testing.T) {
	got := resolveSorted(t, map[string]fakeResult{
		"org/lib-a": {err: errors.New("boom")},
		// org/lib-b not found
	}, "org/lib-a", "org/lib-b")

	if len(got) != 0 {
		t.Errorf("got %v, want empty union", got)
	}
}

func TestResolveFoundButNoDependents(t *testing.T) {
	got := resolveSorted(t, map[string]fakeResult{
		"org/lib": {deps: nil, found: true},
	}, "org/lib")

	if len(got) != 0 {
		t.Errorf("got %v, want empty (declared no dependents)", got)
	}
}

// TestResolveDispatchesPerKind proves the union spans Kinds: a GitHub Dependency
// resolves through the trigger-file fetcher while an ECR Dependency resolves
// through the tag fetcher, and an overlapping Dependent is deduplicated across
// the two.
func TestResolveDispatchesPerKind(t *testing.T) {
	gh := fakeFetcher{results: map[string]fakeResult{
		"org/lib": {deps: []string{"org/app-1", "org/shared"}, found: true},
	}}
	ecr := fakeFetcher{results: map[string]fakeResult{
		"arn:aws:ecr:us-west-2:123456789012:repository/api": {deps: []string{"org/shared", "org/app-2"}, found: true},
	}}

	r := New(map[graph.Kind]DependentsFetcher{
		graph.KindGitHub: gh,
		graph.KindECR:    ecr,
	}, testLogger())

	got, err := r.Resolve(context.Background(), []graph.Dependency{
		{Kind: graph.KindGitHub, Name: "org/lib"},
		{Kind: graph.KindECR, Name: "arn:aws:ecr:us-west-2:123456789012:repository/api"},
	})
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	sort.Strings(got)

	want := []string{"org/app-1", "org/app-2", "org/shared"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// TestResolveDropsDependencyWithUnregisteredKind covers the ECR-disabled
// deployment: an ECR Dependency somehow reaches resolution with no ECR fetcher
// registered. It must be dropped with a warning while the rest of the batch
// resolves normally.
func TestResolveDropsDependencyWithUnregisteredKind(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	gh := fakeFetcher{results: map[string]fakeResult{
		"org/lib": {deps: []string{"org/app-1"}, found: true},
	}}
	r := New(map[graph.Kind]DependentsFetcher{graph.KindGitHub: gh}, logger)

	got, err := r.Resolve(context.Background(), []graph.Dependency{
		{Kind: graph.KindECR, Name: "arn:aws:ecr:us-west-2:123456789012:repository/api"},
		{Kind: graph.KindGitHub, Name: "org/lib"},
	})
	if err != nil {
		t.Fatalf("Resolve error: %v", err)
	}
	if len(got) != 1 || got[0] != "org/app-1" {
		t.Fatalf("got %v, want [org/app-1]", got)
	}
	if !strings.Contains(buf.String(), "no fetcher registered for dependency kind") {
		t.Errorf("expected a warning about the unregistered kind, got: %s", buf.String())
	}
}
