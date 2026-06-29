-- 000008_remove_event_type_check.up.sql
ALTER TABLE healing_events DROP CONSTRAINT IF EXISTS healing_events_event_type_check;
