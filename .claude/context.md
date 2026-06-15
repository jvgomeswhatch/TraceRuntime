# PROJECT CONTEXT

## Objective

Build a LOCAL-FIRST distributed event-driven AI runtime platform focused on:

- observability
- traceability
- operational visibility
- resilience
- debugging
- resource efficiency

This is NOT a chatbot project.

The frontend is an operational dashboard for inspecting distributed runtime behavior.

---

# CORE STACK

## Frontend

- Next.js
- TypeScript
- Tailwind
- shadcn/ui
- SSE for realtime updates

## Backend

- Go

## AI Runtime

- Python
- FastAPI
- LangGraph
- Ollama

## Infrastructure

- Docker Compose
- LocalStack
- PostgreSQL 16 (alpine, pgx/v5, no ORM)
- Terraform (containerized — `hashicorp/terraform:1.12`, profile `infra`)

## Observability

- OpenTelemetry
- OpenTelemetry Collector
- Prometheus
- Grafana
- Tempo
- Loki

## Contracts

- protobuf

---

# AI MODELS

## General Tasks

Qwen 6B

## Coding Tasks

DeepSeek Coder 6B

Models run locally through Ollama.

No cloud APIs allowed.

---

# RESOURCE CONSTRAINTS

The entire platform must run locally on:

- 14GB RAM
- no GPU
- Docker Compose only

All engineering decisions must prioritize:

- low memory usage
- low CPU overhead
- predictable resource usage
- lightweight containers
- operational simplicity

Avoid:

- unnecessary containers
- duplicated runtimes
- excessive concurrency
- eager model loading
- unnecessary caching

Prefer:

- streaming
- lazy loading
- bounded queues
- lightweight Go services
- controlled concurrency

---

# BOOTSTRAP & ENVIRONMENT

## First-time setup

```
make bootstrap
```

This is idempotent and safe to run multiple times.

Flow:

```
localstack + postgres (up -d)
   ↓
terraform (profile infra, one-shot: init + apply → SQS + S3)
   ↓
migrate (one-shot: schema up)
   ↓
api + worker + frontend (up -d)
   ↓
validation (SQS queues + S3 bucket exist)
```

## Daily development

```
make up       — starts all services (no Terraform)
make down     — stops all services
make restart  — down + up
make reset    — destroys volumes + bootstrap from scratch
```

## Profiles

| Profile | Services |
|---|---|
| `core` | localstack, postgres, migrate, api, worker, frontend |
| `no-ai` | same as core (used for development without AI runtime) |
| `full` | core + ai-runtime |
| `infra` | localstack + terraform (provisioning only) |

## Task persistence (Phase 7.5)

- PostgreSQL is the system of record for task state and artifact metadata
- API inserts task as `pending` on creation
- Worker transitions: `pending → processing → completed/failed`
- S3 key recorded in `tasks.artifact_key` after artifact write
- Artifact key format: `{trace_id}/{task_id}.json`
- Migrations in `infra/database/migrations/` via `migrate/migrate:v4.18.1`
- pgx/v5 connection pool in API and Worker (DATABASE_URL env var)

---

# LOCALSTACK RESPONSIBILITIES

Use:

## SQS

Primary durable queue for:

- task orchestration
- retries
- visibility timeout
- DLQ handling
- worker distribution

## SNS

Deferred — provisioned only when multiple real consumers exist.
Current architecture uses SQS only.

## S3

Persistent storage for:

- AI outputs
- task snapshots
- trace exports
- replay payloads

Terraform may provision ONLY:

- SQS queues
- DLQs
- S3 buckets
- SNS topics (deferred — only when real multi-consumer use case exists)

Keep Terraform minimal and explicit.

---

# OBSERVABILITY REQUIREMENTS

Every service MUST include:

- structured logs
- OpenTelemetry instrumentation
- metrics endpoint
- health endpoint
- trace propagation

All telemetry must flow through the OpenTelemetry Collector.

Every async flow must preserve W3C trace context.

The same trace_id must propagate through:

HTTP Request
→ Go API
→ SQS
→ Worker
→ Python Runtime
→ LangGraph
→ Ollama
→ S3
→ Frontend Stream

---

# FRONTEND VISIBILITY

Every important backend action must be visible in the frontend.

This includes:

- task creation
- queue state
- retries
- DLQ events
- worker heartbeats
- traces
- logs
- metrics
- inference execution
- failures
- recovery actions
- queue lag
- latency
- throughput

The frontend must resemble:

- Grafana
- Datadog
- operational runtime dashboards

NOT a chatbot UI.

---

# AUTO-HEALING

The system must detect:

- dead workers
- stale heartbeats
- excessive queue lag
- high p95 latency

The system may:

- restart workers
- requeue tasks
- throttle processing
- mark workers unhealthy

All recovery actions must be observable from the frontend.

---

# SIMPLICITY RULES

Avoid:

- repository pattern
- excessive interfaces
- clean architecture boilerplate
- speculative abstractions
- unnecessary services
- premature optimization

Prefer:

- direct implementations
- explicit flows
- small services
- readable code
- operational clarity

---

# EXECUTION RULES

DO NOT generate the entire system at once.

Implement incrementally.

Per step:

- maximum 1 service
- maximum 1 infrastructure concern
- maximum 1 major dependency

Every implementation must include:

1. Objective
2. Architectural reasoning
3. Files created
4. Minimal implementation
5. How to run
6. Manual validation
7. Expected behavior
8. Failure testing
9. Observability validation

After EVERY implementation:

- STOP
- wait for validation
- never continue automatically

Do not generate unrelated features.

---

# DEFINITION OF DONE

A step is ONLY complete when:

- the service runs locally
- logs are visible
- metrics are exposed
- traces are visible
- health checks pass
- frontend reflects state correctly
- failure scenarios are validated
- containers are healthy

Implementation alone is NOT sufficient.

Operational validation is mandatory.
