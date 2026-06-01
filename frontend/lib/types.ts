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
  inference_duration_ms: number;
  error_reason?: string;
}

export interface CreateTaskResponse {
  task_id: string;
  trace_id: string;
  traceparent: string;
}
