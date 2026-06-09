package db

import (
	"context"
	"fmt"
	"time"
)

func (d *DB) SetProcessing(ctx context.Context, id string) error {
	now := time.Now().UTC()
	// $2::uuid: pgx sends Go strings as pg text; explicit cast required for UUID columns.
	_, err := d.pool.Exec(ctx,
		`UPDATE tasks SET status = 'processing', processing_started_at = $1, updated_at = $1 WHERE id = $2::uuid`,
		now, id,
	)
	if err != nil {
		return fmt.Errorf("db.SetProcessing %s: %w", id, err)
	}
	return nil
}

func (d *DB) SetCompleted(ctx context.Context, id, artifactKey string) error {
	now := time.Now().UTC()
	// $3::uuid: pgx sends Go strings as pg text; explicit cast required for UUID columns.
	_, err := d.pool.Exec(ctx,
		`UPDATE tasks SET status = 'completed', completed_at = $1, artifact_key = $2, updated_at = $1 WHERE id = $3::uuid`,
		now, artifactKey, id,
	)
	if err != nil {
		return fmt.Errorf("db.SetCompleted %s: %w", id, err)
	}
	return nil
}

func (d *DB) SetFailed(ctx context.Context, id, reason string) error {
	now := time.Now().UTC()
	// $3::uuid: pgx sends Go strings as pg text; explicit cast required for UUID columns.
	_, err := d.pool.Exec(ctx,
		`UPDATE tasks SET status = 'failed', completed_at = $1, error_message = $2, updated_at = $1 WHERE id = $3::uuid`,
		now, reason, id,
	)
	if err != nil {
		return fmt.Errorf("db.SetFailed %s: %w", id, err)
	}
	return nil
}
