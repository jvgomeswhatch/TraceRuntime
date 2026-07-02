package db

import (
	"context"
	"fmt"
)

type RuntimeMetrics struct {
	WindowSeconds  int     `json:"window_seconds"`
	P95LatencyMs   float64 `json:"p95_latency_ms"`
	AvgLatencyMs   float64 `json:"avg_latency_ms"`
	SuccessRate    float64 `json:"success_rate"`
	ErrorRate      float64 `json:"error_rate"`
	Completed      int     `json:"completed"`
	Failed         int     `json:"failed"`
	TotalCompleted int     `json:"total_completed"`
	TotalFailed    int     `json:"total_failed"`
}

func (d *DB) RuntimeMetrics(ctx context.Context, windowSeconds int) (*RuntimeMetrics, error) {
	query := `
		WITH window_tasks AS (
			SELECT status, processing_started_at, completed_at
			FROM tasks
			WHERE chaos_run_id IS NULL
			  AND updated_at >= NOW() - INTERVAL '1 second' * $1
			  AND (completed_at IS NOT NULL OR status = 'failed')
		),
		counts AS (
			SELECT
				COUNT(*) FILTER (WHERE status = 'completed') AS completed,
				COUNT(*) FILTER (WHERE status = 'failed')    AS failed
			FROM window_tasks
		),
		totals AS (
			SELECT
				COUNT(*) FILTER (WHERE status = 'completed') AS total_completed,
				COUNT(*) FILTER (WHERE status = 'failed')    AS total_failed
			FROM tasks
			WHERE chaos_run_id IS NULL
		),
		latencies AS (
			SELECT
				COALESCE(PERCENTILE_CONT(0.95) WITHIN GROUP (
					ORDER BY EXTRACT(EPOCH FROM (completed_at - processing_started_at)) * 1000
				), 0) AS p95_ms,
				COALESCE(AVG(
					EXTRACT(EPOCH FROM (completed_at - processing_started_at)) * 1000
				), 0) AS avg_ms
			FROM window_tasks
			WHERE status = 'completed'
			  AND processing_started_at IS NOT NULL
			  AND completed_at IS NOT NULL
		)
		SELECT
			c.completed,
			c.failed,
			l.p95_ms,
			l.avg_ms,
			CASE WHEN (c.completed + c.failed) > 0
				THEN c.completed::double precision / (c.completed + c.failed) * 100
				ELSE 0
			END AS success_rate,
			CASE WHEN (c.completed + c.failed) > 0
				THEN c.failed::double precision / (c.completed + c.failed) * 100
				ELSE 0
			END AS error_rate,
			t.total_completed,
			t.total_failed
		FROM counts c, latencies l, totals t`

	m := &RuntimeMetrics{WindowSeconds: windowSeconds}
	err := d.pool.QueryRow(ctx, query, windowSeconds).Scan(
		&m.Completed,
		&m.Failed,
		&m.P95LatencyMs,
		&m.AvgLatencyMs,
		&m.SuccessRate,
		&m.ErrorRate,
		&m.TotalCompleted,
		&m.TotalFailed,
	)
	if err != nil {
		return nil, fmt.Errorf("db.RuntimeMetrics: %w", err)
	}
	return m, nil
}
