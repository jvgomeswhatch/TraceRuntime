package http

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/runtime-platform/services/api/internal/db"
	"github.com/runtime-platform/services/api/internal/telemetry"
)

type taskListItem struct {
	ID                  string  `json:"id"`
	TraceID             string  `json:"trace_id"`
	Status              string  `json:"status"`
	CreatedAt           string  `json:"created_at"`
	ProcessingStartedAt string  `json:"processing_started_at,omitempty"`
	CompletedAt         string  `json:"completed_at,omitempty"`
	ErrorMessage        string  `json:"error_message,omitempty"`
	PromptTokens        int     `json:"prompt_tokens,omitempty"`
	CompletionTokens    int     `json:"completion_tokens,omitempty"`
	TokensPerSecond     float64 `json:"tokens_per_second,omitempty"`
	Model               string  `json:"model,omitempty"`
	DurationMs          *int64  `json:"duration_ms,omitempty"`
}

type taskListResponse struct {
	Tasks    []taskListItem `json:"tasks"`
	Total    int            `json:"total"`
	Page     int            `json:"page"`
	PageSize int            `json:"page_size"`
}

type TaskListHandler struct {
	db *db.DB
}

func NewTaskListHandler(database *db.DB) *TaskListHandler {
	return &TaskListHandler{db: database}
}

func (h *TaskListHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	if h.db == nil {
		jsonError(w, "database not available", http.StatusServiceUnavailable)
		return
	}

	page := 1
	if v := r.URL.Query().Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			page = n
		}
	}

	pageSize := 50
	if v := r.URL.Query().Get("page_size"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 100 {
			pageSize = n
		}
	}

	statusFilter := r.URL.Query().Get("status")

	result, err := h.db.ListTasks(ctx, page, pageSize, statusFilter)
	if err != nil {
		telemetry.Error(ctx, "task_list: query failed", "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	items := make([]taskListItem, 0, len(result.Tasks))
	for _, t := range result.Tasks {
		item := taskListItem{
			ID:        t.ID,
			TraceID:   t.TraceID,
			Status:    t.Status,
			CreatedAt: t.CreatedAt.UTC().Format(time.RFC3339),
		}
		if t.ProcessingStartedAt != nil {
			item.ProcessingStartedAt = t.ProcessingStartedAt.UTC().Format(time.RFC3339)
		}
		if t.CompletedAt != nil {
			item.CompletedAt = t.CompletedAt.UTC().Format(time.RFC3339)
		}
		if t.ErrorMessage != "" {
			item.ErrorMessage = t.ErrorMessage
		}
		item.PromptTokens = t.PromptTokens
		item.CompletionTokens = t.CompletionTokens
		item.TokensPerSecond = t.TokensPerSecond
		item.Model = t.Model

		if t.ProcessingStartedAt != nil && t.CompletedAt != nil {
			ms := t.CompletedAt.Sub(*t.ProcessingStartedAt).Milliseconds()
			item.DurationMs = &ms
		}

		items = append(items, item)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(taskListResponse{
		Tasks:    items,
		Total:    result.Total,
		Page:     page,
		PageSize: pageSize,
	})
}
