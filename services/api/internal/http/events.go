package http

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"

	"github.com/runtime-platform/services/api/internal/event"
)

type EventsHandler struct {
	broker *event.Broker
}

func NewEventsHandler(broker *event.Broker) *EventsHandler {
	return &EventsHandler{broker: broker}
}

func (h *EventsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	if token := os.Getenv("INTERNAL_TOKEN"); token != "" {
		if r.Header.Get("X-Internal-Token") != token {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		slog.Warn("failed to read /internal/events body", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if !json.Valid(body) {
		slog.Warn("invalid json in /internal/events body")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	h.broker.Publish(body)

	w.WriteHeader(http.StatusAccepted)
}
