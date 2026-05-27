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

	"github.com/runtime-platform/services/api/internal/ai"
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

	queueCapacity := envInt("QUEUE_CAPACITY", 128)

	broker := event.NewBroker()
	q := queue.NewQueue(queueCapacity)

	metrics.MustRegisterAll(func() float64 { return float64(q.Depth()) })

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	aiRuntimeURL := os.Getenv("AI_RUNTIME_URL")
	if aiRuntimeURL == "" {
		aiRuntimeURL = "http://localhost:8001"
	}
	aiClient := ai.NewClient(aiRuntimeURL, 120*time.Second)
	w := worker.New(q, broker, aiClient)
	go w.Run(ctx)

	router := apihttp.NewRouter(broker, q)

	port := envString("PORT", "8082")
	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      router,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 0, // SSE requires no write timeout
		IdleTimeout:  120 * time.Second,
	}

	slog.Info("api server starting", "addr", ":"+port, "queue_capacity", queueCapacity, "ai_runtime_url", aiRuntimeURL)

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
