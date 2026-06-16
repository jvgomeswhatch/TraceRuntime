package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/runtime-platform/services/watchdog/internal/config"
	"github.com/runtime-platform/services/watchdog/internal/db"
	"github.com/runtime-platform/services/watchdog/internal/detector"
	"github.com/runtime-platform/services/watchdog/internal/publisher"
	"github.com/runtime-platform/services/watchdog/internal/telemetry"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg := config.Load()

	shutdown, err := telemetry.Init(context.Background(), "traceruntime-watchdog")
	if err != nil {
		slog.Error("failed to init tracer", "error", err)
		os.Exit(1)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(ctx); err != nil {
			slog.Error("tracer shutdown error", "error", err)
		}
	}()

	database, err := db.Open(context.Background(), cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{
		Addr:         ":9093",
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("watchdog server starting", "addr", ":9093")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("watchdog server error", "error", err)
			os.Exit(1)
		}
	}()

	// --- AWS / SQS client ---
	awsCfg, err := awsconfig.LoadDefaultConfig(context.Background())
	if err != nil {
		slog.Error("failed to load AWS config", "error", err)
		os.Exit(1)
	}
	sqsClient := sqs.NewFromConfig(awsCfg)

	// --- SSE publisher ---
	pub := publisher.NewSSEPublisher(cfg.APIEventsURL, cfg.InternalToken)

	// --- Detection engine ---
	det := detector.New(&cfg, database, pub, sqsClient)
	go det.Run(ctx)

	slog.Info("watchdog started",
		"poll_interval", cfg.PollIntervalSeconds,
		"heartbeat_stale", cfg.HeartbeatStaleSeconds,
		"healthcheck_failures", cfg.HealthcheckFailures,
		"queue_lag_threshold", cfg.QueueLagThreshold,
		"task_stuck_seconds", cfg.TaskStuckSeconds,
	)

	<-ctx.Done()

	slog.Info("watchdog shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		slog.Error("watchdog server shutdown error", "error", err)
	}
}
