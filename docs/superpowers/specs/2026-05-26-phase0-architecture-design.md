# Phase 0 — Architecture Design

**Date:** 2026-05-26  
**Status:** Approved  
**Scope:** Monorepo structure, protobuf contracts, event schema, trace propagation contract

---

## 1. Monorepo Structure

```
runtime-platform/
├── services/
│   ├── api/              # Go API (Phase 1)
│   └── worker/           # Go Worker (Phase 2)
├── ai-runtime/           # Python FastAPI + LangGraph (Phase 3)
├── frontend/             # Next.js dashboard (Phase 1)
├── proto/
│   ├── buf.yaml
│   ├── buf.gen.yaml
│   └── events/
│       └── v1/
│           └── events.proto
├── shared/
│   └── gen/
│       ├── go/
│       └── python/
├── infra/                # Terraform + LocalStack (Phase 6+)
├── observability/        # OTEL Collector, Prometheus, Grafana (Phase 4+)
├── docs/
│   ├── architecture/
│   ├── contracts/
│   └── runbooks/
└── docker-compose.yml
```

**Rationale:**
- Organized by technical layer, not domain — aligns with the agent map in CLAUDE.md
- `proto/` contains only source; generated stubs go to `shared/gen/` — avoids mixing source and generated code, improves CI hygiene
- Future-phase folders (`infra/`, `observability/`) exist as empty placeholders — evolutionary without overengineering
- `docs/` mirrors operational maturity: architecture decisions, contracts, runbooks

---

## 2. Protobuf Event Contract

**File:** `proto/events/v1/events.proto`

### Envelope

```protobuf
syntax = "proto3";

package events.v1;

import "google/protobuf/timestamp.proto";

option go_package = "github.com/runtime-platform/shared/gen/go/events/v1;eventsv1";

enum EventType {
  EVENT_TYPE_UNSPECIFIED = 0;
  TASK_CREATED           = 1;
  TASK_QUEUED            = 2;
  TASK_PROCESSING        = 3;
  TASK_COMPLETED         = 4;
  TASK_FAILED            = 5;
  WORKER_HEARTBEAT       = 6;
  WORKER_UNHEALTHY       = 7;
  RETRY_SCHEDULED        = 8;
  DLQ_MOVED              = 9;
}

enum WorkerStatus {
  WORKER_STATUS_UNSPECIFIED = 0;
  WORKER_STATUS_HEALTHY     = 1;
  WORKER_STATUS_BUSY        = 2;
  WORKER_STATUS_UNHEALTHY   = 3;
}

message Event {
  string                    event_id        = 1;
  EventType                 event_type      = 2;
  string                    trace_id        = 3;  // 32-char hex extracted from W3C traceparent
  string                    source          = 4;  // allowed: api | worker | ai-runtime | frontend | scheduler
  google.protobuf.Timestamp timestamp       = 5;
  uint32                    payload_version = 6;

  oneof payload {
    TaskCreated     task_created     = 10;
    TaskQueued      task_queued      = 11;
    TaskProcessing  task_processing  = 12;
    TaskCompleted   task_completed   = 13;
    TaskFailed      task_failed      = 14;
    WorkerHeartbeat worker_heartbeat = 15;
    WorkerUnhealthy worker_unhealthy = 16;
    RetryScheduled  retry_scheduled  = 17;
    DLQMoved        dlq_moved        = 18;
  }
}
```

### Payload Messages

