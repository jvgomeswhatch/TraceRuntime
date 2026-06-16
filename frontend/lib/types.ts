export interface SSEEvent {
  event_id: string;
  event_type: string;
  trace_id: string;
  traceparent: string;
  task_id: string;
  timestamp: string;
  source: string;
  output?: string;
  model?: string;
  execution_status?: string;
  validation_status?: string;
  inference_duration_ms?: number;
  error_reason?: string;
}

export interface CreateTaskResponse {
  task_id: string;
  trace_id: string;
  traceparent: string;
}

export interface WorkerStatus {
  worker_id: string;
  last_seen_at: string;
  tasks_processed: number;
  tasks_failed: number;
  goroutines: number;
  uptime_seconds: number;
  status: "healthy" | "stale";
}

export interface HealingEvent {
  id: string;
  event_type: string;
  severity: "info" | "warning" | "critical";
  source: string;
  status: "active" | "resolved";
  worker_id?: string;
  details: Record<string, unknown>;
  created_at: string;
  resolved_at?: string;
}

export interface OperationsSummary {
  workers: WorkerStatus[];
  active_events: HealingEvent[];
  recent_events: HealingEvent[];
  timestamp: string;
}

export interface HealingSSEEvent {
  event_type: string;
  severity: "info" | "warning" | "critical";
  status: "active" | "resolved";
  healing_event_id: string;
  details: Record<string, unknown>;
  source: string;
  timestamp: string;
}
