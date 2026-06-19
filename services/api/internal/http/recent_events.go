package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/runtime-platform/services/api/internal/db"
	"github.com/runtime-platform/services/api/internal/telemetry"
)

// recentEvent matches the SSE event shape the frontend expects.
type recentEvent struct {
	EventID     string `json:"event_id"`
	EventType   string `json:"event_type"`
	TaskID      string `json:"task_id"`
	TraceID     string `json:"trace_id"`
	Traceparent string `json:"traceparent"`
	Timestamp   string `json:"timestamp"`
	Source      string `json:"source"`
	ErrorReason string `json:"error_reason,omitempty"`
}

// RecentEventsHandler returns historical task events from PostgreSQL so the
// frontend can hydrate its event feed on page load (SSE only delivers live events).
type RecentEventsHandler struct {
	db *db.DB
}

func NewRecentEventsHandler(database *db.DB) *RecentEventsHandler {
	return &RecentEventsHandler{db: database}
}

func (h *RecentEventsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if h.db == nil {
		jsonError(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > 100 {
		limit = 100
	}

	tasks, err := h.db.RecentTaskEvents(ctx, limit)
	if err != nil {
		telemetry.Error(ctx, "recent_events: query failed", "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	events := make([]recentEvent, 0, len(tasks))
	for _, t := range tasks {
		ts := t.CreatedAt
		if t.CompletedAt != nil {
			ts = *t.CompletedAt
		}

		ev := recentEvent{
			EventID:     t.ID,
			EventType:   "task." + t.Status,
			TaskID:      t.ID,
			TraceID:     t.TraceID,
			Traceparent: "",
			Timestamp:   ts.UTC().Format(time.RFC3339),
			Source:      "api",
		}
		if t.ErrorMessage != "" {
			ev.ErrorReason = t.ErrorMessage
		}
		events = append(events, ev)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(events)
}
