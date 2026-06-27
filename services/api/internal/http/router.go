package http

import (
	"net/http"
	"slices"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	sqssdk "github.com/aws/aws-sdk-go-v2/service/sqs"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/runtime-platform/services/api/internal/db"
	"github.com/runtime-platform/services/api/internal/event"
	"github.com/runtime-platform/services/api/internal/queue"
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

	tokenMw := internalTokenMiddleware(internalToken)
	r.Post("/internal/events", tokenMw(NewEventsHandler(broker)).ServeHTTP)
	r.Get("/api/events/recent", tokenMw(NewRecentEventsHandler(database)).ServeHTTP)
	r.Get("/api/operations/summary", tokenMw(NewOperationsSummaryHandler(database)).ServeHTTP)
	r.Get("/api/capacity/latest", tokenMw(NewCapacityHandler(resultsDir)).ServeHTTP)
	r.Get("/api/metrics/runtime", tokenMw(NewRuntimeMetricsHandler(database, sqsClient, sqsQueueURL, q)).ServeHTTP)

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
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
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
