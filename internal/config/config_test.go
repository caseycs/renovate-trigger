package config

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateValid(t *testing.T) {
	cfg := Config{
		ListenAddr:         ":8080",
		LogLevel:           "info",
		WebhookSecret:      "secret",
		BatchWindowSeconds: 30,
		CronJob:            CronJobRef{Name: "renovate", Namespace: "renovate"},
		Repos:              []string{"org/repo"},
	}
	if err := cfg.validate(); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateMissingCronjobName(t *testing.T) {
	cfg := Config{
		LogLevel:           "info",
		WebhookSecret:      "secret",
		BatchWindowSeconds: 30,
		CronJob:            CronJobRef{Namespace: "renovate"},
		Repos:              []string{"org/repo"},
	}
	if err := cfg.validate(); err == nil {
		t.Fatal("expected error for missing cronjob name")
	}
}

func TestValidateMissingCronjobNamespace(t *testing.T) {
	cfg := Config{
		LogLevel:           "info",
		WebhookSecret:      "secret",
		BatchWindowSeconds: 30,
		CronJob:            CronJobRef{Name: "renovate"},
		Repos:              []string{"org/repo"},
	}
	if err := cfg.validate(); err == nil {
		t.Fatal("expected error for missing cronjob namespace")
	}
}

func TestValidateEmptyRepos(t *testing.T) {
	cfg := Config{
		LogLevel:           "info",
		WebhookSecret:      "secret",
		BatchWindowSeconds: 30,
		CronJob:            CronJobRef{Name: "renovate", Namespace: "renovate"},
		Repos:              []string{},
	}
	if err := cfg.validate(); err == nil {
		t.Fatal("expected error for empty repos")
	}
}

func TestValidateMissingWebhookSecret(t *testing.T) {
	cfg := Config{
		LogLevel:           "info",
		BatchWindowSeconds: 30,
		CronJob:            CronJobRef{Name: "renovate", Namespace: "renovate"},
		Repos:              []string{"org/repo"},
	}
	if err := cfg.validate(); err == nil {
		t.Fatal("expected error for missing webhook secret")
	}
}

func TestValidateInvalidBatchWindow(t *testing.T) {
	cfg := Config{
		LogLevel:           "info",
		WebhookSecret:      "secret",
		BatchWindowSeconds: 0,
		CronJob:            CronJobRef{Name: "renovate", Namespace: "renovate"},
		Repos:              []string{"org/repo"},
	}
	if err := cfg.validate(); err == nil {
		t.Fatal("expected error for invalid batch window")
	}
}

func TestValidateInvalidLogLevel(t *testing.T) {
	cfg := Config{
		LogLevel:           "verbose",
		WebhookSecret:      "secret",
		BatchWindowSeconds: 30,
		CronJob:            CronJobRef{Name: "renovate", Namespace: "renovate"},
		Repos:              []string{"org/repo"},
	}
	if err := cfg.validate(); err == nil {
		t.Fatal("expected error for invalid log level")
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

func TestLoadFromFile(t *testing.T) {
	dir := t.TempDir()
	configContent := `
listenAddr: ":9090"
logLevel: "debug"
webhookSecret: "test-secret"
batchWindowSeconds: 10
cronjob:
  name: "my-renovate"
  namespace: "my-ns"
repos:
  - "org/repo-1"
  - "org/repo-2"
`
	err := os.WriteFile(filepath.Join(dir, "renovate-trigger.yaml"), []byte(configContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

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
	if cfg.CronJob.Name != "my-renovate" {
		t.Errorf("CronJob.Name = %q, want my-renovate", cfg.CronJob.Name)
	}
	if len(cfg.Repos) != 2 {
		t.Errorf("len(Repos) = %d, want 2", len(cfg.Repos))
	}
}

func TestLoadEnvOverride(t *testing.T) {
	dir := t.TempDir()
	configContent := `
listenAddr: ":8080"
logLevel: "info"
webhookSecret: "file-secret"
batchWindowSeconds: 30
cronjob:
  name: "renovate"
  namespace: "renovate"
repos:
  - "org/repo"
`
	err := os.WriteFile(filepath.Join(dir, "renovate-trigger.yaml"), []byte(configContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	origDir, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(origDir)

	t.Setenv("RT_LISTENADDR", ":9999")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error: %v", err)
	}

	if cfg.ListenAddr != ":9999" {
		t.Errorf("ListenAddr = %q, want :9999 (env override)", cfg.ListenAddr)
	}
}
