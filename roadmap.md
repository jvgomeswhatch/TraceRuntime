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

## 9B — Chaos Testing

Test:

- worker crash
- AI runtime failure
- queue congestion
- latency spikes

Goal:

- validate resilience
- validate observability
- validate recovery flows

## 9C — Security & Infrastructure Hardening

Security items deferred from earlier phases — require API Gateway/reverse proxy:

- [ ] Add authentication to `/api/operations/summary` (needs BFF/proxy — client-side token is insecure)
- [ ] Server-side Origin validation on SSE `/events` endpoint (define allowed origins list)

Testing & CI expansion:

- [ ] Create `tests/integration/` with dedicated `go.mod` for end-to-end tests
- [ ] End-to-end integration test: API → SQS → Worker → PostgreSQL → S3 flow
- [ ] Add CI Job 2 service containers for API + Worker (build and run in CI)
- [ ] Resilience tests: queue lag, visibility timeout expiry, DLQ routing
- [ ] CI Job 3 — Chaos Validation: kill worker, simulate queue lag, verify recovery + DLQ + alerts
- [ ] Auto-healing validation pipelines in CI
- [ ] Self-repair automation tests
- [ ] Recovery validation: verify SLO compliance after controlled failure injection
- [ ] Automated chaos scenarios in CI

---

# Phase 10 — Multi-Provider LLM (Plug-and-Play)

Prerequisite: Phase 9 complete — local pipeline fully validated and resilient.

Transform the AI Runtime into a plug-and-play layer that accepts any LLM provider:

Providers:

- Ollama local (already implemented, default)
- Groq (free tier: 30 req/min, Llama/Mixtral)
- Google Gemini (free tier: 15 req/min, Gemini Flash)
- OpenRouter (aggregator, multiple free models)
- OpenAI (paid, GPT-4o)
- Anthropic (paid, Claude)

Configuration via environment variables:

```
AI_PROVIDER=ollama|groq|gemini|openrouter|openai|anthropic
AI_MODEL=qwen2:7b|llama-3-8b|gemini-flash|gpt-4o|claude-sonnet
AI_API_KEY=...              # only for cloud providers
AI_BASE_URL=...             # optional override
```

Implementation:

- Provider adapter interface in AI Runtime (Python)
- One adapter file per provider (~50 lines each)
- Same response contract: output, model, inference_duration_ms
- trace_id propagation works identically across all providers

What does NOT change:

- SQS, PostgreSQL, S3, traces, metrics, dashboard — everything stays the same
- The only thing that changes is where the inference response comes from

Validation:

- Same task, different providers → compare latency, output, cost in Grafana
- Dashboard shows model name and provider per task
- Traces show inference duration per provider

Goal:

- anyone who clones the project can plug their own LLM (local or cloud) without changing infrastructure
- observable comparison between providers using the same operational pipeline
- zero cost to test with Groq/Gemini free tiers

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
