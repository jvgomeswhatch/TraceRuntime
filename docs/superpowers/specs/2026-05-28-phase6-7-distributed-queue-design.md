# Phase 6+7 — Distributed Queue & IaC Design

**Date:** 2026-05-28
**Status:** Draft — awaiting user approval

---

## Overview

Phase 6+7 transitions TraceRuntime from an in-process deterministic queue to a distributed message queue with durable semantics. This is not a migration to AWS APIs. It is the transition of the system to real distributed semantics: retries, redelivery, eventual consistency, partial failures, queue lag, and automatic recovery.

The goal is to keep the system operationally comprehensible after exiting the in-memory deterministic model — not to maximize feature surface.

**Phase 6A:** Infrastructure base — LocalStack, SQS, S3, separated worker container, trace propagation preserved.
**Phase 6B:** Operational validation — lag visibility, retry/DLQ behavior, crash recovery, memory impact measurement.
**Phase 7:** IaC consolidation — Terraform replaces bootstrap script. Resources identical to what was already validated.

---

## What Changes

- Worker becomes a separate binary and container (`services/worker/`)
- SQS replaces the Go channel as task transport
- S3 persists AI runtime outputs
- LocalStack hosts SQS and S3 locally
- Terraform provisions LocalStack resources (after operational validation)

## What Does Not Change

- SSE broker remains in-process in the API service
- W3C trace context propagation via `traceparent`
- `inmemory` mode preserved via `QUEUE_BACKEND` env var
- Admission control deferred — observe lag first, calibrate thresholds later
- SNS: out of scope for this phase

---

## Architectural Decisions

### 1. Admission Control — Observation-First Mode

The current in-process queue rejects immediately via HTTP 503 when the channel is full. With SQS, this property is lost: `SendMessage` always accepts, and depth is only approximated via `GetQueueAttributes`.

**Phase 6A operates in observation-first mode.** The API publishes to SQS without depth checks. This is a temporary and deliberate loss of a property the system previously held. It is documented explicitly as a known regression, not an omission.

Phase 6B reintroduces rejection after observing real saturation behavior:
- `queue_depth_current` (from `ApproximateNumberOfMessages`) provides the lag signal
- Thresholds are calibrated from observed behavior, not set speculatively
- The rejection gate (`QUEUE_MAX_DEPTH`) is introduced as a configurable parameter after calibration

**Important caveat on SQS metrics:** `ApproximateNumberOfMessages` and `ApproximateNumberOfMessagesNotVisible` are not real-time counters. They are periodically updated and may lag behind actual state. Any admission control built on them must account for this imprecision — treating them as trend signals, not exact gates.

### 2. Worker Polling Strategy

Long polling with `WaitTimeSeconds=20`. Rationale:
- Blocks up to 20s if no messages, reducing empty-poll noise
- Returns immediately when messages arrive
- `MaxNumberOfMessages=1` initially — one task at a time, deterministic behavior

**Important:** `MaxMessages=1` is an initial validation decision, not a permanent architecture. It makes retry/visibility/DLQ behavior easier to observe in isolation. Once behavior is understood, batch polling can be introduced. Keep this explicit in operational documentation.

### 3. Visibility Timeout

`VisibilityTimeout` is a configuration value, not a hardcoded constant. Starting point: 150s (120s AI inference timeout + 30s margin).

This value must be calibrated against observed AI runtime p95 latency during Phase 6B. If AI runtime variance increases, the timeout must increase. If too conservative, messages accumulate invisibly (counted in `ApproximateNumberOfMessagesNotVisible`). Treat as an operational parameter, not an architectural commitment.

### 4. Retry and DLQ

`maxReceiveCount=3`. After 3 failed deliveries (message not deleted within visibility timeout), SQS moves the message to the DLQ automatically.

Failure scenarios that trigger DLQ:
- Worker crash during processing (visibility timeout expires, message redelivered, eventually exhausted)
- AI runtime timeout (120s deadline exceeded, worker does not delete message)
- Permanent worker failure (container down, message redelivered 3× then DLQ)

**DLQ visibility in Phase 6A:** DLQ depth is exposed as a Prometheus metric (`queue_dlq_depth`) via periodic polling (low-frequency, ~30s). No dedicated SSE stream for DLQ events in Phase 6A — this is intentional. DLQ is an operational signal visible in the dashboard, not a realtime frontend event. Real-time DLQ notification can be added in a later phase if operational need is demonstrated.

