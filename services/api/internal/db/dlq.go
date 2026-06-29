package db

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
)

func (d *DB) GetTaskStatus(ctx context.Context, taskID string) (status string, errorMsg string, err error) {
	err = d.pool.QueryRow(ctx,
		`SELECT COALESCE(status, ''), COALESCE(error_message, '')
		 FROM tasks WHERE id = $1::uuid`, taskID).Scan(&status, &errorMsg)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return "", "", nil
		}
		return "", "", fmt.Errorf("db.GetTaskStatus: %w", err)
	}
	return status, errorMsg, nil
}

func (d *DB) RevertToPendingForRetry(ctx context.Context, taskID string) error {
	_, err := d.pool.Exec(ctx,
		`UPDATE tasks SET status = 'pending', error_message = NULL, updated_at = NOW()
		 WHERE id = $1::uuid AND status = 'failed'`, taskID)
	if err != nil {
		return fmt.Errorf("db.RevertToPendingForRetry: %w", err)
	}
	return nil
}

func (d *DB) InsertAuditEvent(ctx context.Context, eventType, severity, source string, details json.RawMessage) (string, error) {
	id := uuid.New().String()
	_, err := d.pool.Exec(ctx,
		`INSERT INTO healing_events (id, event_type, severity, source, status, details, created_at)
		 VALUES ($1::uuid, $2, $3, $4, 'resolved', $5, NOW())`,
		id, eventType, severity, source, details)
	if err != nil {
		return "", fmt.Errorf("db.InsertAuditEvent: %w", err)
	}
	return id, nil
}
