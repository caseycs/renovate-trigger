package batch

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"sort"
	"sync"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// fakeCreator records the repos of the most recent job.
type fakeCreator struct {
	mu    sync.Mutex
	calls int
	repos []string
}

func (f *fakeCreator) CreateJobForRepos(_ context.Context, repos []string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.repos = repos
	return "job-fake", nil
}

func (f *fakeCreator) snapshot() (int, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, append([]string(nil), f.repos...)
}

// stubGate reports a fixed active state.
type stubGate struct {
	active bool
	err    error
}

func (g stubGate) Active(context.Context) (bool, error) { return g.active, g.err }

// stubResolver returns a fixed dependent set.
type stubResolver struct{ deps []string }

func (r stubResolver) Resolve(context.Context, []string) ([]string, error) { return r.deps, nil }

// passthroughResolver resolves each source repo to itself.
type passthroughResolver struct{}

func (passthroughResolver) Resolve(_ context.Context, sources []string) ([]string, error) {
	return sources, nil
}

func newTestCollector(window time.Duration, gate RunGate, resolver Resolver, creator JobCreator) *Collector {
	return NewCollector(window, gate, resolver, creator, testLogger())
}

func TestCollectorFlushesAfterWindow(t *testing.T) {
	creator := &fakeCreator{}
	c := newTestCollector(100*time.Millisecond, OpenGate{}, passthroughResolver{}, creator)
	defer c.Stop()

	c.Add("org/repo-a")
	c.Add("org/repo-b")

	time.Sleep(250 * time.Millisecond)

	calls, repos := creator.snapshot()
	if calls != 1 {
		t.Fatalf("expected 1 job, got %d", calls)
	}
	sort.Strings(repos)
	if len(repos) != 2 || repos[0] != "org/repo-a" || repos[1] != "org/repo-b" {
		t.Errorf("unexpected repos: %v", repos)
	}
}

func TestCollectorDeduplicates(t *testing.T) {
	creator := &fakeCreator{}
	c := newTestCollector(100*time.Millisecond, OpenGate{}, passthroughResolver{}, creator)
	defer c.Stop()

	c.Add("org/repo-a")
	c.Add("org/repo-a")
	c.Add("org/repo-a")

	time.Sleep(250 * time.Millisecond)

	_, repos := creator.snapshot()
	if len(repos) != 1 {
		t.Fatalf("expected 1 repo (deduped), got %d: %v", len(repos), repos)
	}
}

func TestCollectorNoFlushWhenEmpty(t *testing.T) {
	creator := &fakeCreator{}
	c := newTestCollector(50*time.Millisecond, OpenGate{}, passthroughResolver{}, creator)
	defer c.Stop()

	time.Sleep(150 * time.Millisecond)

	if calls, _ := creator.snapshot(); calls != 0 {
		t.Error("job should not be created when no repos added")
	}
}

func TestCollectorStopCancelsTimer(t *testing.T) {
	creator := &fakeCreator{}
	c := newTestCollector(200*time.Millisecond, OpenGate{}, passthroughResolver{}, creator)

	c.Add("org/repo")
	c.Stop()

	time.Sleep(300 * time.Millisecond)

	if calls, _ := creator.snapshot(); calls != 0 {
		t.Error("job should not be created after Stop()")
	}
}

func TestCollectorConcurrentAdd(t *testing.T) {
	creator := &fakeCreator{}
	c := newTestCollector(200*time.Millisecond, OpenGate{}, passthroughResolver{}, creator)
	defer c.Stop()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			repo := "org/repo-a"
			if i%2 == 0 {
				repo = "org/repo-b"
			}
			c.Add(repo)
		}(i)
	}
	wg.Wait()

	time.Sleep(350 * time.Millisecond)

	_, repos := creator.snapshot()
	if len(repos) != 2 {
		t.Errorf("expected 2 deduped repos, got %d: %v", len(repos), repos)
	}
}

// The pass-through resolver seam feeds the source repos straight to the creator.
func TestCollectorResolvesThroughSeam(t *testing.T) {
	creator := &fakeCreator{}
	c := newTestCollector(50*time.Millisecond, OpenGate{}, stubResolver{deps: []string{"org/dependent"}}, creator)
	defer c.Stop()

	c.Add("org/source")
	time.Sleep(150 * time.Millisecond)

	_, repos := creator.snapshot()
	if len(repos) != 1 || repos[0] != "org/dependent" {
		t.Errorf("repos = %v, want [org/dependent]", repos)
	}
}

// An empty resolution creates no job.
func TestCollectorEmptyResolutionCreatesNoJob(t *testing.T) {
	creator := &fakeCreator{}
	c := newTestCollector(50*time.Millisecond, OpenGate{}, stubResolver{deps: nil}, creator)
	defer c.Stop()

	c.Add("org/source")
	time.Sleep(150 * time.Millisecond)

	if calls, _ := creator.snapshot(); calls != 0 {
		t.Error("no job should be created for an empty resolution")
	}
}

// An active run postpones the flush (no job created, batch retained).
func TestCollectorPostponesWhenGateActive(t *testing.T) {
	creator := &fakeCreator{}
	c := newTestCollector(50*time.Millisecond, stubGate{active: true}, passthroughResolver{}, creator)
	defer c.Stop()

	c.Add("org/source")
	time.Sleep(150 * time.Millisecond)

	if calls, _ := creator.snapshot(); calls != 0 {
		t.Error("no job should be created while a run is active")
	}
}

// A gate error is treated as active (postpone), never as a green light.
func TestCollectorGateErrorPostpones(t *testing.T) {
	creator := &fakeCreator{}
	c := newTestCollector(50*time.Millisecond, stubGate{err: errors.New("boom")}, passthroughResolver{}, creator)
	defer c.Stop()

	c.Add("org/source")
	time.Sleep(150 * time.Millisecond)

	if calls, _ := creator.snapshot(); calls != 0 {
		t.Error("no job should be created when the gate check errors")
	}
}
