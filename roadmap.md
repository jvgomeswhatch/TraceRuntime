# ROADMAP

## Goal

Build a LOCAL-FIRST distributed event-driven AI runtime platform with:

- observability
- distributed tracing
- realtime operational visibility
- auto-healing
- low resource usage

Constraints:

- 14GB RAM
- no GPU
- Docker Compose only

---

# Phase 1 — Architecture ✅

Define:

- monorepo structure
- JSON event contracts
- event schema
- trace propagation flow

Output:

- stable service boundaries
- event definitions
- architecture diagram

Note: Protobuf was originally defined in this phase but removed after evaluation. All services communicate via JSON over HTTP/SSE/SQS. With 2 Go services, 1 Python runtime, and 1 frontend — all maintained by the same developer — protobuf added complexity (code generation, oneof flattening, protojson configuration) without tangible benefit. JSON contracts are enforced by shared struct definitions in Go and TypeScript interfaces in the frontend. If the project scales to multiple teams or adds gRPC inter-service communication, protobuf can be reintroduced with a real use case.

---

# Phase 2 — API + Frontend ✅

Build:

- Go API
- Next.js dashboard
- SSE realtime updates
- basic OpenTelemetry

Validate:

```txt
Frontend
→ API
→ Frontend updates
```

Goal:

- task creation visible in UI
- trace_id propagation working

NO Docker yet.

---

# Phase 3 — Local Async Processing ✅

Build:

- lightweight local queue
- Go worker
- retry mechanism

Validate:

```txt
API
→ Queue
→ Worker
→ Frontend
```

Goal:

- async processing working
- retries visible in UI

Still NO LocalStack.

---

# Phase 4 — AI Runtime

Build:

- FastAPI runtime
- LangGraph orchestration
- Ollama integration
- ONE model only initially

Validate:

```txt
Task
→ Worker
→ AI Runtime
→ Frontend
```

Goal:

- inference working locally
- RAM usage validated
- trace propagation preserved

---

# Phase 5 — Observability

Add:

- OTEL Collector
- Prometheus
- Grafana
- Tempo
- Loki

Goal:

- traces visible
- metrics visible
- logs visible
- latency measurable

---

# Phase 6 — Docker Compose ✅

Containerize:

- frontend
- API
- workers
- AI runtime
- observability stack

Add:

- memory limits
- CPU limits
- health checks

Goal:

- reproducible local environment

---

# Phase 7 — Infrastructure (LocalStack + Terraform + PostgreSQL + CI/CD) ✅

This phase consolidates all infrastructure concerns: durable queues, persistent storage, IaC, and continuous integration.

## 7A — LocalStack (SQS + S3)

Replace local queue with:

- SQS
- SNS (deferred — no multi-consumer use case yet)
- S3

Goal:

- durable queues
- retries
- DLQ
- event propagation
- artifact persistence

Validated: queue lag, visibility timeout, DLQ behavior.

## 7B — Terraform

Provision with Terraform:

- SQS queues (tasks + DLQ)
- S3 bucket (traceruntime-outputs)

Terraform containerized:

- `hashicorp/terraform:1.12` runs inside Docker Compose (profile `infra`)
- No local Terraform installation required
- Provider cache in named volume `terraform_cache` (no Windows/Linux conflict)
- State persisted via bind mount (`infra/terraform/terraform.tfstate`)
- `terraform.tfstate` gitignored — local-only

Bootstrap flow:

```
make bootstrap
├── docker compose up -d localstack postgres
├── docker compose --profile infra up terraform   (init + apply)
├── docker compose --profile no-ai up -d          (migrate + api + worker + frontend)
└── validation (SQS queues + S3 bucket exist)
```

Daily development: `make up` (no Terraform, no reprovisioning).

## 7C — Task Persistence (PostgreSQL + S3)

Integrate PostgreSQL into the worker runtime:

- API inserts task into PostgreSQL on creation (`pending`)
- Worker writes task state transitions to PostgreSQL (`pending → processing → completed/failed`)
- S3 key recorded in PostgreSQL after artifact write — no artifact exists without a database record
- PostgreSQL becomes the system of record for task state and artifact metadata
- S3 remains the artifact store for large or binary outputs (PDFs, images, audio)

Implementation:

- PostgreSQL 16 (alpine, 256m) with health check (`pg_isready`)
- Schema: `tasks` table with status FSM, `artifact_key`, timestamps, 3 indexes, check constraint
- Migrations via `migrate/migrate:v4.18.1` container (one-shot, `depends_on: postgres healthy`)
- pgx/v5 connection pool in both API and Worker (no ORM)
- Artifact key format: `{trace_id}/{task_id}.json`
- API and Worker depend on `migrate: service_completed_successfully`

Validated:

