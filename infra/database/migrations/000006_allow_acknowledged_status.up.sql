-- 000006_allow_acknowledged_status.up.sql
ALTER TABLE healing_events DROP CONSTRAINT chk_status;
ALTER TABLE healing_events ADD CONSTRAINT chk_status CHECK (status IN ('active', 'acknowledged', 'resolved'));
