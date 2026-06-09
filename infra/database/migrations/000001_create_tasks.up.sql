CREATE TABLE IF NOT EXISTS tasks (
    id                    UUID PRIMARY KEY,
    trace_id              TEXT NOT NULL,
    status                TEXT NOT NULL DEFAULT 'pending'
                              CHECK (status IN ('pending', 'processing', 'completed', 'failed')),
    created_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    processing_started_at TIMESTAMPTZ,
    completed_at          TIMESTAMPTZ,
    artifact_key          TEXT,
    error_message         TEXT
);

CREATE INDEX IF NOT EXISTS idx_tasks_status     ON tasks(status);
CREATE INDEX IF NOT EXISTS idx_tasks_trace_id   ON tasks(trace_id);
CREATE INDEX IF NOT EXISTS idx_tasks_created_at ON tasks(created_at DESC);