- Task creation → PostgreSQL insert (`pending`)
- Worker processing → status `processing` with `processing_started_at`
- Task completion → status `completed` with `artifact_key` and `completed_at`
- S3 artifact content matches database record
- SSE events visible in frontend (task.created → task.processing → task.completed)
- Bootstrap idempotent (Terraform: `0 added, 0 changed, 0 destroyed` on rerun)

## 7D — CI/CD (GitHub Actions)

Two parallel jobs on every PR. No deployment automation — CD is out of scope (no staging, no registry, no remote environment).

### Job 1 — Quality Gate

Goal: detect development errors in under 3 minutes. Blocks merge.

Implemented:

- Go build + test (api and worker separately, `QUEUE_BACKEND=inmemory`)
- golangci-lint v2.2 (`.golangci.yml` with expanded linter set)
- Terraform fmt check + validate
- Docker Compose config validation
- Frontend: npm ci + lint + typecheck + build
- Python: ruff check + pytest
- Cache optimization: Go modules (3 services), pip, npm

No containers. No LocalStack. No Ollama. Fast and reliable.

### Job 2 — Integration

Goal: verify infrastructure provisioning works. Runs in parallel with Job 1.

Services started in CI (GitHub Actions service containers):

- LocalStack 3.4
- PostgreSQL 16

Steps:

- Terraform init + apply (SQS queues + S3 bucket)
- Database migration (migrate container)
- Infrastructure smoke test (`scripts/smoke-test.sh` — 11-point validation)
- Database schema validation

Validates: SQS, DLQ, S3, PostgreSQL schema, Terraform provisioning.

Ollama is never run in CI — it adds RAM, instability, and validates nothing about the infrastructure.

Goal:

- reproducible infrastructure
- minimal IaC
- zero local tool installation beyond Docker
- tasks survive restarts — state is durable, not in-memory
- full traceability: task → trace → artifact → S3 key
- every PR automatically validated
- architectural regressions detected before merge

---

# Phase 8 — Auto-Healing ✅

Implement:

- heartbeat tracking
- stale worker detection
- queue lag monitoring
- p95 latency monitoring
- exponential backoff on SSE reconnect

Actions:

- restart workers
- requeue tasks
- throttling

Goal:

- operational recovery visible in UI

---

# Phase 9 — Operational Tuning, Chaos & Hardening

Prerequisite: Ollama running, Qwen/DeepSeek loaded, full pipeline operational.

## 9A — Operational Tuning & Capacity ✅

This sub-phase resolves the items left open from Phase 7A validation, which could not be characterized with the mock path (sub-millisecond processing, no real backlog).

Measured:

- p50/p95/p99 real inference latency (qwen2.5:3b on Ryzen 5 3500U)
- queue backlog behavior under sustained load (peak 9-10 messages)
- token throughput: avg 2.96 tok/s, p95 3.30 tok/s (hardware ceiling ~3.3)
- `ApproximateNumberOfMessagesNotVisible` behavior during actual processing

Calibrated:

- `VisibilityTimeout` — 240s → 360s (p95 processing = 286s + margin)
- `QUEUE_MAX_DEPTH` admission control threshold — 50 adequate (peak observed: 10)
- Worker concurrency ceiling — 1 worker stable, concurrency > 1 deferred to 9B

Delivered:

- Token metrics instrumentation (ai-runtime → worker → DB → loadtest)
- Grafana LLM Capacity dashboard (7 panels)
- PostgreSQL migration 000005 (token columns)
- Loadtest tool with collector, anomaly tracking, JSON reports
- Clock drift fix: `NOW()` from PostgreSQL in all state transitions
- `docs/baselines/BASELINE_OPERACIONAL_v1.md` with real Ollama numbers

Incidents resolved:

- Docker/WSL2 clock drift causing negative processing durations — root cause identified and fixed

## 9B — Chaos Testing ✅

Chaos testing framework with 6 scenarios, all validated from clean state.

Delivered:

- Chaos runner CLI (`cmd/chaos/`) with preflight checks, SLO validation, JSON reports
- Docker controller (kill, stop, start, pause, unpause, health, restart policy)
- Checker (PostgreSQL healing events, heartbeats, SQS queue depth, HTTP health)
- 6 scenarios: worker-crash, runtime-hang, ai-failure, postgres-failure, queue-flood, slow-inference

Results (from clean `make chaos-reset`):

| Scenario | Status | Duration | Key Detection |
|---|---|---|---|
| worker-crash | PASS | 61s | `worker.stale` detected in 3s |
| runtime-hang | PASS | 340s | `task.stuck` detected at 300s threshold |
| ai-failure | PASS | 540s | Tasks retry via SQS, complete after recovery |
| postgres-failure | PASS | 35s | Graceful degradation, auto-recovery |
| queue-flood | PASS | — | Admission control, backpressure |
| slow-inference | PASS | — | Latency detection, timeout handling |

