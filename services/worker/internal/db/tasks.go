package db

import (
	"context"
	"errors"
	"fmt"
	"time"
)

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
