package http

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"

	"github.com/runtime-platform/services/api/internal/event"
)

var sseTracer = otel.Tracer("traceruntime-api/sse")

type SSEHandler struct {
	broker         *event.Broker
	allowedOrigins []string
}

func NewSSEHandler(broker *event.Broker, allowedOrigins []string) *SSEHandler {
	return &SSEHandler{broker: broker, allowedOrigins: allowedOrigins}
}

func (h *SSEHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin != "" && !isAllowedOrigin(origin, h.allowedOrigins) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}

	_, span := sseTracer.Start(r.Context(), "sse.stream")
	defer span.End()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	clientID := fmt.Sprintf("sse-%d", time.Now().UnixNano())
	ch := h.broker.Subscribe(clientID)
	defer h.broker.Unsubscribe(clientID)

	slog.Info("sse client connected", "client_id", clientID)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Flush headers immediately so the browser establishes the SSE connection
	// without waiting for the first event.
	flusher.Flush()

	for {
		select {
		case <-r.Context().Done():
			slog.Info("sse client disconnected", "client_id", clientID)
			return
		case msg := <-ch:
			_, _ = fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}