Architectural fixes discovered and resolved during chaos testing:

- Worker HTTP timeout hierarchy: watchdog threshold (300s) + poll interval (15s) < worker HTTP timeout (570s) — watchdog detects before worker acts
- Watchdog dedup map synchronization: `reconcileActiveState()` per poll cycle reconciles in-memory map with DB truth
- Immediate heartbeat on worker startup (before ticker) — eliminates detection gap on recovery
- Docker restart policy control (`DisableRestart`/`EnableRestart`) for crash simulation
- Deterministic test setup: resolve events, verify zero active, fresh heartbeat before injection
- AI runtime failure: worker reverts task to `pending` + `ChangeMessageVisibility(30s)` for SQS retry (not permanent fail on first attempt)
- Clock drift fix in chaos checker: `HeartbeatAgeSec()` computes age server-side in PostgreSQL to avoid host/container clock skew

## 9C — Security & Infrastructure Hardening

### 9C.1 — Security Hardening ✅

- [x] Configurable CORS via `ALLOWED_ORIGINS` env var
- [x] Server-side Origin validation on SSE `/events` endpoint
- [x] `INTERNAL_TOKEN` auth on operational endpoints (`/api/operations/summary`, `/api/events/recent`, `/api/capacity/latest`)
- [x] Rate limiting extended to GET endpoints (30 RPS / burst 60)
- [x] `Content-Security-Policy` header on all API responses
- [x] Watchdog Dockerfile non-root user (`USER app`)

### 9C.2 — Integration Testing ✅

- [x] `tests/integration/` with dedicated `go.mod`
- [x] 22 tests across 6 files: task lifecycle, failure path, queue behavior, SSE events, idempotency, healing events
- [x] Mock AI Runtime (`tests/integration/cmd/mock-ai-runtime/`)
- [x] `docker-compose.test.yml` override for test environment
- [x] `make test-integration` target

### 9C.3 — CI Hardening ✅

- [x] CI triggers on `main` (deploy) and `developer` (testing) branches
- [x] Fix stale smoke test assertion: `visibility_timeout = 150` → `360`
- [x] Watchdog added to Quality Gate (build + test + lint)
- [x] Job 3 — E2E Validation: Docker Buildx builds (API/Worker/Watchdog/mock-ai-runtime), LocalStack + PostgreSQL service containers, health polling with failure diagnostics, integration test execution, artifact uploads (logs + test reports)
- [x] E2E dependency DAG: `needs: [quality-gate, integration]`
- [x] Docker Buildx cache with per-service scopes (`type=gha`)
- [x] `.gitignore` hardened: `.env.*` pattern covers all env file variants

### 9C.4 — Operational Metrics & Dashboard Separation ✅

Separated runtime metrics (live operational data) from benchmark data (loadtest reports). Previously, the dashboard KPI cards (Throughput, P95, Error Rate, Queue Depth) pulled data exclusively from loadtest JSON files — meaning they showed stale benchmark data or nothing at all during normal operation.

Delivered:

- `GET /api/metrics/runtime` endpoint — calculates real-time metrics from PostgreSQL `tasks` table within a configurable time window (default 5 min)
- Metrics layer (`db/metrics.go`) with single CTE query: throughput, P95 latency, avg latency, success rate, error rate — only from finished tasks (`completed` + `failed`), never `pending`/`processing`
- Queue depth from SQS (`GetQueueAttributes`) or in-memory queue, not derived from tasks table
- Endpoint protected by `X-Internal-Token` (same security model as other operational endpoints)
- KPI Cards now show live data with 10s polling interval, labels show window ("last 5m"), avg latency, completed/failed counts
- Capacity Report card renamed to "Last Benchmark" — clearly identifies data as benchmark results from `make loadtest`
- `RuntimeMetrics` TypeScript interface added to frontend types

Architecture decision:

- **Runtime Metrics** = live operational telemetry from database (tasks processed in last N minutes)
- **Last Benchmark** = static snapshot from last `make loadtest` run (JSON file in `results/`)
- These are never mixed — runtime data never overwrites benchmark, benchmark never pollutes operational view
- `results/` directory contains only loadtest/chaos JSON reports, served via `GET /api/capacity/latest`

### 9C.5 — Token Metrics Pipeline ✅

Connected token counting from AI Runtime through to the frontend dashboard. Token data (prompt_tokens, completion_tokens, tokens_per_second, model) was already collected by the AI Runtime and stored in PostgreSQL, but never surfaced in the SSE event stream or frontend.

Delivered:

