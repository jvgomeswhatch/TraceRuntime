-- 000007_add_replay_fields.down.sql
DROP INDEX IF EXISTS idx_tasks_replay_of;
ALTER TABLE tasks DROP COLUMN IF EXISTS input_artifact_key;
ALTER TABLE tasks DROP COLUMN IF EXISTS input_payload;
ALTER TABLE tasks DROP COLUMN IF EXISTS replay_of;
