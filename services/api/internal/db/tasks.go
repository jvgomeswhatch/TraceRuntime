package db

import (
	"context"
	"fmt"
	"time"
)

// TaskEvent represents a task row projected for the recent-events API.
type TaskEvent struct {
	ID                  string     `json:"id"`
	TraceID             string     `json:"trace_id"`
	Status              string     `json:"status"`
	CreatedAt           time.Time  `json:"created_at"`
	ProcessingStartedAt *time.Time `json:"processing_started_at,omitempty"`
	CompletedAt         *time.Time `json:"completed_at,omitempty"`
	ArtifactKey         string     `json:"artifact_key,omitempty"`
	ErrorMessage        string     `json:"error_message,omitempty"`
	PromptTokens        int
	CompletionTokens    int
	TokensPerSecond     float64
	Model               string
}

// SetFailed marks a pending task as failed with a reason.
// Used when the task was inserted but could not be published to the queue.
func (d *DB) SetFailed(ctx context.Context, id, reason string) error {
	_, err := d.pool.Exec(ctx,
		`UPDATE tasks SET status = 'failed', completed_at = NOW(), error_message = $2, updated_at = NOW()
		 WHERE id = $1::uuid AND status = 'pending'`,
		id, reason,
	)
	if err != nil {
		return fmt.Errorf("db.SetFailed %s: %w", id, err)
	}
	return nil
}

type CreateTaskParams struct {
	ID               string
	TraceID          string
	InputPayload     string
	InputArtifactKey *string
	ReplayOf         *string
	ChaosRunID       *string
}

func (d *DB) InsertTask(ctx context.Context, params CreateTaskParams) error {
	now := time.Now().UTC()
	_, err := d.pool.Exec(ctx,
		`INSERT INTO tasks (id, trace_id, status, created_at, updated_at, input_payload, input_artifact_key, replay_of, chaos_run_id)
		 VALUES ($1::uuid, $2, 'pending', $3, $3, $4, $5, $6, $7::uuid)`,
		params.ID, params.TraceID, now, params.InputPayload, params.InputArtifactKey, params.ReplayOf, params.ChaosRunID,
	)
	if err != nil {
		return fmt.Errorf("db.InsertTask %s: %w", params.ID, err)
	}
	return nil
}

// TaskDetail holds full task data for trace detail and request inspector endpoints.
type TaskDetail struct {
	ID                  string     `json:"id"`
	TraceID             string     `json:"trace_id"`
	Status              string     `json:"status"`
	CreatedAt           time.Time  `json:"created_at"`
	ProcessingStartedAt *time.Time `json:"processing_started_at,omitempty"`
	CompletedAt         *time.Time `json:"completed_at,omitempty"`
	ArtifactKey         string     `json:"artifact_key,omitempty"`
	ErrorMessage        string     `json:"error_message,omitempty"`
	PromptTokens        int        `json:"prompt_tokens"`
	CompletionTokens    int        `json:"completion_tokens"`
	TokensPerSecond     float64    `json:"tokens_per_second"`
	Model               string     `json:"model,omitempty"`
}

// GetTaskByTraceID returns a task by its trace_id. Uses idx_tasks_trace_id.
func (d *DB) GetTaskByTraceID(ctx context.Context, traceID string) (*TaskDetail, error) {
	var t TaskDetail
	err := d.pool.QueryRow(ctx,
		`SELECT id, trace_id, status, created_at, processing_started_at, completed_at,
		        COALESCE(artifact_key, ''), COALESCE(error_message, ''),
		        COALESCE(prompt_tokens, 0), COALESCE(completion_tokens, 0),
		        COALESCE(tokens_per_second, 0), COALESCE(model, '')
		 FROM tasks WHERE trace_id = $1
		 ORDER BY created_at DESC LIMIT 1`, traceID).
		Scan(&t.ID, &t.TraceID, &t.Status, &t.CreatedAt, &t.ProcessingStartedAt, &t.CompletedAt,
			&t.ArtifactKey, &t.ErrorMessage, &t.PromptTokens, &t.CompletionTokens,
			&t.TokensPerSecond, &t.Model)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("db.GetTaskByTraceID: %w", err)
	}
	return &t, nil
}

