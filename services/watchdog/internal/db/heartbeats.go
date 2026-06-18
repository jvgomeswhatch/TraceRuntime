package db

import (
	"context"
	"fmt"
	"time"
)

type WorkerHeartbeat struct {
	WorkerID       string
	LastSeenAt     time.Time
	TasksProcessed int64
	TasksFailed    int64
	CurrentTaskID  *string
	Goroutines     int
	UptimeSeconds  int64
}

func (d *DB) DeleteStaleHeartbeats(ctx context.Context, maxAge time.Duration) (int64, error) {
	tag, err := d.pool.Exec(ctx,
		`DELETE FROM worker_heartbeats WHERE last_seen_at < NOW() - $1::interval`,
		maxAge.String())
	if err != nil {
		return 0, fmt.Errorf("db.DeleteStaleHeartbeats: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (d *DB) ListHeartbeats(ctx context.Context) ([]WorkerHeartbeat, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT worker_id, last_seen_at, tasks_processed, tasks_failed, current_task_id::text, goroutines, uptime_seconds
		 FROM worker_heartbeats`)
	if err != nil {
		return nil, fmt.Errorf("db.ListHeartbeats: %w", err)
	}
	defer rows.Close()

	var results []WorkerHeartbeat
	for rows.Next() {
		var hb WorkerHeartbeat
		if err := rows.Scan(&hb.WorkerID, &hb.LastSeenAt, &hb.TasksProcessed, &hb.TasksFailed, &hb.CurrentTaskID, &hb.Goroutines, &hb.UptimeSeconds); err != nil {
			return nil, fmt.Errorf("db.ListHeartbeats scan: %w", err)
		}
		results = append(results, hb)
	}
	return results, rows.Err()
}
