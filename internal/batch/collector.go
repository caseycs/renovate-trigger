package batch

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

// flushTimeout bounds the gate check, resolution, and job creation of a single
// flush.
const flushTimeout = 30 * time.Second

// RunGate reports whether a Renovate run is already active, so the collector can
// avoid overlapping runs. Its full behaviour lands with mutual exclusion.
type RunGate interface {
	Active(ctx context.Context) (bool, error)
}

// Resolver expands a batch of source (dependency) repos into the deduplicated
// set of dependent repos to run Renovate on.
type Resolver interface {
	Resolve(ctx context.Context, sources []string) ([]string, error)
}

// JobCreator creates a single Renovate run for the given target repos.
type JobCreator interface {
	CreateJobForRepos(ctx context.Context, repos []string) (string, error)
}

// OpenGate is scaffolding that never reports an active run. Replaced by the real
// RunGate when mutual exclusion is implemented.
type OpenGate struct{}

func (OpenGate) Active(context.Context) (bool, error) { return false, nil }

// PassthroughResolver is scaffolding that resolves each source repo to itself.
// Replaced by the real Resolver once trigger declarations are read.
type PassthroughResolver struct{}

func (PassthroughResolver) Resolve(_ context.Context, sources []string) ([]string, error) {
	return sources, nil
}

type Collector struct {
	mu       sync.Mutex
	repos    map[string]struct{}
	timer    *time.Timer
	window   time.Duration
	gate     RunGate
	resolver Resolver
	creator  JobCreator
	logger   *slog.Logger
}

func NewCollector(window time.Duration, gate RunGate, resolver Resolver, creator JobCreator, logger *slog.Logger) *Collector {
	return &Collector{
		repos:    make(map[string]struct{}),
		window:   window,
		gate:     gate,
		resolver: resolver,
		creator:  creator,
		logger:   logger,
	}
}

func (c *Collector) Add(repo string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.repos[repo] = struct{}{}
	c.logger.Debug("repo added to batch", "repo", repo, "batch_size", len(c.repos))

	if c.timer == nil {
		c.timer = time.AfterFunc(c.window, c.attemptFlush)
		c.logger.Debug("batch timer started", "window", c.window)
	}
}

// attemptFlush is the timer target. It consults the run gate; if a run is active
// it postpones (keeps the batch and re-arms) rather than creating an overlapping
// run, otherwise it drains the batch, resolves dependents, and creates one run.
func (c *Collector) attemptFlush() {
	// Invariant: c.timer stays non-nil for the whole gate-check window below, so
	// a concurrent Add sees a live timer and does not arm a competing one. It is
	// only cleared when we drain, or re-armed when we postpone.
	c.mu.Lock()
	if len(c.repos) == 0 {
		c.timer = nil
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), flushTimeout)
	defer cancel()

	active, err := c.gate.Active(ctx)
	if err != nil {
		c.logger.Warn("run gate check failed, postponing", "error", err)
		active = true
	}
	if active {
		c.mu.Lock()
		c.timer = time.AfterFunc(c.window, c.attemptFlush)
		c.mu.Unlock()
		c.logger.Info("renovate run active, postponing flush")
		return
	}

	c.mu.Lock()
	sources := make([]string, 0, len(c.repos))
	for r := range c.repos {
		sources = append(sources, r)
	}
	c.repos = make(map[string]struct{})
	c.timer = nil
	c.mu.Unlock()

	c.logger.Info("flushing batch", "sources", sources, "count", len(sources))

	dependents, err := c.resolver.Resolve(ctx, sources)
	if err != nil {
		c.logger.Error("failed to resolve dependents", "error", err, "sources", sources)
		return
	}
	if len(dependents) == 0 {
		c.logger.Info("batch resolved to zero dependents, no job created", "sources", sources)
		return
	}

	jobName, err := c.creator.CreateJobForRepos(ctx, dependents)
	if err != nil {
		c.logger.Error("failed to create job", "error", err, "dependents", dependents)
		return
	}
	c.logger.Info("batch job created", "job", jobName, "dependents", dependents)
}

func (c *Collector) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
}
