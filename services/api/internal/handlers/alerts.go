package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/runtime-platform/services/api/internal/db"
	"github.com/runtime-platform/services/api/internal/event"
)

type AlertsHandler struct {
	db     *db.DB
	broker *event.Broker
}

func NewAlertsHandler(database *db.DB, broker *event.Broker) *AlertsHandler {
	return &AlertsHandler{db: database, broker: broker}
}

func (h *AlertsHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	params := db.AlertListParams{
		Status:    r.URL.Query().Get("status"),
		Severity:  r.URL.Query().Get("severity"),
		EventType: r.URL.Query().Get("event_type"),
		Cursor:    r.URL.Query().Get("cursor"),
	}
	if params.Status == "" {
		params.Status = "all"
	}

	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		fmt.Sscanf(l, "%d", &limit)
	}
	params.Limit = limit

	alerts, nextCursor, err := h.db.ListAlerts(ctx, params)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if alerts == nil {
		alerts = []db.Alert{}
	}

	activeCount := 0
	ackCount := 0
	for _, a := range alerts {
		if a.Status == "active" {
			activeCount++
		}
		if a.Status == "acknowledged" {
			ackCount++
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"alerts":             alerts,
		"next_cursor":        nextCursor,
		"total_active":       activeCount,
		"total_acknowledged": ackCount,
	})
}

func (h *AlertsHandler) Acknowledge(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	if err := h.db.AcknowledgeAlert(ctx, id); err != nil {
		if err.Error() == "not found or already processed" {
			jsonError(w, "alert not found or not active", http.StatusConflict)
			return
		}
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	sseEvent, _ := json.Marshal(map[string]string{
		"event_type":       "healing.acknowledged",
		"healing_event_id": id,
		"timestamp":        time.Now().UTC().Format(time.RFC3339),
	})
	h.broker.Publish(sseEvent)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"id":              id,
		"status":          "acknowledged",
		"acknowledged_at": time.Now().UTC().Format(time.RFC3339),
	})
}

func (h *AlertsHandler) Stats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	stats, byType, err := h.db.AlertStats(ctx)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"active":                 stats.Active,
		"acknowledged":           stats.Acknowledged,
		"resolved_24h":           stats.Resolved24h,
		"critical_active":        stats.CriticalActive,
		"avg_resolution_seconds": stats.AvgResolutionSeconds,
		"by_type":                byType,
	})
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