// RecentTaskEvents returns the most recent tasks ordered by created_at DESC.
// limit is clamped to [1, 100].
func (d *DB) RecentTaskEvents(ctx context.Context, limit int) ([]TaskEvent, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := d.pool.Query(ctx,
		`SELECT id, trace_id, status, created_at,
		        processing_started_at, completed_at,
		        COALESCE(artifact_key, ''),
		        COALESCE(error_message, ''),
		        COALESCE(prompt_tokens, 0),
		        COALESCE(completion_tokens, 0),
		        COALESCE(tokens_per_second, 0),
		        COALESCE(model, '')
		 FROM tasks
		 WHERE chaos_run_id IS NULL
		 ORDER BY created_at DESC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("db.RecentTaskEvents: %w", err)
	}
	defer rows.Close()

	var results []TaskEvent
	for rows.Next() {
		var te TaskEvent
		if err := rows.Scan(&te.ID, &te.TraceID, &te.Status, &te.CreatedAt, &te.ProcessingStartedAt, &te.CompletedAt, &te.ArtifactKey, &te.ErrorMessage, &te.PromptTokens, &te.CompletionTokens, &te.TokensPerSecond, &te.Model); err != nil {
			return nil, fmt.Errorf("db.RecentTaskEvents scan: %w", err)
		}
		results = append(results, te)
	}
	return results, rows.Err()
}

type TaskListResult struct {
	Tasks []TaskEvent
	Total int
}

func (d *DB) ListTasks(ctx context.Context, page, pageSize int, statusFilter string) (*TaskListResult, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 100 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	var total int
	if statusFilter != "" {
		err := d.pool.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE chaos_run_id IS NULL AND status = $1`, statusFilter).Scan(&total)
		if err != nil {
			return nil, fmt.Errorf("db.ListTasks count: %w", err)
		}
	} else {
		err := d.pool.QueryRow(ctx, `SELECT COUNT(*) FROM tasks WHERE chaos_run_id IS NULL`).Scan(&total)
		if err != nil {
			return nil, fmt.Errorf("db.ListTasks count: %w", err)
		}
	}

	var query string
	var args []any
	if statusFilter != "" {
		query = `SELECT id, trace_id, status, created_at,
		         processing_started_at, completed_at,
		         COALESCE(artifact_key, ''), COALESCE(error_message, ''),
		         COALESCE(prompt_tokens, 0), COALESCE(completion_tokens, 0),
		         COALESCE(tokens_per_second, 0), COALESCE(model, '')
		  FROM tasks WHERE chaos_run_id IS NULL AND status = $1
		  ORDER BY created_at DESC
		  LIMIT $2 OFFSET $3`
		args = []any{statusFilter, pageSize, offset}
	} else {
		query = `SELECT id, trace_id, status, created_at,
		         processing_started_at, completed_at,
		         COALESCE(artifact_key, ''), COALESCE(error_message, ''),
		         COALESCE(prompt_tokens, 0), COALESCE(completion_tokens, 0),
		         COALESCE(tokens_per_second, 0), COALESCE(model, '')
		  FROM tasks WHERE chaos_run_id IS NULL
		  ORDER BY created_at DESC
		  LIMIT $1 OFFSET $2`
		args = []any{pageSize, offset}
	}

	rows, err := d.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("db.ListTasks: %w", err)
	}
	defer rows.Close()

	var results []TaskEvent
	for rows.Next() {
		var te TaskEvent
		if err := rows.Scan(&te.ID, &te.TraceID, &te.Status, &te.CreatedAt, &te.ProcessingStartedAt, &te.CompletedAt, &te.ArtifactKey, &te.ErrorMessage, &te.PromptTokens, &te.CompletionTokens, &te.TokensPerSecond, &te.Model); err != nil {
			return nil, fmt.Errorf("db.ListTasks scan: %w", err)
		}
		results = append(results, te)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return &TaskListResult{Tasks: results, Total: total}, nil
}

// IsChaosTask returns true if the given task has a non-NULL chaos_run_id.
// Returns false if the task is not found or on any query error.
func (d *DB) IsChaosTask(ctx context.Context, taskID string) bool {
	var chaosRunID *string
	err := d.pool.QueryRow(ctx,
		`SELECT chaos_run_id::text FROM tasks WHERE id = $1::uuid`, taskID).Scan(&chaosRunID)
	if err != nil {
		return false
	}
	return chaosRunID != nil
}
