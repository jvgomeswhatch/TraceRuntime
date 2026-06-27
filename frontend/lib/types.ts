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
  prompt_tokens?: number;
  completion_tokens?: number;
  tokens_per_second?: number;
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

export interface RuntimeMetrics {
  window_seconds: number;
  p95_latency_ms: number;
  avg_latency_ms: number;
  success_rate: number;
  error_rate: number;
  completed: number;
  failed: number;
  queue_depth: number;
}

export interface CapacityLatencyBucket {
  p50: number;
  p95: number;
  p99: number;
  min: number;
  max: number;
}

export interface CapacityReport {
  timestamp: string;
  config: {
    tasks: number;
    rate: number;
    concurrency: number;
    api_url: string;
  };
  results: {
    duration_seconds: number;
    tasks_submitted: number;
    tasks_completed: number;
    tasks_failed: number;
    throughput_rps: number;
    latency_ms: {
      api_request: CapacityLatencyBucket;
      submit_to_processing: CapacityLatencyBucket;
      processing_duration: CapacityLatencyBucket;
      end_to_end: CapacityLatencyBucket;
    };
  };
  queue_metrics: {
    max_visible_messages: number;
    max_inflight_messages: number;
    backlog_converged: boolean;
  };
  recommendations: {
    visibility_timeout: {
      current: number;
      recommended: number;
      formula: string;
    };
    queue_max_depth: {
      current: number;
      recommendation: string;
      reason: string;
    };
    worker_concurrency: {
      current: number;
      observation: string;
    };
  };
  status: "PASS" | "WARNING" | "FAIL";
  status_criteria: {
    error_rate_pct: number;
    timeouts: number;
    backlog_converged: boolean;
    dlq_triggered: boolean;
  };
}
