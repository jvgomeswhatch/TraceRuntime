package db

import (
	"context"
	"fmt"
	"time"
)

func (d *DB) InsertTask(ctx context.Context, id, traceID string) error {
	now := time.Now().UTC()
	// $1::uuid: pgx sends Go strings as pg text; explicit cast required for UUID columns.
	_, err := d.pool.Exec(ctx,
		`INSERT INTO tasks (id, trace_id, status, created_at, updated_at)
		 VALUES ($1::uuid, $2, 'pending', $3, $3)`,
		id, traceID, now,
	)
	if err != nil {
		return fmt.Errorf("db.InsertTask %s: %w", id, err)
	}
	return nil
}
