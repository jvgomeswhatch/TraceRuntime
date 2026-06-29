-- 000007_add_replay_fields.up.sql
ALTER TABLE tasks ADD COLUMN replay_of UUID;
ALTER TABLE tasks ADD COLUMN input_payload TEXT;
ALTER TABLE tasks ADD COLUMN input_artifact_key TEXT;
CREATE INDEX idx_tasks_replay_of ON tasks(replay_of);
