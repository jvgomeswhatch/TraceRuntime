# Phase 0 — Monorepo + Contracts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Bootstrap the `runtime-platform` monorepo with folder structure, compiled protobuf contracts, and the trace propagation reference document — giving Phase 1 a clean, compilable foundation.

**Architecture:** Monorepo organized by technical layer (`services/`, `ai-runtime/`, `frontend/`, `proto/`, `shared/`, `infra/`, `observability/`, `docs/`). Proto source lives in `proto/events/v1/`; generated stubs are emitted to `shared/gen/go/` and `shared/gen/python/` via `buf generate`. The trace contract is a standalone markdown doc consumed by all future phases.

**Tech Stack:** `buf` CLI (v2), protobuf 3, `google.protobuf.Timestamp`, Go module path `github.com/runtime-platform/shared/gen/go/events/v1`

---

## Prerequisites

Before starting, verify these tools are installed:

```bash
# buf CLI
buf --version
# Expected: v1.x or v2.x

# git
git --version
```

If `buf` is not installed:
```bash
# macOS/Linux via brew
brew install bufbuild/buf/buf

# or via curl
curl -sSL https://github.com/bufbuild/buf/releases/latest/download/buf-Linux-x86_64 -o /usr/local/bin/buf && chmod +x /usr/local/bin/buf

# Windows (via scoop)
scoop install buf
```

---

## Task 1: Create Monorepo Folder Structure

**Files:**
- Create: `services/api/.gitkeep`
- Create: `services/worker/.gitkeep`
- Create: `ai-runtime/.gitkeep`
- Create: `frontend/.gitkeep`
- Create: `infra/.gitkeep`
- Create: `observability/.gitkeep`
- Create: `shared/gen/go/.gitkeep`
- Create: `shared/gen/python/.gitkeep`
- Create: `docs/architecture/.gitkeep`
- Create: `docs/contracts/.gitkeep`
- Create: `docs/runbooks/.gitkeep`
- Create: `docker-compose.yml`
- Create: `.gitignore`

> All commands run from the repo root (`EventDrive/`).

- [ ] **Step 1: Create all service and runtime directories**

```bash
mkdir -p services/api \
         services/worker \
         ai-runtime \
         frontend \
         infra \
         observability \
         shared/gen/go \
         shared/gen/python \
         docs/architecture \
         docs/contracts \
         docs/runbooks \
         proto/events/v1
```

- [ ] **Step 2: Add .gitkeep to empty directories so git tracks them**

```bash
touch services/api/.gitkeep \
      services/worker/.gitkeep \
      ai-runtime/.gitkeep \
      frontend/.gitkeep \
      infra/.gitkeep \
      observability/.gitkeep \
      shared/gen/go/.gitkeep \
      shared/gen/python/.gitkeep \
      docs/architecture/.gitkeep \
      docs/contracts/.gitkeep \
      docs/runbooks/.gitkeep
```

- [ ] **Step 3: Create the root docker-compose.yml placeholder**

```bash
cat > docker-compose.yml << 'EOF'
# docker-compose.yml
# Populated incrementally starting from Phase 5.
# Each phase adds its own services with memory and CPU limits.
version: "3.9"
services: {}
EOF
```

- [ ] **Step 4: Create .gitignore**

```bash
cat > .gitignore << 'EOF'
# OS
.DS_Store
Thumbs.db

# Go
*.exe
*.test
*.out
vendor/

# Python
__pycache__/
*.pyc
*.pyo
.venv/
venv/
dist/
*.egg-info/

# Node
node_modules/
.next/
out/

# Generated protobuf stubs — regenerate with: buf generate
# Uncomment the lines below if you prefer not to commit generated code.
# shared/gen/go/
# shared/gen/python/

# Local env
.env
.env.local
EOF
```

- [ ] **Step 5: Verify structure looks correct**

```bash
find . -not -path './.git/*' -not -path './docs/superpowers/*' | sort
```

Expected output (trimmed):
```
.
./.gitignore
./ai-runtime/.gitkeep
./docker-compose.yml
./docs/architecture/.gitkeep
./docs/contracts/.gitkeep
./docs/runbooks/.gitkeep
./frontend/.gitkeep
./infra/.gitkeep
./observability/.gitkeep
./proto/events/v1
./services/api/.gitkeep
./services/worker/.gitkeep
./shared/gen/go/.gitkeep
./shared/gen/python/.gitkeep
```

- [ ] **Step 6: Commit**

```bash
git add .gitignore docker-compose.yml services/ ai-runtime/ frontend/ infra/ observability/ shared/ docs/architecture docs/contracts docs/runbooks proto/
git commit -m "chore: scaffold monorepo structure for Phase 0"
```

---

## Task 2: Write the Protobuf Event Schema

**Files:**
- Create: `proto/events/v1/events.proto`
- Create: `proto/buf.yaml`
- Create: `proto/buf.gen.yaml`

- [ ] **Step 1: Write buf.yaml**

```bash
cat > proto/buf.yaml << 'EOF'
version: v2
modules:
  - path: .
deps:
  - buf.build/googleapis/googleapis
EOF
```

- [ ] **Step 2: Write buf.gen.yaml**

```bash
cat > proto/buf.gen.yaml << 'EOF'
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
EOF
```

- [ ] **Step 3: Write events.proto**

