-- 000006_allow_acknowledged_status.down.sql
UPDATE healing_events SET status = 'active' WHERE status = 'acknowledged';
ALTER TABLE healing_events DROP CONSTRAINT chk_status;
ALTER TABLE healing_events ADD CONSTRAINT chk_status CHECK (status IN ('active', 'resolved'));
