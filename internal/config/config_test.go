package config

import (
	"log/slog"
	"testing"
	"time"
)

func setEnv(t *testing.T, listenAddr, logLevel, secret, batchWindow, cronName, cronNs, repos string) {
	t.Helper()
	t.Setenv("RT_LISTEN_ADDR", listenAddr)
	t.Setenv("RT_LOG_LEVEL", logLevel)
	t.Setenv("RT_WEBHOOK_SECRET", secret)
	t.Setenv("RT_BATCH_WINDOW_SECONDS", batchWindow)
	t.Setenv("RT_CRONJOB_NAME", cronName)
	t.Setenv("RT_CRONJOB_NAMESPACE", cronNs)
	t.Setenv("RT_REPOS", repos)
}

func TestLoadValid(t *testing.T) {
	setEnv(t, ":9090", "debug", "secret", "10", "renovate", "renovate-ns", "org/a,org/b")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.ListenAddr != ":9090" {
		t.Errorf("ListenAddr = %q, want :9090", cfg.ListenAddr)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want debug", cfg.LogLevel)
	}
	if cfg.BatchWindow != 10*time.Second {
		t.Errorf("BatchWindow = %v, want 10s", cfg.BatchWindow)
	}
	if cfg.CronJobName != "renovate" {
		t.Errorf("CronJobName = %q, want renovate", cfg.CronJobName)
	}
	if cfg.CronJobNs != "renovate-ns" {
		t.Errorf("CronJobNs = %q, want renovate-ns", cfg.CronJobNs)
	}
	if len(cfg.Repos) != 2 || cfg.Repos[0] != "org/a" || cfg.Repos[1] != "org/b" {
		t.Errorf("Repos = %v, want [org/a org/b]", cfg.Repos)
	}
}

func TestLoadDefaults(t *testing.T) {
	setEnv(t, "", "", "secret", "", "renovate", "ns", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}
	if cfg.ListenAddr != ":8080" {
		t.Errorf("ListenAddr = %q, want :8080 (default)", cfg.ListenAddr)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want info (default)", cfg.LogLevel)
	}
	if cfg.BatchWindow != 30*time.Second {
		t.Errorf("BatchWindow = %v, want 30s (default)", cfg.BatchWindow)
	}
}

func TestLoadMissingCronJobName(t *testing.T) {
	setEnv(t, "", "", "secret", "", "", "ns", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing RT_CRONJOB_NAME")
	}
}

func TestLoadMissingCronJobNamespace(t *testing.T) {
	setEnv(t, "", "", "secret", "", "renovate", "", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing RT_CRONJOB_NAMESPACE")
	}
}

func TestLoadMissingWebhookSecret(t *testing.T) {
	setEnv(t, "", "", "", "", "renovate", "ns", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing RT_WEBHOOK_SECRET")
	}
}

func TestLoadInvalidLogLevel(t *testing.T) {
	setEnv(t, "", "verbose", "secret", "", "renovate", "ns", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid log level")
	}
}

func TestLoadEmptyReposAccepted(t *testing.T) {
	setEnv(t, "", "", "secret", "", "renovate", "ns", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("empty repos should be valid: %v", err)
	}
	if len(cfg.Repos) != 0 {
		t.Errorf("Repos = %v, want empty", cfg.Repos)
	}
}

func TestParseRepos(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"", nil},
		{"org/a", []string{"org/a"}},
		{"org/a,org/b", []string{"org/a", "org/b"}},
		{" org/a , org/b , org/c ", []string{"org/a", "org/b", "org/c"}},
		{"org/a,,org/b", []string{"org/a", "org/b"}},
	}
	for _, tt := range tests {
		got := parseRepos(tt.input)
		if len(got) != len(tt.expected) {
			t.Errorf("parseRepos(%q) = %v, want %v", tt.input, got, tt.expected)
			continue
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Errorf("parseRepos(%q)[%d] = %q, want %q", tt.input, i, got[i], tt.expected[i])
			}
		}
	}
}

func TestSlogLevel(t *testing.T) {
	tests := []struct {
		level    string
		expected slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"INFO", slog.LevelInfo},
	}
	for _, tt := range tests {
		cfg := Config{LogLevel: tt.level}
		if got := cfg.SlogLevel(); got != tt.expected {
			t.Errorf("SlogLevel(%q) = %v, want %v", tt.level, got, tt.expected)
		}
	}
}

func TestRepoSet(t *testing.T) {
	cfg := Config{Repos: []string{"org/a", "org/b"}}
	set := cfg.RepoSet()
	if _, ok := set["org/a"]; !ok {
		t.Error("expected org/a in set")
	}
	if _, ok := set["org/b"]; !ok {
		t.Error("expected org/b in set")
	}
	if _, ok := set["org/c"]; ok {
		t.Error("unexpected org/c in set")
	}
}

func TestRepoSetEmpty(t *testing.T) {
	cfg := Config{Repos: nil}
	set := cfg.RepoSet()
	if len(set) != 0 {
		t.Errorf("expected empty set, got %v", set)
	}
}