```bash
cat > proto/events/v1/events.proto << 'EOF'
syntax = "proto3";

package events.v1;

import "google/protobuf/timestamp.proto";

option go_package = "github.com/runtime-platform/shared/gen/go/events/v1;eventsv1";

// ─── Enums ───────────────────────────────────────────────────────────────────

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

// ─── Envelope ────────────────────────────────────────────────────────────────

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

// ─── Payloads ────────────────────────────────────────────────────────────────

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
  string                    task_id    = 1;
  string                    worker_id  = 2;
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
  string                    worker_id         = 1;
  string                    reason            = 2;
  google.protobuf.Timestamp last_heartbeat_at = 3;
}

message RetryScheduled {
  string                    task_id      = 1;
  uint32                    attempt      = 2;
  google.protobuf.Timestamp scheduled_at = 3;
  string                    reason       = 4;
}

message DLQMoved {
  string                    task_id        = 1;
  string                    reason         = 2;
  string                    original_queue = 3;
  google.protobuf.Timestamp moved_at       = 4;
}
EOF
```

- [ ] **Step 4: Verify proto file is well-formed (lint)**

```bash
cd proto && buf lint
```

Expected: no output (clean lint). If errors appear, fix before continuing.

- [ ] **Step 5: Commit proto source**

```bash
cd .. && git add proto/
git commit -m "feat(proto): add events/v1 schema with oneof payload and WorkerStatus enum"
```

---

## Task 3: Generate Protobuf Stubs

**Files:**
- Create: `shared/gen/go/events/v1/events.pb.go` (generated)
- Create: `shared/gen/python/events/v1/events_pb2.py` (generated)
- Create: `shared/gen/python/events/v1/events_pb2.pyi` (generated)

- [ ] **Step 1: Update buf dependencies**

```bash
cd proto && buf dep update
```

Expected: creates or updates `proto/buf.lock`. No errors.

- [ ] **Step 2: Run buf generate**

```bash
buf generate
```

Expected: no output. Files appear in `shared/gen/go/` and `shared/gen/python/`.

- [ ] **Step 3: Verify Go stub was generated**

```bash
ls ../shared/gen/go/events/v1/
```

Expected: `events.pb.go` present.

- [ ] **Step 4: Verify Python stub was generated**

```bash
ls ../shared/gen/python/events/v1/
```

Expected: `events_pb2.py` and `events_pb2.pyi` present.

- [ ] **Step 5: Verify Go stub compiles**

```bash
cd ../shared/gen/go && go mod init github.com/runtime-platform/shared/gen/go && go mod tidy
```

Expected: `go.mod` and `go.sum` created, no compile errors.

> Note: `go mod tidy` will pull `google.golang.org/protobuf` as a dependency. This is expected.

- [ ] **Step 6: Commit generated stubs and lock file**

```bash
cd ../../..
git add proto/buf.lock shared/gen/
git commit -m "chore(proto): generate Go and Python stubs from events/v1 schema"
```

---

## Task 4: Write Trace Propagation Contract

**Files:**
- Create: `docs/contracts/trace-propagation.md`

- [ ] **Step 1: Write the trace propagation document**

```bash
cat > docs/contracts/trace-propagation.md << 'EOF'
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
EOF
```

- [ ] **Step 2: Verify the file was written correctly**

```bash
wc -l docs/contracts/trace-propagation.md
```

Expected: ~90 lines.

- [ ] **Step 3: Commit**

```bash
git add docs/contracts/trace-propagation.md
git commit -m "docs: add trace propagation contract with W3C/OTEL rules and source values"
```

---

## Task 5: Final Validation

- [ ] **Step 1: Verify full directory tree is correct**

```bash
find . -not -path './.git/*' -not -path './docs/superpowers/*' -not -path './shared/gen/go/vendor/*' | sort
```

Expected structure:
```
.
./.gitignore
./ai-runtime/.gitkeep
./docker-compose.yml
./docs/architecture/.gitkeep
./docs/contracts/.gitkeep
./docs/contracts/trace-propagation.md
./docs/runbooks/.gitkeep
./frontend/.gitkeep
./infra/.gitkeep
./observability/.gitkeep
./proto/buf.gen.yaml
./proto/buf.lock
./proto/buf.yaml
./proto/events/v1/events.proto
./services/api/.gitkeep
./services/worker/.gitkeep
./shared/gen/go/events/v1/events.pb.go
./shared/gen/go/go.mod
./shared/gen/go/go.sum
./shared/gen/python/events/v1/events_pb2.py
./shared/gen/python/events/v1/events_pb2.pyi
```

- [ ] **Step 2: Re-run buf lint to confirm proto is still clean**

```bash
cd proto && buf lint && echo "lint: OK"
```

Expected: `lint: OK`

- [ ] **Step 3: Re-run buf generate to confirm it is idempotent**

```bash
buf generate && echo "generate: OK"
```

Expected: `generate: OK` — no file changes (stubs are identical).

- [ ] **Step 4: Confirm git history is clean**

```bash
cd .. && git log --oneline
```

Expected (4 commits on top of initial):
```
<hash> docs: add trace propagation contract with W3C/OTEL rules and source values
<hash> chore(proto): generate Go and Python stubs from events/v1 schema
<hash> feat(proto): add events/v1 schema with oneof payload and WorkerStatus enum
<hash> chore: scaffold monorepo structure for Phase 0
```

- [ ] **Step 5: Definition of Done check**

Confirm each item:
- [ ] Monorepo structure created and committed
- [ ] `proto/events/v1/events.proto` written and linted clean
- [ ] `buf.yaml` and `buf.gen.yaml` configured
- [ ] Go stubs generated in `shared/gen/go/events/v1/events.pb.go`
- [ ] Python stubs generated in `shared/gen/python/events/v1/`
- [ ] `docs/contracts/trace-propagation.md` written and committed
- [ ] No compilation errors from `buf generate`

Phase 0 is complete. Phase 1 (Go API + Next.js + SSE) can begin.
