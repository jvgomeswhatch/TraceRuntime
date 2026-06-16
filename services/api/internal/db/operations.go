package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type WorkerStatus struct {
	WorkerID       string    `json:"worker_id"`
	LastSeenAt     time.Time `json:"last_seen_at"`
	TasksProcessed int64     `json:"tasks_processed"`
	TasksFailed    int64     `json:"tasks_failed"`
	Goroutines     int       `json:"goroutines"`
	UptimeSeconds  int64     `json:"uptime_seconds"`
	Status         string    `json:"status"`
}

type HealingEvent struct {
	ID         string          `json:"id"`
	EventType  string          `json:"event_type"`
	Severity   string          `json:"severity"`
	Source     string          `json:"source"`
	Status     string          `json:"status"`
	WorkerID   string          `json:"worker_id,omitempty"`
	Details    json.RawMessage `json:"details"`
	CreatedAt  time.Time       `json:"created_at"`
	ResolvedAt *time.Time      `json:"resolved_at,omitempty"`
}

func (d *DB) ListWorkerStatuses(ctx context.Context, staleThresholdSeconds int) ([]WorkerStatus, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT worker_id, last_seen_at, tasks_processed, tasks_failed, goroutines, uptime_seconds,
		        CASE WHEN last_seen_at < NOW() - make_interval(secs => $1) THEN 'stale' ELSE 'healthy' END AS status
		 FROM worker_heartbeats
		 ORDER BY worker_id`, staleThresholdSeconds)
	if err != nil {
		return nil, fmt.Errorf("db.ListWorkerStatuses: %w", err)
	}
	defer rows.Close()

	var results []WorkerStatus
	for rows.Next() {
		var ws WorkerStatus
		if err := rows.Scan(&ws.WorkerID, &ws.LastSeenAt, &ws.TasksProcessed, &ws.TasksFailed, &ws.Goroutines, &ws.UptimeSeconds, &ws.Status); err != nil {
			return nil, fmt.Errorf("db.ListWorkerStatuses scan: %w", err)
		}
		results = append(results, ws)
	}
	return results, rows.Err()
}

func (d *DB) ListActiveHealingEvents(ctx context.Context, limit int) ([]HealingEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	rows, err := d.pool.Query(ctx,
		`SELECT id, event_type, severity, source, status, COALESCE(worker_id, ''), details, created_at, resolved_at
		 FROM healing_events
		 WHERE status = 'active'
		 ORDER BY created_at DESC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("db.ListActiveHealingEvents: %w", err)
	}
	defer rows.Close()

	var results []HealingEvent
	for rows.Next() {
		var he HealingEvent
		if err := rows.Scan(&he.ID, &he.EventType, &he.Severity, &he.Source, &he.Status, &he.WorkerID, &he.Details, &he.CreatedAt, &he.ResolvedAt); err != nil {
			return nil, fmt.Errorf("db.ListActiveHealingEvents scan: %w", err)
		}
		results = append(results, he)
	}
	return results, rows.Err()
}

func (d *DB) ListRecentHealingEvents(ctx context.Context, limit int) ([]HealingEvent, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT id, event_type, severity, source, status, COALESCE(worker_id, ''), details, created_at, resolved_at
		 FROM healing_events
		 ORDER BY created_at DESC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("db.ListRecentHealingEvents: %w", err)
	}
	defer rows.Close()

	var results []HealingEvent
	for rows.Next() {
		var he HealingEvent
		if err := rows.Scan(&he.ID, &he.EventType, &he.Severity, &he.Source, &he.Status, &he.WorkerID, &he.Details, &he.CreatedAt, &he.ResolvedAt); err != nil {
			return nil, fmt.Errorf("db.ListRecentHealingEvents scan: %w", err)
		}
		results = append(results, he)
	}
	return results, rows.Err()
}
