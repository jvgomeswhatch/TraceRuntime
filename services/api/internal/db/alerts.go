package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

type AlertListParams struct {
	Status    string
	Severity  string
	EventType string
	Limit     int
	Cursor    string
}

type Alert struct {
	ID              string          `json:"id"`
	EventType       string          `json:"event_type"`
	Severity        string          `json:"severity"`
	Source          string          `json:"source"`
	Status          string          `json:"status"`
	WorkerID        string          `json:"worker_id,omitempty"`
	Details         json.RawMessage `json:"details"`
	CreatedAt       time.Time       `json:"created_at"`
	ResolvedAt      *time.Time      `json:"resolved_at,omitempty"`
	DurationSeconds *float64        `json:"duration_seconds,omitempty"`
}

func (d *DB) ListAlerts(ctx context.Context, params AlertListParams) ([]Alert, string, error) {
	if params.Limit <= 0 || params.Limit > 100 {
		params.Limit = 50
	}

	query := `SELECT id, event_type, severity, source, status, COALESCE(worker_id, ''),
	                 details, created_at, resolved_at,
	                 EXTRACT(EPOCH FROM (COALESCE(resolved_at, NOW()) - created_at))
	          FROM healing_events
	          WHERE ($1 = 'all' OR status = $1)
	            AND ($2 = '' OR severity = $2)
	            AND ($3 = '' OR event_type = $3)
	            AND ($4 = '' OR created_at < $4::timestamptz)
	          ORDER BY created_at DESC
	          LIMIT $5`

	rows, err := d.pool.Query(ctx, query, params.Status, params.Severity, params.EventType, params.Cursor, params.Limit)
	if err != nil {
		return nil, "", fmt.Errorf("db.ListAlerts: %w", err)
	}
	defer rows.Close()

	var results []Alert
	for rows.Next() {
		var a Alert
		var dur float64
		if err := rows.Scan(&a.ID, &a.EventType, &a.Severity, &a.Source, &a.Status, &a.WorkerID, &a.Details, &a.CreatedAt, &a.ResolvedAt, &dur); err != nil {
			return nil, "", fmt.Errorf("db.ListAlerts scan: %w", err)
		}
		a.DurationSeconds = &dur
		results = append(results, a)
	}
	if err := rows.Err(); err != nil {
		return nil, "", fmt.Errorf("db.ListAlerts rows: %w", err)
	}

	var nextCursor string
	if len(results) == params.Limit {
		nextCursor = results[len(results)-1].CreatedAt.Format(time.RFC3339Nano)
	}
	return results, nextCursor, nil
}

func (d *DB) AcknowledgeAlert(ctx context.Context, id string) error {
	tag, err := d.pool.Exec(ctx,
		`UPDATE healing_events SET status = 'acknowledged'
		 WHERE id = $1::uuid AND status = 'active'`, id)
	if err != nil {
		return fmt.Errorf("db.AcknowledgeAlert: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("not found or already processed")
	}
	return nil
}

type AlertStats struct {
	Active               int     `json:"active"`
	Acknowledged         int     `json:"acknowledged"`
	Resolved24h          int     `json:"resolved_24h"`
	CriticalActive       int     `json:"critical_active"`
	AvgResolutionSeconds float64 `json:"avg_resolution_seconds"`
}

type AlertTypeStats struct {
	Active   int `json:"active"`
	Total24h int `json:"total_24h"`
}

func (d *DB) AlertStats(ctx context.Context) (*AlertStats, map[string]AlertTypeStats, error) {
	var stats AlertStats
	var avgRes *float64
	err := d.pool.QueryRow(ctx,
		`SELECT
		   COUNT(*) FILTER (WHERE status = 'active'),
		   COUNT(*) FILTER (WHERE status = 'acknowledged'),
		   COUNT(*) FILTER (WHERE status = 'resolved' AND resolved_at > NOW() - INTERVAL '24 hours'),
		   COUNT(*) FILTER (WHERE severity = 'critical' AND status = 'active'),
		   AVG(EXTRACT(EPOCH FROM (resolved_at - created_at))) FILTER (WHERE status = 'resolved' AND resolved_at IS NOT NULL)
		 FROM healing_events`).Scan(&stats.Active, &stats.Acknowledged, &stats.Resolved24h, &stats.CriticalActive, &avgRes)
	if err != nil {
		return nil, nil, fmt.Errorf("db.AlertStats: %w", err)
	}
	if avgRes != nil {
		stats.AvgResolutionSeconds = *avgRes
	}

	rows, err := d.pool.Query(ctx,
		`SELECT event_type,
		   COUNT(*) FILTER (WHERE status = 'active'),
		   COUNT(*) FILTER (WHERE created_at > NOW() - INTERVAL '24 hours')
		 FROM healing_events
		 GROUP BY event_type`)
	if err != nil {
		return nil, nil, fmt.Errorf("db.AlertStats by_type: %w", err)
	}
	defer rows.Close()

	byType := make(map[string]AlertTypeStats)
	for rows.Next() {
		var eventType string
		var ts AlertTypeStats
		if err := rows.Scan(&eventType, &ts.Active, &ts.Total24h); err != nil {
			return nil, nil, fmt.Errorf("db.AlertStats by_type scan: %w", err)
		}
		byType[eventType] = ts
	}
	return &stats, byType, rows.Err()
}
