package config

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	ListenAddr     string
	LogLevel       string
	WebhookSecret  string
	BatchWindow    time.Duration
	CronJobName    string
	CronJobNs      string
	GitHubClientID string
	PrivateKeyFile string
	PrivateKey     *rsa.PrivateKey
}

func Load() (Config, error) {
	batchSec, _ := strconv.Atoi(envOrDefault("RT_BATCH_WINDOW_SECONDS", "30"))
	if batchSec <= 0 {
		batchSec = 30
	}

	cfg := Config{
		ListenAddr:     envOrDefault("RT_LISTEN_ADDR", ":8080"),
		LogLevel:       envOrDefault("RT_LOG_LEVEL", "info"),
		WebhookSecret:  os.Getenv("RT_WEBHOOK_SECRET"),
		BatchWindow:    time.Duration(batchSec) * time.Second,
		CronJobName:    os.Getenv("RT_CRONJOB_NAME"),
		CronJobNs:      os.Getenv("RT_CRONJOB_NAMESPACE"),
		GitHubClientID: os.Getenv("RT_GITHUB_CLIENT_ID"),
		PrivateKeyFile: os.Getenv("RT_GITHUB_APP_PRIVATE_KEY_FILE"),
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}

	key, err := parsePrivateKey(cfg.PrivateKeyFile)
	if err != nil {
		return Config{}, err
	}
	cfg.PrivateKey = key

	return cfg, nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func (c Config) validate() error {
	if c.CronJobName == "" {
		return fmt.Errorf("RT_CRONJOB_NAME is required")
	}
	if c.CronJobNs == "" {
		return fmt.Errorf("RT_CRONJOB_NAMESPACE is required")
	}
	if c.WebhookSecret == "" {
		return fmt.Errorf("RT_WEBHOOK_SECRET is required")
	}
	if c.GitHubClientID == "" {
		return fmt.Errorf("RT_GITHUB_CLIENT_ID is required")
	}
	if c.PrivateKeyFile == "" {
		return fmt.Errorf("RT_GITHUB_APP_PRIVATE_KEY_FILE is required")
	}
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[strings.ToLower(c.LogLevel)] {
		return fmt.Errorf("RT_LOG_LEVEL must be one of: debug, info, warn, error")
	}
	return nil
}

// parsePrivateKey reads a PEM-encoded RSA private key (PKCS#1 or PKCS#8) from
// path. It fails loudly so a missing or malformed key crash-loops the process
// at boot rather than surfacing as a silent flush failure later.
func parsePrivateKey(path string) (*rsa.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading private key %s: %w", path, err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("private key %s is not valid PEM", path)
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parsing private key %s: %w", path, err)
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key %s is not an RSA key", path)
	}
	return key, nil
}

func (c Config) SlogLevel() slog.Level {
	switch strings.ToLower(c.LogLevel) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
