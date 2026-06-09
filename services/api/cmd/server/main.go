package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	sqssdk "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/runtime-platform/services/api/internal/ai"
	"github.com/runtime-platform/services/api/internal/db"
	"github.com/runtime-platform/services/api/internal/event"
	apihttp "github.com/runtime-platform/services/api/internal/http"
	"github.com/runtime-platform/services/api/internal/metrics"
	"github.com/runtime-platform/services/api/internal/queue"
	"github.com/runtime-platform/services/api/internal/telemetry"
	"github.com/runtime-platform/services/api/internal/worker"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	shutdown, err := telemetry.Init(context.Background(), "traceruntime-api")
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

	database, err := db.Open(context.Background(), mustEnv("DATABASE_URL"))
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	broker := event.NewBroker()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	aiRuntimeURL := envString("AI_RUNTIME_URL", "http://localhost:8001")

	var publisher apihttp.Publisher
	var q *queue.Queue

	queueBackend := envString("QUEUE_BACKEND", "inmemory")
	switch queueBackend {
	case "sqs":
		awsCfg, err := awsconfig.LoadDefaultConfig(context.Background())
		if err != nil {
			slog.Error("failed to load AWS config", "error", err)
			os.Exit(1)
		}
		sqsClient := sqssdk.NewFromConfig(awsCfg)
		sqsQueueURL := mustEnv("SQS_QUEUE_URL")
		publisher = apihttp.NewSQSPublisher(sqsClient, sqsQueueURL)
		q = queue.NewQueue(1)
		metrics.MustRegisterAll(func() float64 { return 0 })
		slog.Info("queue backend: sqs", "queue_url", sqsQueueURL)
	default:
		q = queue.NewQueue(envInt("QUEUE_CAPACITY", 128))
		metrics.MustRegisterAll(func() float64 { return float64(q.Depth()) })
		publisher = apihttp.NewQueuePublisher(q)
		aiClient := ai.NewClient(aiRuntimeURL, 120*time.Second)
		w := worker.New(q, broker, aiClient)
		go w.Run(ctx)
		slog.Info("queue backend: inmemory")
	}

	router := apihttp.NewRouter(broker, publisher, q, database)

	port := envString("PORT", "8082")
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0,
		IdleTimeout:  120 * time.Second,
	}

	slog.Info("api server starting", "addr", ":"+port, "queue_backend", queueBackend, "ai_runtime_url", aiRuntimeURL)

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down server")

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		slog.Error("server shutdown error", "error", err)
	}
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envString(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("required env var not set", "key", key)
		os.Exit(1)
	}
	return v
}
