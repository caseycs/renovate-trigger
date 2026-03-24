package batch

import (
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

func TestCollectorFlushesAfterWindow(t *testing.T) {
	var mu sync.Mutex
	var flushedRepos []string

	c := NewCollector(100*time.Millisecond, func(repos []string) {
		mu.Lock()
		defer mu.Unlock()
		flushedRepos = repos
	}, testLogger())
	defer c.Stop()

	c.Add("org/repo-a")
	c.Add("org/repo-b")

	time.Sleep(250 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(flushedRepos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(flushedRepos))
	}

	sort.Strings(flushedRepos)
	if flushedRepos[0] != "org/repo-a" || flushedRepos[1] != "org/repo-b" {
		t.Errorf("unexpected repos: %v", flushedRepos)
	}
}

func TestCollectorDeduplicates(t *testing.T) {
	var mu sync.Mutex
	var flushedRepos []string

	c := NewCollector(100*time.Millisecond, func(repos []string) {
		mu.Lock()
		defer mu.Unlock()
		flushedRepos = repos
	}, testLogger())
	defer c.Stop()

	c.Add("org/repo-a")
	c.Add("org/repo-a")
	c.Add("org/repo-a")

	time.Sleep(250 * time.Millisecond)

	mu.Lock()
	defer mu.Unlock()

	if len(flushedRepos) != 1 {
		t.Fatalf("expected 1 repo (deduped), got %d: %v", len(flushedRepos), flushedRepos)
	}
}

func TestCollectorNoFlushWhenEmpty(t *testing.T) {
	flushCalled := false
	c := NewCollector(50*time.Millisecond, func(repos []string) {
		flushCalled = true
	}, testLogger())
	defer c.Stop()

	time.Sleep(150 * time.Millisecond)

	if flushCalled {
		t.Error("flush should not be called when no repos added")
	}
}

func TestCollectorStopCancelsTimer(t *testing.T) {
	flushCalled := false
	c := NewCollector(200*time.Millisecond, func(repos []string) {
		flushCalled = true
	}, testLogger())

	c.Add("org/repo")
	c.Stop()

	time.Sleep(300 * time.Millisecond)

	if flushCalled {
		t.Error("flush should not be called after Stop()")
	}
}

func TestCollectorConcurrentAdd(t *testing.T) {
	var mu sync.Mutex
	var flushedRepos []string

	c := NewCollector(200*time.Millisecond, func(repos []string) {
		mu.Lock()
		defer mu.Unlock()
		flushedRepos = repos
	}, testLogger())
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

	mu.Lock()
	defer mu.Unlock()

	if len(flushedRepos) != 2 {
		t.Errorf("expected 2 deduped repos, got %d: %v", len(flushedRepos), flushedRepos)
	}
}
