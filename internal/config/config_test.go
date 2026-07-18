package config

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeTestKey writes a valid PKCS#1 RSA private key to a temp file and returns
// its path.
func writeTestKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generating key: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	path := filepath.Join(t.TempDir(), "key.pem")
	if err := os.WriteFile(path, pemBytes, 0o600); err != nil {
		t.Fatalf("writing key: %v", err)
	}
	return path
}

func setEnv(t *testing.T, listenAddr, logLevel, secret, batchWindow, cronName, cronNs, clientID, keyFile string) {
	t.Helper()
	t.Setenv("RT_LISTEN_ADDR", listenAddr)
	t.Setenv("RT_LOG_LEVEL", logLevel)
	t.Setenv("RT_WEBHOOK_SECRET", secret)
	t.Setenv("RT_BATCH_WINDOW_SECONDS", batchWindow)
	t.Setenv("RT_CRONJOB_NAME", cronName)
	t.Setenv("RT_CRONJOB_NAMESPACE", cronNs)
	t.Setenv("RT_GITHUB_CLIENT_ID", clientID)
	t.Setenv("RT_GITHUB_APP_PRIVATE_KEY_FILE", keyFile)
}

func TestLoadValid(t *testing.T) {
	key := writeTestKey(t)
	setEnv(t, ":9090", "debug", "secret", "10", "renovate", "renovate-ns", "Iv23liABC", key)

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
	if cfg.GitHubClientID != "Iv23liABC" {
		t.Errorf("GitHubClientID = %q, want Iv23liABC", cfg.GitHubClientID)
	}
	if cfg.PrivateKey == nil {
		t.Error("PrivateKey = nil, want a parsed RSA key")
	}
}

func TestLoadDefaults(t *testing.T) {
	key := writeTestKey(t)
	setEnv(t, "", "", "secret", "", "renovate", "ns", "client", key)

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
	setEnv(t, "", "", "secret", "", "", "ns", "client", writeTestKey(t))
	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing RT_CRONJOB_NAME")
	}
}

func TestLoadMissingCronJobNamespace(t *testing.T) {
	setEnv(t, "", "", "secret", "", "renovate", "", "client", writeTestKey(t))
	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing RT_CRONJOB_NAMESPACE")
	}
}

func TestLoadMissingWebhookSecret(t *testing.T) {
	setEnv(t, "", "", "", "", "renovate", "ns", "client", writeTestKey(t))
	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing RT_WEBHOOK_SECRET")
	}
}

func TestLoadMissingClientID(t *testing.T) {
	setEnv(t, "", "", "secret", "", "renovate", "ns", "", writeTestKey(t))
	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing RT_GITHUB_CLIENT_ID")
	}
}

func TestLoadMissingPrivateKeyFile(t *testing.T) {
	setEnv(t, "", "", "secret", "", "renovate", "ns", "client", "")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing RT_GITHUB_APP_PRIVATE_KEY_FILE")
	}
}

func TestLoadInvalidLogLevel(t *testing.T) {
	setEnv(t, "", "verbose", "secret", "", "renovate", "ns", "client", writeTestKey(t))
	if _, err := Load(); err == nil {
		t.Fatal("expected error for invalid log level")
	}
}

func TestLoadUnparseablePrivateKey(t *testing.T) {
	badKey := filepath.Join(t.TempDir(), "bad.pem")
	if err := os.WriteFile(badKey, []byte("not a pem key"), 0o600); err != nil {
		t.Fatalf("writing bad key: %v", err)
	}
	setEnv(t, "", "", "secret", "", "renovate", "ns", "client", badKey)
	if _, err := Load(); err == nil {
		t.Fatal("expected error for unparseable private key")
	}
}

func TestLoadMissingPrivateKeyFileOnDisk(t *testing.T) {
	setEnv(t, "", "", "secret", "", "renovate", "ns", "client", filepath.Join(t.TempDir(), "nope.pem"))
	if _, err := Load(); err == nil {
		t.Fatal("expected error for private key file that does not exist")
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
