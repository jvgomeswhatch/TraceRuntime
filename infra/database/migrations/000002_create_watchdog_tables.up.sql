CREATE TABLE IF NOT EXISTS worker_heartbeats (
    worker_id       TEXT PRIMARY KEY,
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    tasks_processed BIGINT NOT NULL DEFAULT 0,
    tasks_failed    BIGINT NOT NULL DEFAULT 0,
    current_task_id UUID,
    goroutines      INT NOT NULL DEFAULT 0,
    uptime_seconds  BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS healing_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type  TEXT NOT NULL,
    severity    TEXT NOT NULL,
    source      TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'active',
    worker_id   TEXT,
    details     JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ,
    CONSTRAINT chk_severity CHECK (severity IN ('info', 'warning', 'critical')),
    CONSTRAINT chk_status   CHECK (status IN ('active', 'resolved'))
);

CREATE INDEX IF NOT EXISTS idx_healing_events_type_created
    ON healing_events (event_type, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_healing_events_status_created
    ON healing_events (status, created_at DESC);
