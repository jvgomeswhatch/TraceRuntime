package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/runtime-platform/cmd/chaos/internal/chaos"
	"github.com/runtime-platform/cmd/chaos/internal/chaos/checker"
	"github.com/runtime-platform/cmd/chaos/internal/chaos/docker"
	"github.com/runtime-platform/cmd/chaos/internal/chaos/scenarios"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg := chaos.ParseConfig()
	if err := cfg.Validate(); err != nil {
		slog.Error("invalid config", "error", err)
		os.Exit(1)
	}

	slog.Info("chaos runner configured",
		"api_url", cfg.APIURL,
		"ai_runtime_url", cfg.AIRuntimeURL,
		"worker_url", cfg.WorkerURL,
		"output_dir", cfg.OutputDir,
		"timeout", cfg.GlobalTimeout,
		"scenario", cfg.Scenario,
		"list", cfg.List,
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg); err != nil {
		slog.Error("chaos runner failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg chaos.Config) error {
	dc, err := docker.NewController("traceruntime", docker.RetryPolicy{MaxAttempts: 3, Backoff: time.Second})
	if err != nil {
		return fmt.Errorf("docker controller: %w", err)
	}
	defer dc.Close()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	if err != nil {
		return fmt.Errorf("aws config: %w", err)
	}
	sqsClient := sqs.NewFromConfig(awsCfg, func(o *sqs.Options) {
		o.BaseEndpoint = aws.String(cfg.SQSEndpoint)
	})

	chk := checker.New(pool, dc, sqsClient, cfg.APIURL, cfg.WorkerURL, cfg.AIRuntimeURL, cfg.SQSQueueURL, cfg.SQSDlqURL)

	requiredContainers := []string{"api", "worker", "watchdog", "postgres", "localstack"}
	if cfg.InternalToken != "" {
		requiredContainers = append(requiredContainers, "ai-runtime")
	}

	preflight := chaos.Preflight(ctx, chk, requiredContainers)
	if !preflight.Passed {
		return fmt.Errorf("preflight failed: %v", preflight.Errors)
	}
	slog.Info("preflight passed")

	allScenarios := []chaos.Scenario{
		&scenarios.QueueFlood{},
		&scenarios.SlowInference{},
		&scenarios.AIFailure{},
		&scenarios.RuntimeHang{},
		&scenarios.WorkerCrash{},
		&scenarios.PostgresFailure{},
	}

	runner := chaos.NewRunner(cfg, allScenarios)
	sc := &chaos.ScenarioContext{
		RunID:   fmt.Sprintf("chaos-%s", time.Now().Format("20060102-150405")),
		Config:  cfg,
		Checker: chk,
		Docker:  dc,
	}

	suite, err := runner.Run(ctx, sc)
	if err != nil {
		return err
	}

	slog.Info("chaos suite complete",
		"total", suite.Summary.Total,
		"passed", suite.Summary.Passed,
		"warned", suite.Summary.Warned,
		"failed", suite.Summary.Failed,
	)

	if suite.Summary.Failed > 0 {
		return fmt.Errorf("%d scenario(s) FAILED", suite.Summary.Failed)
	}
	return nil
}