```protobuf
message TaskCreated {
  string task_id       = 1;
  string input_preview = 2;  // truncated preview for logs/UI
  string input_ref     = 3;  // S3/object reference for full payload (large inputs)
  string source        = 4;
}

message TaskQueued {
  string task_id    = 1;
  string queue_name = 2;
  uint32 attempt    = 3;
}

message TaskProcessing {
  string task_id   = 1;
  string worker_id = 2;
  google.protobuf.Timestamp started_at = 3;
}

message TaskCompleted {
  string task_id     = 1;
  string worker_id   = 2;
  string output_ref  = 3;  // S3 key
  uint64 duration_ms = 4;
}

message TaskFailed {
  string task_id   = 1;
  string worker_id = 2;
  string error     = 3;
  uint32 attempt   = 4;
  bool   retryable = 5;
}

message WorkerHeartbeat {
  string       worker_id       = 1;
  WorkerStatus status          = 2;
  uint64       tasks_processed = 3;
  uint64       memory_bytes    = 4;
}

message WorkerUnhealthy {
  string worker_id         = 1;
  string reason            = 2;
  google.protobuf.Timestamp last_heartbeat_at = 3;
}

message RetryScheduled {
  string task_id    = 1;
  uint32 attempt    = 2;
  google.protobuf.Timestamp scheduled_at = 3;
  string reason     = 4;
}

message DLQMoved {
  string task_id        = 1;
  string reason         = 2;
  string original_queue = 3;
  google.protobuf.Timestamp moved_at = 4;
}
```

### buf config

**`buf.yaml`:**
```yaml
version: v2
modules:
  - path: .
deps:
  - buf.build/googleapis/googleapis
```

**`buf.gen.yaml`:**
```yaml
version: v2
plugins:
  - remote: buf.build/protocolbuffers/go
    out: ../shared/gen/go
    opt:
      - paths=source_relative
  - remote: buf.build/protocolbuffers/python
    out: ../shared/gen/python
    opt:
      - paths=source_relative
```

**Design decisions:**
- `oneof payload` enforces type safety across Go and Python — no bytes/JSON carrier
- `EventType` and `WorkerStatus` as enums prevent typo drift between services
- `google.protobuf.Timestamp` is the correct type for timestamps (not string)
- `payload_version` enables schema evolution without breaking consumers
- `event_id` enables deduplication and replay
- `input_preview` + `input_ref` separates display from storage — avoids large payloads in events
- `memory_bytes` (not `memory_mb`) matches observability/runtime conventions and preserves precision
- `source` allowed values are explicit: `api`, `worker`, `ai-runtime`, `frontend`, `scheduler`

---

## 3. Trace Propagation Contract

**File:** `docs/contracts/trace-propagation.md`

### Trace ID Format

- 32 lowercase hexadecimal characters
- Extracted from the W3C `traceparent` header: `00-{trace_id}-{parent_id}-{flags}`
- Example: `4bf92f3577b34da6a3ce929d0e0e4736`
- Compatible with OpenTelemetry trace context specification

### Propagation Flow

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

### Rules

1. **No service may create a new root `trace_id` mid-pipeline.** All downstream operations must preserve the original `trace_id`. Child spans and retry spans are allowed within the same trace.

2. **Missing `trace_id` must:**
   - Emit a structured warning log
   - Increment a telemetry counter (`events.trace_missing_total`)
   - Mark the event as invalid
   - Optionally route to DLQ

3. **Queue boundaries preserve trace continuity but create new spans.** Workers must create child spans from the propagated trace context — not new root spans.

4. **SQS message attributes carry the full `traceparent` header.** Trace context must not depend solely on the event payload — attributes enable debugging at the queue level without deserializing the message body.

5. **Frontend displays `trace_id` on every SSE event.** Every user-visible action is traceable from the UI back to the originating HTTP request.

6. **S3 artifacts are stored with `trace_id` in object metadata.** Enables post-mortem correlation and replay of any artifact back to its originating trace.

7. **Trace propagation failures must never stop task processing.** Observability failures must degrade gracefully — telemetry is a side effect, not a dependency of the runtime.

---

## 4. Implementation Approach

**Approach B — Scaffold + compiled contracts:**
- Create real monorepo folder structure
- Write `.proto` files
- Configure `buf.yaml` / `buf.gen.yaml`
- Generate Go and Python stubs into `shared/gen/`
- Write trace contract doc

At the end of Phase 0: compilable stubs exist, folder structure is in place, Phase 1 inherits directly.

---

## Definition of Done — Phase 0

- [ ] Monorepo structure created
- [ ] `proto/events/v1/events.proto` written
- [ ] `buf.yaml` and `buf.gen.yaml` configured
- [ ] Go stubs generated in `shared/gen/go/`
- [ ] Python stubs generated in `shared/gen/python/`
- [ ] `docs/contracts/trace-propagation.md` written
- [ ] No compilation errors from `buf generate`
