package http

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/runtime-platform/services/api/internal/db"
	"github.com/runtime-platform/services/api/internal/event"
	"github.com/runtime-platform/services/api/internal/queue"
)

func NewRouter(broker *event.Broker, publisher Publisher, q *queue.Queue, database *db.DB) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)
	r.Use(otelMiddleware)

	r.Get("/health", Health)
	r.Get("/metrics", NewMetricsHandler(broker, q).ServeHTTP)
	r.Get("/events", NewSSEHandler(broker).ServeHTTP)
	r.Post("/tasks", NewTaskHandler(broker, publisher, database).ServeHTTP)
	r.Options("/tasks", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	r.Post("/internal/events", NewEventsHandler(broker).ServeHTTP)

	return r
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3001")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		next.ServeHTTP(w, r)
	})
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
