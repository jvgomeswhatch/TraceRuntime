package http

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/runtime-platform/services/api/internal/db"
	"github.com/runtime-platform/services/api/internal/event"
)

type EventsHandler struct {
	broker *event.Broker
	db     *db.DB
}

func NewEventsHandler(broker *event.Broker, database *db.DB) *EventsHandler {
	return &EventsHandler{broker: broker, db: database}
}

func (h *EventsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
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

	var envelope struct {
		TaskID         string `json:"task_id"`
		HealingEventID string `json:"healing_event_id"`
		Source         string `json:"source"`
	}
	if json.Unmarshal(body, &envelope) == nil {
		if envelope.TaskID != "" && h.db.IsChaosTask(r.Context(), envelope.TaskID) {
			slog.Debug("skipping SSE broadcast for chaos task", "task_id", envelope.TaskID)
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if envelope.HealingEventID != "" && h.db.IsChaosRunActive(r.Context()) {
			slog.Debug("skipping SSE broadcast for healing event during chaos run", "healing_event_id", envelope.HealingEventID)
			w.WriteHeader(http.StatusAccepted)
			return
		}
	}

	h.broker.Publish(body)

	w.WriteHeader(http.StatusAccepted)
}
