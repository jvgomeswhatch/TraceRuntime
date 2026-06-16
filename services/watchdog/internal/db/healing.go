package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type HealingEvent struct {
	ID         string
	EventType  string
	Severity   string
	Source     string
	Status     string
	WorkerID   string
	Details    json.RawMessage
	CreatedAt  time.Time
	ResolvedAt *time.Time
}

func (d *DB) InsertHealingEvent(ctx context.Context, eventType, severity, source, workerID string, details json.RawMessage) (string, error) {
	id := uuid.New().String()
	_, err := d.pool.Exec(ctx,
		`INSERT INTO healing_events (id, event_type, severity, source, status, worker_id, details)
		 VALUES ($1::uuid, $2, $3, $4, 'active', $5, $6)`,
		id, eventType, severity, source, workerID, details,
	)
	if err != nil {
		return "", fmt.Errorf("db.InsertHealingEvent: %w", err)
	}
	return id, nil
}

func (d *DB) ResolveHealingEvent(ctx context.Context, id string) error {
	_, err := d.pool.Exec(ctx,
		`UPDATE healing_events SET status = 'resolved', resolved_at = NOW()
		 WHERE id = $1::uuid AND status = 'active'`,
		id,
	)
	if err != nil {
		return fmt.Errorf("db.ResolveHealingEvent %s: %w", id, err)
	}
	return nil
}

func (d *DB) ListActiveHealingEvents(ctx context.Context) ([]HealingEvent, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT id, event_type, severity, source, status, COALESCE(worker_id, ''), details, created_at, resolved_at
		 FROM healing_events
		 WHERE status = 'active'
		 ORDER BY created_at DESC`)
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

type StuckTask struct {
	TaskID              string
	TraceID             string
	ProcessingStartedAt time.Time
}

func (d *DB) StuckTasks(ctx context.Context, stuckSeconds int) ([]StuckTask, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT id::text, trace_id, processing_started_at
		 FROM tasks
		 WHERE status = 'processing'
		   AND processing_started_at < NOW() - make_interval(secs => $1)`,
		stuckSeconds,
	)
	if err != nil {
		return nil, fmt.Errorf("db.StuckTasks: %w", err)
	}
	defer rows.Close()

	var results []StuckTask
	for rows.Next() {
		var st StuckTask
		if err := rows.Scan(&st.TaskID, &st.TraceID, &st.ProcessingStartedAt); err != nil {
			return nil, fmt.Errorf("db.StuckTasks scan: %w", err)
		}
		results = append(results, st)
	}
	return results, rows.Err()
}
