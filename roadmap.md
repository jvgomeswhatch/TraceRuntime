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

# Phase 0 — Architecture

Define:

- monorepo structure
- protobuf contracts
- event schema
- trace propagation flow

Output:

- stable service boundaries
- event definitions
- architecture diagram

---

# Phase 1 — API + Frontend

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

# Phase 2 — Local Async Processing

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

# Phase 3 — AI Runtime

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

# Phase 4 — Observability

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

# Phase 5 — Docker Compose

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

# Phase 6 — LocalStack

Replace local queue with:

- SQS
- SNS
- S3

Goal:

- durable queues
- retries
- DLQ
- event propagation
- artifact persistence

Validate:

- queue lag
- visibility timeout
- DLQ behavior

---

# Phase 7 — Terraform ✅

Provision with Terraform:

- SQS queues (tasks + DLQ)
- S3 bucket (traceruntime-outputs)
- SNS (deferred — no multi-consumer use case yet)

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

Goal:

- reproducible infrastructure
- minimal IaC
- zero local tool installation beyond Docker

---

# Phase 7.5 — Task Persistence (PostgreSQL + S3) ✅

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

Goal:

- tasks survive restarts — state is durable, not in-memory
- full traceability: task → trace → artifact → S3 key
- operational queries become possible: "which tasks failed?", "which tasks generated artifacts?"

Note: This phase resolves the known gap from Phase 6 where task state lives exclusively in memory/SQS and S3 has no corresponding PostgreSQL record.

---

# Phase 7B — CI/CD (GitHub Actions)

Prerequisite: Phase 7.5 complete — integration tests require task → queue → worker → PostgreSQL → S3 flow to exist.

Two parallel jobs on every PR. No deployment automation — CD is out of scope (no staging, no registry, no remote environment).

## Job 1 — Fast Quality Gate

Goal: detect development errors in under 3 minutes. Blocks merge.

- `go build ./...`
- `go test ./...`
- `golangci-lint run`
- `buf lint` + `buf generate --template buf.gen.yaml`
- `terraform fmt -check` + `terraform validate`
- `docker compose config`
- `npm ci` + `npm run lint` + `npm run typecheck` + `npm run build`
- `ruff check .` + `pytest`

No containers. No LocalStack. No Ollama. Fast and reliable.

## Job 2 — Integration Validation

Goal: verify the architecture works end-to-end with mock AI. Runs in parallel with Job 1.

Services started in CI:
- LocalStack
- PostgreSQL
- API
- Worker
- AI Runtime (mock — returns `{"result": "mock-response"}`)

Steps:
- `make infra-apply`
- `make infra-smoke`
- End-to-end task flow: create task → enqueue → worker consume → persist state → artifact write → task complete

Validates: SQS, DLQ, PostgreSQL, S3, trace propagation.

Ollama is never run in CI — it adds RAM, instability, and validates nothing about the infrastructure. Real model validation belongs to Phase 8.5 and Phase 9.

## Job 3 — Chaos Validation (future, post Phase 8)

Kill worker, simulate queue lag, simulate runtime failure. Verify heartbeat, recovery, DLQ, alerts. Added after Auto-Healing exists.

## Goal

- Every PR is automatically validated
- Architectural regressions are detected before merge
- No deployment automation

---

# Phase 8 — Auto-Healing

Implement:

- heartbeat tracking
- stale worker detection
- queue lag monitoring
- p95 latency monitoring

Actions:

- restart workers
- requeue tasks
- throttling

Goal:

- operational recovery visible in UI

---

# Phase 8.5 — Operational Tuning & Capacity

Prerequisite: Ollama running, Qwen/DeepSeek loaded, full pipeline operational.

This phase resolves the three items left open from Phase 6B validation, which could not be characterized with the mock path (sub-millisecond processing, no real backlog):

Measure:

- p50/p95/p99 real inference latency (`traceruntime_worker_task_duration_seconds`)
- queue backlog behavior under sustained load
- memory consumption of models under real inference
- `ApproximateNumberOfMessagesNotVisible` behavior during actual processing

Calibrate:

- `VisibilityTimeout` — current value 150s is provisional; must be > p95 inference time + 30s margin
- `QUEUE_MAX_DEPTH` admission control threshold — define from observed saturation point, not speculation
- Worker concurrency ceiling — characterize degradation before adding workers

Document:

- baseline numbers for all metrics as reference for Phase 9 chaos experiments
- identified bottlenecks and resource ceiling

Goal:

- all operational parameters are derived from real measurement, not defaults
- numbers from this phase serve as the SLO baseline for Phase 9

Note: measurements obtained with mock AI (Phase 6B) are archived as reference but are not valid operational baselines.

---

# Phase 9 — Chaos Testing

Test:

- worker crash
- AI runtime failure
- queue congestion
- latency spikes

Goal:

- validate resilience
- validate observability
- validate recovery flows

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
