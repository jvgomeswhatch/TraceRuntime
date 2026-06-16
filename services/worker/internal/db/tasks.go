package db

import (
	"context"
	"errors"
	"fmt"
	"time"
)

type Heartbeat struct {
	WorkerID       string
	TasksProcessed int64
	TasksFailed    int64
	CurrentTaskID  string // empty string = no current task
	Goroutines     int
	UptimeSeconds  int64
}

var ErrStateConflict = errors.New("state transition conflict: row not in expected status")

func (d *DB) SetProcessing(ctx context.Context, id string) error {
	now := time.Now().UTC()
	tag, err := d.pool.Exec(ctx,
		`UPDATE tasks SET status = 'processing', processing_started_at = $1, updated_at = $1
		 WHERE id = $2::uuid AND status = 'pending'`,
		now, id,
	)
	if err != nil {
		return fmt.Errorf("db.SetProcessing %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("db.SetProcessing %s: %w", id, ErrStateConflict)
	}
	return nil
}

func (d *DB) SetCompleted(ctx context.Context, id, artifactKey string) error {
	now := time.Now().UTC()
	tag, err := d.pool.Exec(ctx,
		`UPDATE tasks SET status = 'completed', completed_at = $1, artifact_key = $2, updated_at = $1
		 WHERE id = $3::uuid AND status = 'processing'`,
		now, artifactKey, id,
	)
	if err != nil {
		return fmt.Errorf("db.SetCompleted %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("db.SetCompleted %s: %w", id, ErrStateConflict)
	}
	return nil
}

func (d *DB) SetFailed(ctx context.Context, id, reason string) error {
	now := time.Now().UTC()
	tag, err := d.pool.Exec(ctx,
		`UPDATE tasks SET status = 'failed', completed_at = $1, error_message = $2, updated_at = $1
		 WHERE id = $3::uuid AND status = 'processing'`,
		now, reason, id,
	)
	if err != nil {
		return fmt.Errorf("db.SetFailed %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("db.SetFailed %s: %w", id, ErrStateConflict)
	}
	return nil
}

func (d *DB) UpsertHeartbeat(ctx context.Context, hb Heartbeat) error {
	var currentTaskID *string
	if hb.CurrentTaskID != "" {
		currentTaskID = &hb.CurrentTaskID
	}
	_, err := d.pool.Exec(ctx,
		`INSERT INTO worker_heartbeats (worker_id, last_seen_at, tasks_processed, tasks_failed, current_task_id, goroutines, uptime_seconds)
		 VALUES ($1, NOW(), $2, $3, $4::uuid, $5, $6)
		 ON CONFLICT (worker_id) DO UPDATE SET
		   last_seen_at = NOW(),
		   tasks_processed = $2,
		   tasks_failed = $3,
		   current_task_id = $4::uuid,
		   goroutines = $5,
		   uptime_seconds = $6`,
		hb.WorkerID, hb.TasksProcessed, hb.TasksFailed, currentTaskID, hb.Goroutines, hb.UptimeSeconds,
	)
	if err != nil {
		return fmt.Errorf("db.UpsertHeartbeat %s: %w", hb.WorkerID, err)
	}
	return nil
}
