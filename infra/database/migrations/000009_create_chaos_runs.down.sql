-- 000009_create_chaos_runs.down.sql
ALTER TABLE healing_events DROP COLUMN IF EXISTS chaos_run_id;
ALTER TABLE tasks DROP COLUMN IF EXISTS chaos_run_id;
DROP TABLE IF EXISTS chaos_runs;
