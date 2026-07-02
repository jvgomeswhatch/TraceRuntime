-- 000009_create_chaos_runs.up.sql
-- Chaos Run as a first-class entity: isolates chaos data from operational dashboards.

CREATE TABLE IF NOT EXISTS chaos_runs (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id            TEXT NOT NULL UNIQUE,
    scenario          TEXT NOT NULL,
    status            TEXT NOT NULL DEFAULT 'queued'
                          CHECK (status IN ('queued', 'running', 'completed', 'failed', 'cancelled', 'deleted', 'purged')),
    git_commit        TEXT,
    environment       TEXT NOT NULL DEFAULT 'local-docker',
    total_scenarios   INT NOT NULL DEFAULT 0,
    passed_scenarios  INT NOT NULL DEFAULT 0,
    warned_scenarios  INT NOT NULL DEFAULT 0,
    failed_scenarios  INT NOT NULL DEFAULT 0,
    duration_seconds  DOUBLE PRECISION NOT NULL DEFAULT 0,
    queued_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    started_at        TIMESTAMPTZ,
    completed_at      TIMESTAMPTZ,
    deleted_at        TIMESTAMPTZ,
    purged_at         TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_chaos_runs_status ON chaos_runs(status);
CREATE INDEX IF NOT EXISTS idx_chaos_runs_queued_at ON chaos_runs(queued_at DESC);

-- FK on tasks: operational queries filter WHERE chaos_run_id IS NULL
ALTER TABLE tasks ADD COLUMN IF NOT EXISTS chaos_run_id UUID REFERENCES chaos_runs(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_tasks_chaos_run_id ON tasks(chaos_run_id) WHERE chaos_run_id IS NOT NULL;

-- FK on healing_events: correlates watchdog events to the chaos run that caused them
ALTER TABLE healing_events ADD COLUMN IF NOT EXISTS chaos_run_id UUID REFERENCES chaos_runs(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_healing_events_chaos_run_id ON healing_events(chaos_run_id) WHERE chaos_run_id IS NOT NULL;
