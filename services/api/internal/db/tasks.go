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
		`UPDATE tasks SET status = 'failed', error_message = $2, updated_at = NOW()
		 WHERE id = $1::uuid AND status = 'pending'`,
		id, reason,
	)
	if err != nil {
		return fmt.Errorf("db.SetFailed %s: %w", id, err)
	}
	return nil
}

func (d *DB) InsertTask(ctx context.Context, id, traceID string) error {
	now := time.Now().UTC()
	// $1::uuid: pgx sends Go strings as pg text; explicit cast required for UUID columns.
	_, err := d.pool.Exec(ctx,
		`INSERT INTO tasks (id, trace_id, status, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'pending', $3, $3)`,
		id, traceID, now,
	)
	if err != nil {
		return fmt.Errorf("db.InsertTask %s: %w", id, err)
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
