package batch

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/ilia/renovate-trigger/internal/graph"
)

// flushTimeout bounds the gate check, resolution, and job creation of a single
// flush.
const flushTimeout = 30 * time.Second

// postponeWarnThreshold is the number of consecutive postpones after which the
// collector warns that a Renovate run may be stuck.
const postponeWarnThreshold = 10

// RunGate reports whether a Renovate run is already active, so the collector can
// avoid overlapping runs. Its full behaviour lands with mutual exclusion.
type RunGate interface {
	Active(ctx context.Context) (bool, error)
}

// Resolver expands a batch of Dependencies into the deduplicated set of Dependents
// to run Renovate on.
type Resolver interface {
	Resolve(ctx context.Context, deps []graph.Dependency) ([]string, error)
}

// JobCreator creates a single Renovate run for the given target repos.
type JobCreator interface {
	CreateJobForRepos(ctx context.Context, repos []string) (string, error)
}

type Collector struct {
	mu            sync.Mutex
	deps          map[graph.Dependency]struct{}
	timer         *time.Timer
	postponements int
	window        time.Duration
	gate          RunGate
	resolver      Resolver
	creator       JobCreator
	logger        *slog.Logger
}

func NewCollector(window time.Duration, gate RunGate, resolver Resolver, creator JobCreator, logger *slog.Logger) *Collector {
	return &Collector{
		deps:     make(map[graph.Dependency]struct{}),
		window:   window,
		gate:     gate,
		resolver: resolver,
		creator:  creator,
		logger:   logger,
	}
}

// Add puts a Dependency into the current Batch, starting the Window if this is the
// first one. Dependencies dedupe on Kind and Name, so the same ECR repository
// pushed under several tags collapses to a single entry.
func (c *Collector) Add(dep graph.Dependency) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.deps[dep] = struct{}{}
	c.logger.Debug("dependency added to batch", "dependency", dep, "batch_size", len(c.deps))

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
	if len(c.deps) == 0 {
		c.timer = nil
		c.postponements = 0
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
		c.postponements++
		n := c.postponements
		c.timer = time.AfterFunc(c.window, c.attemptFlush)
		c.mu.Unlock()
		if n == postponeWarnThreshold {
			c.logger.Warn("flush postponed repeatedly; a Renovate run may be stuck", "postponements", n)
		} else {
			c.logger.Info("Renovate run active, postponing flush", "postponements", n)
		}
		return
	}

	c.mu.Lock()
	deps := make([]graph.Dependency, 0, len(c.deps))
	for d := range c.deps {
		deps = append(deps, d)
	}
	c.deps = make(map[graph.Dependency]struct{})
	c.timer = nil
	c.postponements = 0
	c.mu.Unlock()

	c.logger.Info("flushing batch", "dependencies", deps, "count", len(deps))

	dependents, err := c.resolver.Resolve(ctx, deps)
	if err != nil {
		c.logger.Error("failed to resolve dependents", "error", err, "dependencies", deps)
		return
	}
	if len(dependents) == 0 {
		c.logger.Info("batch resolved to zero dependents, no job created", "dependencies", deps)
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
