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
	broker *event.Broker
}

func NewSSEHandler(broker *event.Broker) *SSEHandler {
	return &SSEHandler{broker: broker}
}

func (h *SSEHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, span := sseTracer.Start(r.Context(), "sse.stream")
	defer span.End()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	clientID := fmt.Sprintf("sse-%d", time.Now().UnixNano())
	ch := h.broker.Subscribe(clientID)
	defer h.broker.Unsubscribe(clientID)

	slog.Info("sse client connected", "client_id", clientID)

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	for {
		select {
		case <-r.Context().Done():
			slog.Info("sse client disconnected", "client_id", clientID)
			return
		case msg := <-ch:
			fmt.Fprintf(w, "data: %s\n\n", msg)
			flusher.Flush()
		}
	}
}