### 5. Worker → API Communication (SSE Events)

The worker does not own the SSE broker. To publish realtime events to the frontend, it calls an internal HTTP endpoint on the API:

```
POST /internal/events
{"type": "task.completed", "trace_id": "...", "payload": {...}}
```

API receives and publishes to the existing SSE broker.

**Critical constraint:** SSE publication is best-effort and must never block task processing. If the API is degraded, the worker logs the failure and continues. The internal HTTP call has a short timeout (2s). A failed SSE publish does not affect task outcome — the task still completes and the output is written to S3.

This introduces a new synchronous dependency. The failure mode is visible degradation of realtime updates, not silent data loss. SSE is already best-effort within the API (slow subscribers are dropped). The alternative — SNS fanout — adds complexity without operational benefit at this scale.

### 6. Trace Propagation via SQS

SQS has no native HTTP headers. The `traceparent` is sent as a SQS Message Attribute:

```json
{
  "traceparent": {
    "DataType": "String",
    "StringValue": "00-<traceId>-<spanId>-01"
  }
}
```

Worker extracts the attribute on receive, reconstructs OTEL context via `MapCarrier`, creates a child span. Same W3C semantics, different carrier. The same `trace_id` propagates end-to-end from HTTP request through SQS through AI runtime to frontend SSE event.

### 7. S3 for AI Outputs

S3 is the artifact store for AI inference outputs. It is not a general event store or payload transport.

Worker writes the AI output to S3 after successful inference:
```
s3://traceruntime-outputs/<trace_id>/<task_id>.json
```

The S3 key is included in the `task.completed` SSE event so the frontend can reference the artifact. The SQS message carries only the minimal envelope — not the payload.

**Current state:** Tasks live in memory/SQS and are not persisted. S3 is the only durable store for task output in this phase.

**Architectural direction (not yet implemented):**

- PostgreSQL will be the system of record for task state and artifact metadata.
- S3 remains the artifact store for large or binary outputs (PDFs, images, audio, and other outputs that do not belong in a relational database).
- Every future implementation that writes to S3 must maintain traceability between task, trace, and artifact — no artifact should exist in S3 without a corresponding record in PostgreSQL.
- The integration of PostgreSQL into the worker runtime is out of scope for Phase 6. It is the explicit responsibility of a future phase.

### 8. inmemory Fallback Mode

`QUEUE_BACKEND=inmemory` preserves the existing channel-based queue for local development without LocalStack, deterministic unit tests, and regression baseline.

`QUEUE_BACKEND=sqs` activates the SQS path.

**These modes do not have identical behavior.** inmemory and SQS differ in: timing, delivery guarantees, retry semantics, ordering, and visibility mechanics. The contract between modes is:
- Same message envelope (protobuf contract preserved)
- Same trace context propagation (`traceparent` extracted identically)
- Same expected UX outcome (task appears as completed in frontend)

The behavioral differences are a feature, not a bug — they expose real distributed semantics that inmemory cannot simulate.

In compose, always use `QUEUE_BACKEND=sqs`. inmemory mode is intended for local development without Docker and for unit tests.

### 9. Terraform Lifecycle

Terraform is not part of the Docker Compose lifecycle. Compose does not invoke `terraform apply` on startup. This would couple environment startup to Terraform runtime, making debugging harder and startup more fragile.

The provisioning model:
- `scripts/bootstrap.sh` — creates SQS queues, DLQ, S3 bucket via awslocal CLI (fast, no state)
- `make infra-apply` — runs `terraform apply` explicitly when IaC needs to be validated or reproduced
- Compose depends on LocalStack being healthy, not on Terraform having run

This keeps compose startup fast and debuggable. Terraform validates reproducibility independently.

### 10. Idempotency — Known Gap and Future Direction

Duplicate delivery is possible with SQS at-least-once semantics. Triggers: visibility timeout expiration, delete failure, network partition during delete.

**Single worker does not eliminate duplicate delivery risk.** It reduces probability but does not prevent it. A message can be delivered twice if the worker processes it but fails to delete before visibility expiration.

