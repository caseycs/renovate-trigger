package resolve

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sort"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

type fakeResult struct {
	deps  []string
	found bool
	err   error
}

// fakeFetcher returns a scripted result per source repo; an unlisted repo is
// treated as not-found.
type fakeFetcher struct {
	results map[string]fakeResult
}

func (f fakeFetcher) FetchTriggerFile(_ context.Context, repo string) ([]string, bool, error) {
	r, ok := f.results[repo]
	if !ok {
		return nil, false, nil
	}
	return r.deps, r.found, r.err
}

func resolveSorted(t *testing.T, results map[string]fakeResult, sources ...string) []string {
	t.Helper()
	r := New(fakeFetcher{results: results}, testLogger())
	got, err := r.Resolve(context.Background(), sources)
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
