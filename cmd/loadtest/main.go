package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

// Config holds all load test parameters.
type Config struct {
	Tasks       int
	Rate        float64
	Concurrency int
	APIURL      string
	DBURL       string
	SQSURL      string
	OutputDir   string
}

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg := Config{}

	flag.IntVar(&cfg.Tasks, "tasks", 50, "total number of tasks to send")
	flag.Float64Var(&cfg.Rate, "rate", 2.0, "tasks per second")
	flag.IntVar(&cfg.Concurrency, "concurrency", 4, "max concurrent in-flight requests")
	flag.StringVar(&cfg.APIURL, "api-url", "http://localhost:8082", "base URL of the API service")
	flag.StringVar(&cfg.DBURL, "db-url",
		envOrDefault("DATABASE_URL", "postgres://traceruntime:traceruntime@localhost:5432/traceruntime?sslmode=disable"),
		"PostgreSQL connection string")
	flag.StringVar(&cfg.SQSURL, "sqs-url",
		envOrDefault("SQS_QUEUE_URL", "http://localhost:4566/000000000000/traceruntime-tasks"),
		"SQS queue URL for verification")
	flag.StringVar(&cfg.OutputDir, "output-dir", "results", "directory to write result JSON files")
	flag.Parse()

	slog.Info("loadtest configured",
		"tasks", cfg.Tasks,
		"rate", cfg.Rate,
		"concurrency", cfg.Concurrency,
		"api_url", cfg.APIURL,
		"db_url", cfg.DBURL,
		"sqs_url", cfg.SQSURL,
		"output_dir", cfg.OutputDir,
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, cfg); err != nil {
		slog.Error("loadtest failed", "error", err)
		os.Exit(1)
	}

	slog.Info("loadtest finished")
}

func run(ctx context.Context, cfg Config) error {
	start := time.Now()

	qm := &QueueMetrics{}
	queueCtx, queueCancel := context.WithCancel(ctx)
	defer queueCancel()
	go trackQueueMetrics(queueCtx, cfg.SQSURL, qm)

	submissions, err := sendTasks(ctx, cfg)
	if err != nil && ctx.Err() == nil {
		return fmt.Errorf("send tasks: %w", err)
	}

	successCount := 0
	for _, s := range submissions {
		if s.Error == nil {
			successCount++
		}
	}
	slog.Info("submission phase complete", "success", successCount, "errors", len(submissions)-successCount)

	if successCount == 0 {
		return fmt.Errorf("no tasks were successfully submitted")
	}

	waitTimeout := 10 * time.Minute
	collected, err := collectResults(ctx, cfg.DBURL, submissions, waitTimeout)
	if err != nil {
		return fmt.Errorf("collect results: %w", err)
	}

	queueCancel()
	time.Sleep(1 * time.Second)

	converged := checkBacklogConverged(ctx, cfg.SQSURL)

	duration := time.Since(start)
	report := buildReport(cfg, duration, submissions, collected, qm, converged)

	printSummary(report)

	path, err := writeReport(report, cfg.OutputDir)
	if err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	slog.Info("report written", "path", path)

	return nil
}

// envOrDefault returns the value of the environment variable named by key,
// or def if the variable is not set.
func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
