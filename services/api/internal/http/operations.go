package http

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/runtime-platform/services/api/internal/db"
	"github.com/runtime-platform/services/api/internal/telemetry"
)

type operationsSummary struct {
	Workers      []db.WorkerStatus `json:"workers"`
	ActiveEvents []db.HealingEvent `json:"active_events"`
	RecentEvents []db.HealingEvent `json:"recent_events"`
	Timestamp    string            `json:"timestamp"`
}

type OperationsSummaryHandler struct {
	db *db.DB
}

func NewOperationsSummaryHandler(database *db.DB) *OperationsSummaryHandler {
	return &OperationsSummaryHandler{db: database}
}

func (h *OperationsSummaryHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if h.db == nil {
		jsonError(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	workers, err := h.db.ListWorkerStatuses(ctx, 60)
	if err != nil {
		telemetry.Error(ctx, "operations.summary: list workers failed", "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	active, err := h.db.ListActiveHealingEvents(ctx, 100)
	if err != nil {
		telemetry.Error(ctx, "operations.summary: list active events failed", "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	recent, err := h.db.ListRecentHealingEvents(ctx, 50)
	if err != nil {
		telemetry.Error(ctx, "operations.summary: list recent events failed", "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	if workers == nil {
		workers = []db.WorkerStatus{}
	}
	if active == nil {
		active = []db.HealingEvent{}
	}
	if recent == nil {
		recent = []db.HealingEvent{}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(operationsSummary{
		Workers:      workers,
		ActiveEvents: active,
		RecentEvents: recent,
		Timestamp:    time.Now().UTC().Format(time.RFC3339),
	})
}
