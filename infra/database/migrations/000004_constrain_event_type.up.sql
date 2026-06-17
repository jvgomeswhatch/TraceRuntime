ALTER TABLE healing_events ADD CONSTRAINT healing_events_event_type_check CHECK (event_type IN ('worker.stale', 'worker.down', 'queue.lag', 'task.stuck', 'dlq.nonempty', 'dlq.growing'));