This is a documented known gap in Phase 6. Future mitigation direction (not implemented now):
- Deduplicate by `task_id` using a short-lived processed-task store (Redis or PostgreSQL)
- State transitions: `pending → processing → completed` with idempotent state guards
- Reject re-processing of tasks already in terminal state

---

## Memory Budget

Running estimate with full stack:

| Service | Limit |
|---|---|
| api | 256m |
| worker | 128m |
| ai-runtime | 768m |
| localstack | 512m |
| otel-collector | 128m |
| prometheus | 256m |
| grafana | 256m |
| tempo | 256m |
| loki | 256m |
| frontend | 256m |
| **Total (compose)** | **~3.07GB** |

Ollama runs on host outside compose. Models (Qwen/DeepSeek 6B) consume 4–6GB. Total on machine: ~7–9GB. Within 14GB constraint.

**Known instability risk:** LocalStack + full observability stack + Ollama simultaneously is a diagnosed source of instability. Use the `no-ai` profile during Phase 6A infrastructure development. Only bring ai-runtime in once SQS integration is validated.

---

## Docker Compose Profiles

**Single worker service** — `AI_RUNTIME_ENABLED` controls behavior, not profiles:
- `AI_RUNTIME_ENABLED=false` (default in compose) — worker uses mock output. Phase 6A default.
- `AI_RUNTIME_ENABLED=true` — worker calls ai-runtime. Set explicitly via `.env` or env var override.
- No override compose files. One compose, one worker, one operational path.

**`core`** — minimum viable distributed system:
- api, worker (mock mode), localstack, frontend
- otel-collector comes from `infra/observability/docker-compose.yml` (shared network)

**`full`** — everything including ai-runtime:
- All services. Set `AI_RUNTIME_ENABLED=true` via `.env` to enable real inference.

**`no-ai`** — alias for core intent: same as `core`, AI_RUNTIME_ENABLED=false by default.
- Kept as profile label for clarity, not as a separate service or override file.

**Startup order (no-ai / core):**
1. `docker network create traceruntime` (if not exists)
2. `docker compose -f infra/observability/docker-compose.yml up -d` (otel-collector, prometheus, etc.)
3. `docker compose --profile no-ai up -d` (localstack, api, worker, frontend)
4. `bash scripts/bootstrap.sh` (create SQS queues + S3 bucket)

Services tolerate otel-collector being unavailable — spans are dropped silently, startup does not block.

Usage:
```bash
# Phase 6A development (recommended)
docker compose --profile no-ai up

# Full stack with AI (after Phase 6A validated)
AI_RUNTIME_ENABLED=true docker compose --profile full up
```

---

## New Metrics

| Metric | Owner | Purpose |
|---|---|---|
| `queue_depth_current` | worker (poll 10s) | primary lag signal — approximate, not real-time |
| `queue_inflight_current` | worker | messages currently invisible (being processed) |
| `queue_dlq_depth` | worker (poll 30s) | DLQ accumulation — triggers manual investigation |
| `queue_visibility_expired_total` | worker | redeliveries due to timeout (self-reported) |
| `sqs_receive_duration_seconds` | worker | long poll latency |
| `worker_task_duration_seconds` | worker | end-to-end processing time per task |
| `worker_sse_publish_errors_total` | worker | failed internal SSE calls |

`queue_depth_current` is the primary signal of this phase. It replaces `queue_capacity` as the main saturation indicator. Interpreted as a trend signal, not a precise counter — SQS metrics are approximate.

Queue lag duration = `queue_depth_current × avg(worker_task_duration_seconds)`. This is the key operational concept: not how full the queue is, but how long tasks wait.

All metrics exposed via `/metrics` on the worker service. Scraped by Prometheus.

---

## Service Structure

### `services/worker/`

New Go binary. Minimal scope:

```
services/worker/
  cmd/worker/main.go         — bootstrap, signal handling, graceful shutdown
  internal/queue/sqs.go      — SQS receive/delete, message attribute extraction
  internal/queue/inmemory.go — channel-based fallback (extracted from api)
  internal/processor/        — task processing logic (moved from api worker)
  internal/metrics/          — Prometheus metrics
  internal/telemetry/        — OTEL setup (same pattern as api)
```

