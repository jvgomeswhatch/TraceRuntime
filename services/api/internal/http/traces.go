package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/runtime-platform/services/api/internal/db"
	"github.com/runtime-platform/services/api/internal/tempo"
)

type TracesHandler struct {
	tempo *tempo.Client
	db    *db.DB
}

func NewTracesHandler(tempoClient *tempo.Client, database *db.DB) *TracesHandler {
	return &TracesHandler{tempo: tempoClient, db: database}
}

type traceDetailResponse struct {
	TraceID        string          `json:"trace_id"`
	Task           *db.TaskDetail  `json:"task"`
	Spans          []tempo.Span    `json:"spans"`
	Services       []string        `json:"services"`
	TempoAvailable bool            `json:"tempo_available"`
}

func (h *TracesHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	traceID := chi.URLParam(r, "traceID")
	if !tempo.ValidTraceID(traceID) {
		jsonError(w, "invalid trace_id format: must be 32 hex characters", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()

	var (
		task      *db.TaskDetail
		taskErr   error
		traceResp *tempo.TraceResponse
		traceErr  error
		wg        sync.WaitGroup
	)

	wg.Add(2)
	go func() {
		defer wg.Done()
		dbCtx, dbCancel := context.WithTimeout(ctx, 3*time.Second)
		defer dbCancel()
		task, taskErr = h.db.GetTaskByTraceID(dbCtx, traceID)
	}()
	go func() {
		defer wg.Done()
		if h.tempo != nil {
			traceResp, traceErr = h.tempo.GetTrace(ctx, traceID)
		}
	}()
	wg.Wait()

	if taskErr != nil {
		slog.Error("trace detail: db query failed", "trace_id", traceID, "error", taskErr)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if task == nil {
		jsonError(w, "trace not found", http.StatusNotFound)
		return
	}

	if traceErr != nil {
		slog.Warn("trace detail: tempo unavailable", "trace_id", traceID, "error", traceErr)
	}

	resp := traceDetailResponse{
		TraceID:        traceID,
		Task:           task,
		TempoAvailable: traceResp != nil,
	}
	if traceResp != nil {
		resp.Spans = traceResp.Spans
		resp.Services = traceResp.Services
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