- Worker SSE events now include `prompt_tokens`, `completion_tokens`, `tokens_per_second` fields on `task.completed`
- API `/api/events/recent` endpoint returns token fields from PostgreSQL
- Frontend Event Feed shows inline token count and tok/s per task event
- Frontend Tasks table has "Tokens" and "tok/s" columns
- `SSEEvent` TypeScript interface extended with token fields

### 9C.6 — Task Lifecycle Integrity ✅

Fixed orphaned pending tasks caused by SQS publish failures.

Previously, the API inserted a task into PostgreSQL as `pending` before publishing to SQS. If SQS was unavailable (e.g., LocalStack restarted), the publish failed but the task remained `pending` forever — no worker would ever process it.

Delivered:

- `db.SetFailed()` method marks task as `failed` with error reason when SQS publish fails
- API calls `SetFailed` in both error paths: SQS unavailable (`"sqs: queue unavailable"`) and queue full (`"queue full"`)
- Tasks no longer get stuck as orphaned `pending`

### 9C.7 — Build/Run Separation ✅

Separated `make build` (compile) from `make up` (start) in Makefile.

Previously, `make up` always ran `--build`, causing unnecessary rebuilds on every start. Now:

- `make up` / `make up-full` — start services using existing images (fast, daily use)
- `make build` — `docker compose --profile full build` (compile only, no start)
- `make rebuild` — `docker compose --profile full up -d --build` (build + start)

---

# Phase 10 — Operational Investigation

Prerequisite: Phase 9 complete — local pipeline fully validated and resilient.

Transform the dashboard from a metrics viewer into an investigation tool. Every feature must be testable in CI (integration or E2E).

Specs:
- [Baseline & Riscos](docs/specs/phase-10-baseline-and-risks.md)
- [Features](docs/specs/phase-10-features.md)

| Feature | Status | Description |
|---------|--------|-------------|
| Trace Details | ✅ | Waterfall timeline showing latency per stage (API → SQS → Worker → AI → S3) |
| Request Inspector | | Request/response detail: trace context, payload, artifact, duration |
| Runtime Topology | | Live architecture map with real metrics: status, requests, latency, throughput per component |
| Worker Details | | Worker page: heartbeat, uptime, current task, tasks processed, avg latency |

### Feature 1 — Trace Details ✅

Delivered:

- `GET /api/traces/{traceID}` — fetches task from PostgreSQL + spans from Tempo, with graceful degradation (Tempo down → `spans: null`, `tempo_available: false`)
- Tempo client (`services/api/internal/tempo/`) with 5s timeout, `Accept: application/json`, hex traceID validation
- `GetTaskByTraceID` DB query using existing `idx_tasks_trace_id` index
- Frontend `/traces/[traceId]` page with span waterfall (grid-aligned), KPI cards, task details with mini-cards
- Span click-to-select with emerald border indicator and detail panel
- Duration fallback: spans → DB timestamps (`processing_started_at` → `completed_at`) when Tempo data expired
- Real-time polling (5s) for in-progress tasks, auto-stops on terminal status
- Duration persisted in Tasks and Traces pages via `processing_started_at`/`completed_at` from `/api/events/recent`
- Error boundary (`error.tsx`) for route-level errors
- Google Fonts replaced with `next/font/local` (eliminates network dependency in Docker build)
- Tasks Processed KPI changed from 5-min sliding window to all-time total (`total_completed` + `total_failed`)

Technical debt:

- **LLM output not visible after page reload** — output stored in S3 (`artifact_key`) but no endpoint to fetch it. SSE carries output in-memory during session only. Needs: API endpoint to retrieve S3 artifact by task ID + UI to display response text.

---

# Phase 11 — Operational Control

Prerequisite: Phase 10 complete — investigation capabilities validated.

Add operational actions: alerting, recovery, replay, chaos. Every feature must be testable in CI.

| Feature | Description |
|---------|-------------|
| Alert Center | Alert list with status/history/filters, replaces healing event counter |
| Replay Task | Re-execute tasks from original payload or S3 artifact |
| DLQ Explorer | Inspect, retry, or delete dead letter queue messages |
| Chaos Dashboard | Frontend for chaos scenarios: kill worker, pause runtime, flood queue |

---

# Definition of Done (Project Complete)

The project is complete when Phases 10 + 11 are done and it demonstrates:

- **Investigation** — trace details, topology, worker inspection, request inspector
- **Operations** — alert center, DLQ explorer, replay, chaos
- **All visible in the frontend** — no CLI required
- **All testable in CI** — integration/E2E tests for every feature

At this point the project stops being a metrics dashboard and becomes a complete operational platform for a distributed runtime.

---

# Final Result

The platform should resemble:

- distributed runtime infrastructure
- internal platform tooling
- operational control systems

NOT:

- a chatbot clone
- a CRUD SaaS
- an AI wrapper app