No new abstraction layer over queue implementations. Both `sqs.go` and `inmemory.go` are concrete and explicit. `QUEUE_BACKEND` env var selects at startup.

### `services/api/` changes

- Remove `internal/worker/` package (moved to worker service)
- Remove `internal/queue/` package (moved to worker service)
- Add `internal/http/events.go` — `/internal/events` endpoint (worker → SSE bridge)
- API publishes to SQS directly via AWS SDK when `QUEUE_BACKEND=sqs`
- When `QUEUE_BACKEND=inmemory`, API writes to local channel; worker goroutine runs in-process (non-compose mode only)

---

## Phase 6A — Infrastructure Base (implementation tasks)

1. Add LocalStack to docker-compose with `core` and `no-ai` profiles
2. Write `scripts/bootstrap.sh`: create SQS queue + DLQ + S3 bucket via awslocal CLI
3. Create `services/worker/` — SQS polling loop, trace extraction, mock AI path, metrics, OTEL
4. Modify API — publish to SQS when `QUEUE_BACKEND=sqs`, add `/internal/events` endpoint
5. Validate trace continuity end-to-end (HTTP → SQS → worker → SSE → Tempo)
6. Validate `no-ai` profile: full flow with mock output, no ai-runtime dependency
7. Validate worker crash → visibility timeout → redelivery → successful processing

## Phase 6B — Operational Validation (validation tasks, not implementation)

1. Observe `queue_depth_current` under normal load — confirm metric reflects real state
2. Simulate AI runtime timeout — verify DLQ routing after 3 attempts; confirm `queue_dlq_depth` increments
3. Observe duplicate delivery scenario — document behavior; confirm idempotency gap
4. Measure memory: `core` profile, `metrics` profile, `full` profile
5. Measure memory with LocalStack + Ollama simultaneously — document instability threshold
6. Calibrate `VisibilityTimeout` from observed AI runtime p95 latency
7. Document admission control thresholds from observed saturation — baseline for Phase 6C or Phase 8

## Phase 7 — Terraform IaC

1. Write Terraform module: SQS main queue + DLQ with validated attributes
2. Write Terraform module: S3 bucket
3. Run `terraform plan` — validate output matches bootstrap script resources exactly
4. Document `make infra-apply` workflow; Terraform does not run in compose startup
5. Document local state: `.tfstate` committed to repo; no remote backend (local-first)

---

## Known Gaps (explicit)

- **Idempotency:** Duplicate delivery possible. Worker does not deduplicate. Future direction documented in section 10.
- **Admission control:** Temporarily absent in Phase 6A (observation-first mode). Reintroduced in Phase 6B after calibration.
- **SQS metric precision:** `ApproximateNumberOfMessages` is not real-time. All lag-based decisions must account for this.
- **DLQ realtime events:** DLQ depth is a metric only in Phase 6A. No SSE event per DLQ entry. Added in a later phase if operationally needed.
- **Worker scale-out:** Single worker, `MaxMessages=1`. Multi-worker requires deduplication first.
- **SNS:** Out of scope. No operational need demonstrated.

---

## Definition of Done — Phase 6A

- [ ] `docker compose --profile core --profile no-ai up` starts cleanly; all containers healthy
- [ ] Task flows: HTTP → SQS → worker → mock AI output → S3 → SSE → frontend
- [ ] Same `trace_id` visible in Tempo from HTTP ingress through worker span
- [ ] Worker crash (container kill) → task redelivered after visibility timeout → completes
- [ ] All new metrics visible in Prometheus (`queue_depth_current`, `queue_inflight_current`, `queue_dlq_depth`)

## Definition of Done — Phase 6B

- [ ] `queue_depth_current` reflects real lag trend under load (not necessarily exact)
- [ ] `queue_dlq_depth` increments after 3 failed deliveries
- [ ] Memory measured and within budget across all profiles
- [ ] `VisibilityTimeout` value updated with observed rationale
- [ ] Admission control thresholds documented from real observation

## Definition of Done — Phase 7

- [ ] `terraform plan` output matches bootstrap script resources
- [ ] `make infra-apply` documented and working
- [ ] `terraform destroy` + `make infra-apply` reproduces clean environment
- [ ] Bootstrap script retained as fast-path; Terraform as reproducibility validation
