package chaos

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type ChaosRunDB struct {
	pool *pgxpool.Pool
}

func NewChaosRunDB(pool *pgxpool.Pool) *ChaosRunDB {
	return &ChaosRunDB{pool: pool}
}

func (db *ChaosRunDB) Insert(ctx context.Context, runID, scenario, gitCommit, environment string) (string, error) {
	var id string
	err := db.pool.QueryRow(ctx,
		`INSERT INTO chaos_runs (run_id, scenario, status, git_commit, environment, queued_at)
		 VALUES ($1, $2, 'queued', $3, $4, NOW())
		 RETURNING id::text`,
		runID, scenario, gitCommit, environment,
	).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("insert chaos_run %s: %w", runID, err)
	}
	return id, nil
}

func (db *ChaosRunDB) SetRunning(ctx context.Context, dbID string) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE chaos_runs SET status = 'running', started_at = NOW() WHERE id = $1::uuid`,
		dbID,
	)
	return err
}

func (db *ChaosRunDB) SetCompleted(ctx context.Context, dbID string, passed, warned, failed int, totalScenarios int, duration float64) error {
	status := "completed"
	if failed > 0 {
		status = "failed"
	}
	_, err := db.pool.Exec(ctx,
		`UPDATE chaos_runs SET status = $2, completed_at = NOW(),
		 total_scenarios = $3, passed_scenarios = $4, warned_scenarios = $5, failed_scenarios = $6,
		 duration_seconds = $7
		 WHERE id = $1::uuid`,
		dbID, status, totalScenarios, passed, warned, failed, duration,
	)
	return err
}

func (db *ChaosRunDB) SetCancelled(ctx context.Context, dbID string) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE chaos_runs SET status = 'cancelled', completed_at = NOW() WHERE id = $1::uuid`,
		dbID,
	)
	return err
}

// TaskIDsByRun returns all task IDs belonging to a chaos run.
func (db *ChaosRunDB) TaskIDsByRun(ctx context.Context, dbID string) ([]string, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT id::text FROM tasks WHERE chaos_run_id = $1::uuid`, dbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// ArtifactKeysByRun returns all S3 artifact keys for tasks in a chaos run.
func (db *ChaosRunDB) ArtifactKeysByRun(ctx context.Context, dbID string) ([]string, error) {
	rows, err := db.pool.Query(ctx,
		`SELECT artifact_key FROM tasks WHERE chaos_run_id = $1::uuid AND artifact_key IS NOT NULL AND artifact_key != ''`,
		dbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// DeleteArtifacts removes all DB artifacts for a chaos run: healing_events then tasks.
func (db *ChaosRunDB) DeleteArtifacts(ctx context.Context, dbID string) (healingDeleted, tasksDeleted int64, err error) {
	tag, err := db.pool.Exec(ctx,
		`DELETE FROM healing_events WHERE chaos_run_id = $1::uuid`, dbID)
	if err != nil {
		return 0, 0, fmt.Errorf("delete healing_events: %w", err)
	}
	healingDeleted = tag.RowsAffected()

	tag, err = db.pool.Exec(ctx,
		`DELETE FROM tasks WHERE chaos_run_id = $1::uuid`, dbID)
	if err != nil {
		return healingDeleted, 0, fmt.Errorf("delete tasks: %w", err)
	}
	tasksDeleted = tag.RowsAffected()

	return healingDeleted, tasksDeleted, nil
}

// MarkPurged sets the run as purged after all artifacts have been removed.
func (db *ChaosRunDB) MarkPurged(ctx context.Context, dbID string) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE chaos_runs SET status = 'purged', purged_at = NOW() WHERE id = $1::uuid`, dbID)
	return err
}

// SoftDelete marks a run as deleted without removing artifacts.
func (db *ChaosRunDB) SoftDelete(ctx context.Context, dbID string) error {
	_, err := db.pool.Exec(ctx,
		`UPDATE chaos_runs SET status = 'deleted', deleted_at = NOW() WHERE id = $1::uuid AND status NOT IN ('deleted', 'purged')`,
		dbID)
	return err
}

// GetDBIDByRunID returns the UUID of a chaos run by its text run_id.
func (db *ChaosRunDB) GetDBIDByRunID(ctx context.Context, runID string) (string, error) {
	var id string
	err := db.pool.QueryRow(ctx,
		`SELECT id::text FROM chaos_runs WHERE run_id = $1`, runID).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("get chaos_run by run_id %s: %w", runID, err)
	}
	return id, nil
}

type ChaosRunSummary struct {
	ID               string
	RunID            string
	Scenario         string
	Status           string
	GitCommit        string
	TotalScenarios   int
	PassedScenarios  int
	WarnedScenarios  int
	FailedScenarios  int
	DurationSeconds  float64
	QueuedAt         time.Time
	StartedAt        *time.Time
	CompletedAt      *time.Time
}

func (db *ChaosRunDB) List(ctx context.Context, limit int) ([]ChaosRunSummary, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := db.pool.Query(ctx,
		`SELECT id::text, run_id, scenario, status, COALESCE(git_commit, ''),
		        total_scenarios, passed_scenarios, warned_scenarios, failed_scenarios,
		        duration_seconds, queued_at, started_at, completed_at
		 FROM chaos_runs
		 WHERE status NOT IN ('deleted', 'purged')
		 ORDER BY queued_at DESC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var runs []ChaosRunSummary
	for rows.Next() {
		var r ChaosRunSummary
		if err := rows.Scan(&r.ID, &r.RunID, &r.Scenario, &r.Status, &r.GitCommit,
			&r.TotalScenarios, &r.PassedScenarios, &r.WarnedScenarios, &r.FailedScenarios,
			&r.DurationSeconds, &r.QueuedAt, &r.StartedAt, &r.CompletedAt); err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}
