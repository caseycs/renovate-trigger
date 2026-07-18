package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ilia/renovate-trigger/internal/batch"
	"github.com/ilia/renovate-trigger/internal/config"
	"github.com/ilia/renovate-trigger/internal/ghapp"
	"github.com/ilia/renovate-trigger/internal/k8s"
	"github.com/ilia/renovate-trigger/internal/resolve"
	"github.com/ilia/renovate-trigger/internal/server"
	"github.com/ilia/renovate-trigger/internal/webhook"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: cfg.SlogLevel(),
	}))

	k8sClient, err := k8s.NewInClusterClient()
	if err != nil {
		logger.Error("failed to create kubernetes client", "error", err)
		os.Exit(1)
	}

	jobCreator := k8s.NewJobCreator(k8sClient, cfg.CronJobName, cfg.CronJobNs, logger)

	startupCtx, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	if err := jobCreator.Verify(startupCtx); err != nil {
		cancelStartup()
		logger.Error("source cronjob not found", "error", err)
		os.Exit(1)
	}
	cancelStartup()

	ghClient := ghapp.NewClient(cfg.GitHubClientID, cfg.PrivateKey)
	resolver := resolve.New(ghClient, logger)
	gate := k8s.NewRunGate(k8sClient, cfg.CronJobName, cfg.CronJobNs, logger)

	collector := batch.NewCollector(cfg.BatchWindow, gate, resolver, jobCreator, logger)
	defer collector.Stop()

	handler := webhook.NewHandler(cfg.WebhookSecret, collector, logger)
	srv := server.New(cfg.ListenAddr, handler, k8sClient, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := srv.Start(); err != nil {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	logger.Info("renovate-trigger started",
		"addr", cfg.ListenAddr,
		"cronjob", cfg.CronJobName,
		"cronjob_namespace", cfg.CronJobNs,
		"batch_window", cfg.BatchWindow)

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", "error", err)
	}

	logger.Info("renovate-trigger stopped")
}
