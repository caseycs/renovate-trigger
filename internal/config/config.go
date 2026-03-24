package config

import (
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/spf13/viper"
)

type CronJobRef struct {
	Name      string `mapstructure:"name"`
	Namespace string `mapstructure:"namespace"`
}

type Config struct {
	ListenAddr         string        `mapstructure:"listenAddr"`
	LogLevel           string        `mapstructure:"logLevel"`
	WebhookSecret      string        `mapstructure:"webhookSecret"`
	BatchWindowSeconds int           `mapstructure:"batchWindowSeconds"`
	CronJob            CronJobRef    `mapstructure:"cronjob"`
	Repos              []string      `mapstructure:"repos"`
	BatchWindow        time.Duration `mapstructure:"-"`
}

func Load() (Config, error) {
	v := viper.New()
	v.SetConfigName("renovate-trigger")
	v.SetConfigType("yaml")
	v.AddConfigPath("/etc/renovate-trigger")
	v.AddConfigPath(".")

	v.SetEnvPrefix("RT")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	v.SetDefault("listenAddr", ":8080")
	v.SetDefault("logLevel", "info")
	v.SetDefault("batchWindowSeconds", 30)

	// Config file is optional — env vars can provide everything
	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return Config{}, fmt.Errorf("reading config file: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return Config{}, fmt.Errorf("unmarshaling config: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return Config{}, err
	}

	cfg.BatchWindow = time.Duration(cfg.BatchWindowSeconds) * time.Second

	return cfg, nil
}

func (c Config) validate() error {
	if c.CronJob.Name == "" {
		return fmt.Errorf("cronjob.name is required")
	}
	if c.CronJob.Namespace == "" {
		return fmt.Errorf("cronjob.namespace is required")
	}
	if len(c.Repos) == 0 {
		return fmt.Errorf("repos must contain at least one entry")
	}
	if c.WebhookSecret == "" {
		return fmt.Errorf("webhookSecret is required (set RT_WEBHOOKSECRET env var)")
	}
	if c.BatchWindowSeconds <= 0 {
		return fmt.Errorf("batchWindowSeconds must be > 0")
	}
	validLevels := map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
	if !validLevels[strings.ToLower(c.LogLevel)] {
		return fmt.Errorf("logLevel must be one of: debug, info, warn, error")
	}
	return nil
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

func (c Config) RepoSet() map[string]struct{} {
	set := make(map[string]struct{}, len(c.Repos))
	for _, r := range c.Repos {
		set[r] = struct{}{}
	}
	return set
}
