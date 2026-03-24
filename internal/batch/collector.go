package batch

import (
	"log/slog"
	"sync"
	"time"
)

type FlushFunc func(repos []string)

type Collector struct {
	mu      sync.Mutex
	repos   map[string]struct{}
	timer   *time.Timer
	window  time.Duration
	onFlush FlushFunc
	logger  *slog.Logger
}

func NewCollector(window time.Duration, onFlush FlushFunc, logger *slog.Logger) *Collector {
	return &Collector{
		repos:   make(map[string]struct{}),
		window:  window,
		onFlush: onFlush,
		logger:  logger,
	}
}

func (c *Collector) Add(repo string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.repos[repo] = struct{}{}
	c.logger.Debug("repo added to batch", "repo", repo, "batch_size", len(c.repos))

	if c.timer == nil {
		c.timer = time.AfterFunc(c.window, c.flush)
		c.logger.Debug("batch timer started", "window", c.window)
	}
}

func (c *Collector) flush() {
	c.mu.Lock()
	if len(c.repos) == 0 {
		c.timer = nil
		c.mu.Unlock()
		return
	}

	repos := make([]string, 0, len(c.repos))
	for r := range c.repos {
		repos = append(repos, r)
	}
	c.repos = make(map[string]struct{})
	c.timer = nil
	c.mu.Unlock()

	c.logger.Info("flushing batch", "repos", repos, "count", len(repos))
	c.onFlush(repos)
}

func (c *Collector) Stop() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
}
