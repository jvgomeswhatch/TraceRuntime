# Trace Propagation Contract

This document is the authoritative reference for how trace context flows through
the runtime-platform. All services must follow these rules exactly.

---

## Trace ID Format

- 32 lowercase hexadecimal characters
- Extracted from the W3C `traceparent` header: `00-{trace_id}-{parent_id}-{flags}`
- Example: `4bf92f3577b34da6a3ce929d0e0e4736`
- Compatible with the OpenTelemetry trace context specification

The `trace_id` is NOT the full `traceparent` header. It is the 32-char segment only.

---

## Propagation Flow

```
HTTP Request
  → Header: traceparent: 00-{trace_id}-{parent_id}-01
  → Go API extracts trace context, creates the initial application span
  → Injects trace_id into Event.trace_id when publishing to queue
  → Go Worker reads Event.trace_id, reconstructs OTEL context, opens child span
  → Injects traceparent into HTTP request to AI Runtime
  → Python FastAPI extracts context, propagates through LangGraph nodes
  → Ollama call inherits the same trace as a child span
  → Result persisted to S3 with trace_id in object metadata
  → SSE event carries trace_id to frontend
```

---

## Rules

### Rule 1 — No new root traces mid-pipeline

No service may create a new root `trace_id` mid-pipeline.
All downstream operations must preserve the original `trace_id`.
Child spans and retry spans are allowed within the same trace.

### Rule 2 — Missing trace_id handling

If a service receives an event without a `trace_id`, it must:
- Emit a structured warning log: `{ "level": "warn", "msg": "missing trace_id", "event_id": "..." }`
- Increment the telemetry counter: `events_trace_missing_total`
- Mark the event as invalid in its own processing state
- Optionally route the event to DLQ depending on phase configuration

### Rule 3 — Queue boundaries create child spans, not root spans

Queue boundaries preserve trace continuity but create new spans.
Workers must create child spans from the propagated trace context — not new root spans.

```
# Correct
worker_span = tracer.start_span("worker.process", context=propagated_ctx)

# Wrong
worker_span = tracer.start_span("worker.process")  # creates new root trace
```

### Rule 4 — SQS attributes carry the full traceparent

SQS message attributes must carry the full `traceparent` header.
Trace context must not depend solely on the Event payload.
This enables debugging at the queue level without deserializing the message body.

SQS attribute name: `traceparent`
SQS attribute type: `String`

### Rule 5 — Frontend displays trace_id on every SSE event

Every SSE event emitted to the frontend must include the `trace_id` field.
This makes every user-visible action traceable back to its originating HTTP request.

### Rule 6 — S3 artifacts carry trace_id in object metadata

All S3 objects written by the platform must include `trace_id` in their object metadata.
This enables post-mortem correlation and replay of any artifact back to its originating trace.

S3 metadata key: `x-trace-id`

### Rule 7 — Observability failures degrade gracefully

Trace propagation failures must never stop task processing.
Observability is a side effect of the runtime, not a dependency.

If span creation fails, log the error and continue processing.
If trace context extraction fails, apply Rule 2 and continue processing.

---

## Source Values

The `Event.source` field must be one of:

| Value        | Description                        |
|--------------|------------------------------------|
| `api`        | Go HTTP API                        |
| `worker`     | Go async worker                    |
| `ai-runtime` | Python FastAPI + LangGraph runtime |
| `frontend`   | Next.js dashboard (rare)           |
| `scheduler`  | Future: scheduled task runner      |

Any other value is invalid and must be treated as `EVENT_TYPE_UNSPECIFIED`.
