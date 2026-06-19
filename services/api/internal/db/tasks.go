package db

import (
	"context"
	"fmt"
	"time"
)

// TaskEvent represents a task row projected for the recent-events API.
type TaskEvent struct {
	ID           string     `json:"id"`
	TraceID      string     `json:"trace_id"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
	ArtifactKey  string     `json:"artifact_key,omitempty"`
	ErrorMessage string     `json:"error_message,omitempty"`
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

// RecentTaskEvents returns the most recent tasks ordered by created_at DESC.
// limit is clamped to [1, 100].
func (d *DB) RecentTaskEvents(ctx context.Context, limit int) ([]TaskEvent, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := d.pool.Query(ctx,
		`SELECT id, trace_id, status, created_at,
		        completed_at,
		        COALESCE(artifact_key, ''),
		        COALESCE(error_message, '')
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
		if err := rows.Scan(&te.ID, &te.TraceID, &te.Status, &te.CreatedAt, &te.CompletedAt, &te.ArtifactKey, &te.ErrorMessage); err != nil {
			return nil, fmt.Errorf("db.RecentTaskEvents scan: %w", err)
		}
		results = append(results, te)
	}
	return results, rows.Err()
}
