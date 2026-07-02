package http

import (
	"net/http"
	"os"
	"path/filepath"
	"slices"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	sqssdk "github.com/aws/aws-sdk-go-v2/service/sqs"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/runtime-platform/services/api/internal/db"
	"github.com/runtime-platform/services/api/internal/event"
	"github.com/runtime-platform/services/api/internal/handlers"
	"github.com/runtime-platform/services/api/internal/queue"
	"github.com/runtime-platform/services/api/internal/tempo"
)

func NewRouter(broker *event.Broker, publisher Publisher, q *queue.Queue, database *db.DB, resultsDir string, allowedOrigins []string, internalToken string, sqsClient *sqssdk.Client, sqsQueueURL string) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware(allowedOrigins))
	r.Use(otelMiddleware)
	r.Use(RateLimitMiddleware())

	r.Get("/health", Health)
	r.Get("/metrics", NewMetricsHandler(broker, q).ServeHTTP)
	r.Get("/events", NewSSEHandler(broker, allowedOrigins).ServeHTTP)
	r.Post("/tasks", NewTaskHandler(broker, publisher, database).ServeHTTP)
	r.Options("/tasks", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	tempoURL := os.Getenv("TEMPO_URL")
	var tempoClient *tempo.Client
	if tempoURL != "" {
		tempoClient = tempo.New(tempoURL)
	}

	tokenMw := internalTokenMiddleware(internalToken)
	r.Post("/internal/events", tokenMw(NewEventsHandler(broker, database)).ServeHTTP)
	r.Get("/api/events/recent", tokenMw(NewRecentEventsHandler(database)).ServeHTTP)
	r.Get("/api/tasks/list", tokenMw(NewTaskListHandler(database)).ServeHTTP)
	r.Get("/api/operations/summary", tokenMw(NewOperationsSummaryHandler(database)).ServeHTTP)
	r.Get("/api/capacity/latest", tokenMw(NewCapacityHandler(resultsDir)).ServeHTTP)
	r.Get("/api/metrics/runtime", tokenMw(NewRuntimeMetricsHandler(database, sqsClient, sqsQueueURL, q)).ServeHTTP)
	r.Get("/api/traces/{traceID}", tokenMw(NewTracesHandler(tempoClient, database)).ServeHTTP)

	// Alert Center (Phase 11)
	alertsHandler := handlers.NewAlertsHandler(database, broker)
	r.Get("/api/alerts", tokenMw(http.HandlerFunc(alertsHandler.List)).ServeHTTP)
	r.Post("/api/alerts/{id}/acknowledge", tokenMw(http.HandlerFunc(alertsHandler.Acknowledge)).ServeHTTP)
	r.Get("/api/alerts/stats", tokenMw(http.HandlerFunc(alertsHandler.Stats)).ServeHTTP)

	// Replay Task (Phase 11)
	replayHandler := handlers.NewReplayHandler(database, broker, sqsClient, sqsQueueURL)
	r.Post("/api/tasks/{taskID}/replay", tokenMw(http.HandlerFunc(replayHandler.Replay)).ServeHTTP)
	r.Options("/api/tasks/{taskID}/replay", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// DLQ Explorer (Phase 11)
	dlqURL := os.Getenv("SQS_DLQ_URL")
	if dlqURL != "" && sqsClient != nil {
		dlqHandler := handlers.NewDLQHandler(database, broker, sqsClient, dlqURL, sqsQueueURL)
		r.Get("/api/dlq/messages", tokenMw(http.HandlerFunc(dlqHandler.ListMessages)).ServeHTTP)
		r.Post("/api/dlq/messages/{messageID}/retry", tokenMw(http.HandlerFunc(dlqHandler.Retry)).ServeHTTP)
		r.Delete("/api/dlq/messages/{messageID}", tokenMw(http.HandlerFunc(dlqHandler.Delete)).ServeHTTP)
		r.Post("/api/dlq/purge", tokenMw(http.HandlerFunc(dlqHandler.Purge)).ServeHTTP)
		r.Get("/api/dlq/stats", tokenMw(http.HandlerFunc(dlqHandler.Stats)).ServeHTTP)
		r.Options("/api/dlq/messages/{messageID}/retry", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
		r.Options("/api/dlq/messages/{messageID}", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
		r.Options("/api/dlq/purge", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
	}

	// Chaos Dashboard (Phase 11)
	chaosResultsDir := os.Getenv("CAPACITY_RESULTS_DIR")
	chaosRequestDir := os.Getenv("CHAOS_REQUESTS_DIR")
	if chaosResultsDir == "" {
		chaosResultsDir = "results"
	}
	if chaosRequestDir == "" {
		chaosRequestDir = filepath.Join(chaosResultsDir, "chaos-requests")
	}
	chaosHandler := handlers.NewChaosHandler(chaosResultsDir, chaosRequestDir)
	r.Get("/api/chaos/reports", tokenMw(http.HandlerFunc(chaosHandler.ListReports)).ServeHTTP)
	r.Get("/api/chaos/reports/{reportId}", tokenMw(http.HandlerFunc(chaosHandler.GetReport)).ServeHTTP)
	r.Post("/api/chaos/trigger", tokenMw(http.HandlerFunc(chaosHandler.Trigger)).ServeHTTP)
	r.Options("/api/chaos/trigger", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	r.Get("/api/chaos/status", tokenMw(http.HandlerFunc(chaosHandler.Status)).ServeHTTP)
	r.Delete("/api/chaos/requests/{runId}", tokenMw(http.HandlerFunc(chaosHandler.Cancel)).ServeHTTP)
	r.Options("/api/chaos/requests/{runId}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	return r
}

func corsMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	originSet := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		originSet[o] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if originSet[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Internal-Token")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			next.ServeHTTP(w, r)
		})
	}
}

func isAllowedOrigin(origin string, allowedOrigins []string) bool {
	return slices.Contains(allowedOrigins, origin)
}

func internalTokenMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token != "" && r.Header.Get("X-Internal-Token") != token {
				jsonError(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// otelMiddleware extracts W3C traceparent if present, otherwise starts a new root span.
// Skips /health and OPTIONS (preflight) — both are noise in traces.
func otelMiddleware(next http.Handler) http.Handler {
	tracer := otel.Tracer("traceruntime-api")
	propagator := otel.GetTextMapPropagator()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" || r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := tracer.Start(ctx, r.Method+" "+r.URL.Path)
		defer span.End()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
