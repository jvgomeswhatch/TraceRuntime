package db

import (
	"context"
	"fmt"
)

type TaskReplayInfo struct {
	ID           string
	TraceID      string
	Status       string
	InputPayload *string
	Model        string
}

func (d *DB) GetTaskForReplay(ctx context.Context, taskID string) (*TaskReplayInfo, error) {
	var t TaskReplayInfo
	var inputPayload *string
	err := d.pool.QueryRow(ctx,
		`SELECT id, trace_id, status, input_payload, COALESCE(model, '')
		 FROM tasks WHERE id = $1::uuid`, taskID).
		Scan(&t.ID, &t.TraceID, &t.Status, &inputPayload, &t.Model)
	if err != nil {
		if err.Error() == "no rows in result set" {
			return nil, nil
		}
		return nil, fmt.Errorf("db.GetTaskForReplay: %w", err)
	}
	t.InputPayload = inputPayload
	return &t, nil
}
