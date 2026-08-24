package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/sqs"

	"github.com/ilia/renovate-trigger/internal/awsecr"
	"github.com/ilia/renovate-trigger/internal/batch"
	"github.com/ilia/renovate-trigger/internal/config"
	"github.com/ilia/renovate-trigger/internal/ghapp"
	"github.com/ilia/renovate-trigger/internal/graph"
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

	ghClient := ghapp.NewClient(cfg.GitHubClientID, cfg.PrivateKey)

	// One fetcher per Dependency kind. GitHub is always present; ECR is added
	// below only when a queue is configured, so a Dependency kind that cannot be
	// ingested also cannot be resolved.
	fetchers := map[graph.Kind]resolve.DependentsFetcher{
		graph.KindGitHub: ghClient,
	}

	gate := k8s.NewRunGate(k8sClient, cfg.CronJobName, cfg.CronJobNs, logger)

	// The collector is built before the consumer because the consumer adds to it,
	// but after the fetchers map because the resolver reads it — so the ECR
	// clients have to be constructed in between.
	var sqsClient *sqs.Client
	if cfg.ECREnabled() {
		awsCfg, err := loadAWSConfig(startupCtx, cfg)
		if err != nil {
			cancelStartup()
			logger.Error("failed to load aws config", "error", err)
			os.Exit(1)
		}
		if awsCfg.Region == "" {
			cancelStartup()
			logger.Error("aws region could not be resolved; set RT_AWS_REGION or AWS_REGION")
			os.Exit(1)
		}
		fetchers[graph.KindECR] = awsecr.NewTagReader(ecr.NewFromConfig(awsCfg), cfg.ECRDependentsTag)
		sqsClient = sqs.NewFromConfig(awsCfg)
	}

	resolver := resolve.New(fetchers, logger)
	collector := batch.NewCollector(cfg.BatchWindow, gate, resolver, jobCreator, logger)
	defer collector.Stop()

	var consumer *awsecr.Consumer
	if sqsClient != nil {
		consumer = awsecr.NewConsumer(sqsClient, cfg.ECRQueueURL, collector, logger)
		// Fail loud at boot, exactly as the CronJob check above does: a wrong
		// queue URL or a missing IAM policy should crash-loop the pod rather than
		// leave it deployed and silently ingesting nothing.
		if err := consumer.Verify(startupCtx); err != nil {
			cancelStartup()
			logger.Error("ecr event queue not reachable", "error", err)
			os.Exit(1)
		}
	}
	cancelStartup()

	handler := webhook.NewHandler(cfg.WebhookSecret, collector, logger)
	srv := server.New(cfg.ListenAddr, handler, k8sClient, logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Closed when the consumer has stopped. Pre-closed when ECR is disabled, so
	// the shutdown path below needs no special case.
	consumerDone := make(chan struct{})
	if consumer != nil {
		go func() {
			defer close(consumerDone)
			consumer.Run(ctx)
		}()
	} else {
		close(consumerDone)
	}

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
		"batch_window", cfg.BatchWindow,
		"ecr_enabled", consumer != nil)

	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", "error", err)
	}

	select {
	case <-consumerDone:
	case <-shutdownCtx.Done():
		logger.Warn("ecr consumer did not stop within the shutdown timeout")
	}

	logger.Info("renovate-trigger stopped")
}

// loadAWSConfig resolves credentials and region from the SDK's default chain,
// which under EKS Pod Identity means the container credentials endpoint the Pod
// Identity Agent injects. RT_AWS_REGION overrides region resolution when set;
// there are no static-credential env vars by design.
func loadAWSConfig(ctx context.Context, cfg config.Config) (aws.Config, error) {
	opts := []func(*awsconfig.LoadOptions) error{}
	if cfg.AWSRegion != "" {
		opts = append(opts, awsconfig.WithRegion(cfg.AWSRegion))
	}
	return awsconfig.LoadDefaultConfig(ctx, opts...)
}
