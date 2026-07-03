# TraceRuntime

Local-first distributed AI runtime platform with full observability, distributed tracing, auto-healing, chaos testing, and realtime operational control.

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
![Python](https://img.shields.io/badge/Python-3.11-3776AB?logo=python&logoColor=white)
![Next.js](https://img.shields.io/badge/Next.js-16-000000?logo=next.js&logoColor=white)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?logo=postgresql&logoColor=white)
![Terraform](https://img.shields.io/badge/Terraform-1.12-844FBA?logo=terraform&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker&logoColor=white)
![OpenTelemetry](https://img.shields.io/badge/OpenTelemetry-OTEL-F5A800?logo=opentelemetry&logoColor=white)
![AWS](https://img.shields.io/badge/AWS-LocalStack-FF9900?logo=amazonaws&logoColor=white)

---

## Stack

| Layer          | Technology                                                            |
| -------------- | --------------------------------------------------------------------- |
| Frontend       | Next.js 16 · TypeScript · Tailwind · shadcn/ui · SSE                  |
| Backend        | Go 1.26 · net/http · chi                                              |
| AI Runtime     | Python 3.11 · FastAPI · LangGraph · Ollama                            |
| Queue          | SQS + DLQ via LocalStack                                              |
| Storage        | S3 via LocalStack (artifact persistence)                              |
| Database       | PostgreSQL 16 · pgx (no ORM) · 9 migrations                           |
| Infrastructure | Docker Compose · LocalStack · Terraform                               |
| Observability  | OpenTelemetry · Prometheus · Grafana · Tempo · Loki · OTEL Collector  |
| CI/CD          | GitHub Actions · 5 workflows · SHA-pinned · CodeQL · Gitleaks · Trivy |

## Architecture

```
HTTP Request
  → Go API            trace_id generated, span opened
  → PostgreSQL        task persisted (pending)
  → SQS               menssage enqueued via LocalStack
  → Go Worker         trace propagated via W3C Trace Context
  → AI Runtime        FastAPI → LangGraph → Ollama
  → S3                output artifact persisted ({trace_id}/{task_id}.json)
  → PostgreSQL       task status updated (completed), artifact_key recorded
  → SSE stream        frontend updated in realtime
  → Prometheus        metrics exposed
  → OTEL Collector    spans → Tempo, logs → Loki
```

The same `trace_id` propagates end-to-end — from HTTP request through SQS to the frontend. Task state is durable in PostgreSQL; artifacts are stored in S3. No in-memory-only state.

## Features

### Operational Dashboard

Realtime SSE-powered UI with task management, trace visualization, KPI cards (live metrics from PostgreSQL with configurable time window), system timeline, capacity reports, and token throughput monitoring.

### Trace Details

Waterfall timeline showing latency per stage (API → SQS → Worker → AI → S3). Spans from Tempo with graceful degradation when Tempo is unavailable. Click-to-select spans with detail panel.

### Alert Center

Filterable alert list with severity levels, status filtering (active/acknowledged/resolved), event type filtering, acknowledgment workflow, stats cards, and 10s auto-refresh.

### DLQ Explorer

Inspect dead-letter queue messages, view full payloads, retry individual messages back to the main queue, delete messages, purge entire DLQ, with stats and audit trail via SSE.

### Replay

Re-execute failed or completed tasks from existing payload. Preserves trace lineage — replayed tasks carry reference to the original `trace_id`. Modal-based UI with immediate feedback.

### Chaos Testing

Controlled failure injection framework with 6 scenarios:

| Scenario         | What it tests                                     |
| ---------------- | ------------------------------------------------- |
| worker-crash     | Worker kill → stale detection (3s) → recovery     |
| runtime-hang     | AI hang → stuck task detection (300s threshold)   |
| ai-failure       | AI errors → SQS retry → completion after recovery |
| postgres-failure | DB down → graceful degradation → auto-recovery    |
| queue-flood      | Message flood → admission control → backpressure  |
| slow-inference   | High latency → timeout handling → detection       |

Dedicated dashboard with report list, detail view, trigger/cancel/delete controls, and integration tests.

### Auto-Healing

Watchdog service with heartbeat tracking, stale worker detection, queue lag monitoring, p95 latency monitoring, and healing event visibility via SSE. Exponential backoff on SSE reconnect. All healing actions visible in the frontend timeline.

### Security Hardening

- Configurable CORS via `ALLOWED_ORIGINS`
- Server-side Origin validation on SSE endpoints
- `INTERNAL_TOKEN` auth on operational endpoints
- Rate limiting on all endpoints (30 RPS / burst 60)
- `Content-Security-Policy` header on all responses
- Non-root Docker containers

## Requirements

- Docker Desktop (with Compose v2)
- Go 1.26+
- Node.js 22+
- Python 3.11+
- Terraform 1.12+

No GPU required. No cloud APIs. Everything runs locally.

## Quick Start

```bash
cp .env.example .env

# First-time setup
make bootstrap

# Open the dashboard
open http://localhost:3001
```

`make bootstrap` provisions infrastructure (LocalStack SQS/S3, PostgreSQL, Terraform, migrations) and starts all services.

## Development

```bash
make up          # Start all services
make up-full     # Start with AI Runtime (Ollama)
make build       # Build container images only
make rebuild     # Build + start all services
make down        # Stop everything
make restart     # down + up
make clean       # Wipe all data (DB, SQS, S3, results) — keeps containers running
make reset       # Destroy volumes + bootstrap from scratch
make health      # Check health of all services
make ps          # Show running containers
make logs        # Tail logs from all services
make queue-stats # Show SQS queue depth and inflight messages
```

## Testing

```bash
make test                # Unit tests (Go + Python)
make test-go             # Go tests only (api, worker, watchdog)
make test-python         # Python tests only (ai-runtime)
make test-integration    # E2E against real services (no mocks)
make test-all            # Unit + integration
```

### Linting

```bash
make lint                # All linters
make lint-go             # golangci-lint
make lint-frontend       # ESLint + TypeScript
make lint-python         # ruff
make lint-terraform      # terraform fmt + validate
make fmt                 # Format Go and Terraform files
```

### Load Testing

```bash
make loadtest                        # 50 tasks, rate 2/s
make loadtest TASKS=100 RATE=5       # Custom parameters
make loadtest CONCURRENCY=8          # More concurrent workers
```

Results are saved to `results/` and displayed in the "Last Benchmark" dashboard card.

### Chaos Testing

Chaos testing requires a dedicated environment with `CHAOS_ENABLED=true` and a secure token. Use `make chaos-up` to start services in chaos mode before running scenarios.

```bash
make chaos-up                        # Start all services in chaos mode (generates token, enables chaos on AI Runtime)
make chaos                           # Run all chaos scenarios
make chaos SCENARIO=worker-crash     # Specific scenario
make chaos LIST=true                 # List available scenarios
make chaos-down                      # Stop chaos environment
make chaos-reset                     # Destroy volumes + bootstrap from scratch
```

> **Do not run `make chaos` against services started with `make up`.** The AI Runtime must be started with `CHAOS_ENABLED=true` and a valid `CHAOS_INTERNAL_TOKEN` (32+ chars) for chaos endpoints to be available. This token is separate from `INTERNAL_TOKEN` (used by the API for operational endpoints) so the dashboard remains functional during chaos testing.

## Services

| Service    | Port | Health                    |
| ---------- | ---- | ------------------------- |
| API        | 8082 | `GET /health`             |
| Worker     | 9091 | `GET /health`             |
| Watchdog   | 9093 | `GET /health`             |
| Frontend   | 3001 | Dashboard UI              |
| AI Runtime | 8001 | `GET /health`             |
| LocalStack | 4566 | `GET /_localstack/health` |
| PostgreSQL | 5432 | `pg_isready`              |

### Observability

| Service        | Port      | URL                   |
| -------------- | --------- | --------------------- |
| Grafana        | 3000      | http://localhost:3000 |
| Prometheus     | 9090      | http://localhost:9090 |
| Tempo          | 3200      | http://localhost:3200 |
| Loki           | 3100      | http://localhost:3100 |
| OTEL Collector | 4317/4318 | gRPC / HTTP           |

Grafana ships with 4 pre-built dashboards: Runtime Overview, Queue & Worker, AI Pipeline, and LLM Capacity.

### Docker Compose Profiles

| Profile | Services                                             |
| ------- | ---------------------------------------------------- |
| `core`  | localstack, postgres, migrate, api, worker, frontend |
| `no-ai` | Same as core (development without AI runtime)        |
| `full`  | core + ai-runtime                                    |
| `infra` | localstack + terraform (provisioning only)           |

## AI Runtime

The AI Runtime handles inference via a simple HTTP contract (`POST /infer`). The Worker communicates with it over HTTP — whatever happens inside (Ollama, OpenAI, chain of models) is invisible to the rest of the system.

### Current provider: Ollama (local)

| Task Type | Model                 | Trigger                              |
| --------- | --------------------- | ------------------------------------ |
| Coding    | `deepseek-coder:6.7b` | Input contains code-related keywords |
| General   | `qwen2.5:3b`          | Everything else                      |

Configurable via: `GENERAL_MODEL`, `CODING_MODEL`, `OLLAMA_HOST`.

## CI/CD Pipeline

5 GitHub Actions workflows, all actions SHA-pinned for supply chain security:

| Workflow        | Trigger   | Purpose                                                                                    |
| --------------- | --------- | ------------------------------------------------------------------------------------------ |
| CI — Validation | push + PR | Build, test, lint (Go/Frontend/Python), Terraform validate, Hadolint                       |
| Security        | push + PR | CodeQL SAST, Gitleaks secret scan, govulncheck, pip-audit, npm audit, Dependency Review    |
| Integration     | push + PR | Terraform apply against LocalStack, database migrations, smoke test, schema validation     |
| E2E — PR        | PR only   | Build Docker images, start all containers, health checks, integration tests — blocks merge |
| E2E — Full      | push only | Same as PR + Trivy container scan (CRITICAL/HIGH), SBOM generation, artifact upload        |

## Infrastructure

```bash
make infra-init          # Initialize Terraform
make infra-plan          # Preview changes
make infra-apply         # Apply (SQS queues, S3 bucket, DLQ)
make infra-destroy       # Tear down
make infra-smoke         # Verify queues + bucket exist
```

Terraform provisions against LocalStack:

- SQS queue (`traceruntime-tasks`) with visibility timeout and redrive policy
- SQS DLQ (`traceruntime-tasks-dlq`) for failed messages
- S3 bucket (`traceruntime-outputs`) for artifact persistence

## Project Structure

```
TraceRuntime/
├── services/
│   ├── api/                 # Go — HTTP API, SSE, task management, rate limiting
│   ├── worker/              # Go — SQS consumer, task processing, S3 artifacts
│   └── watchdog/            # Go — auto-healing, heartbeat monitoring, fault detection
├── frontend/                # Next.js — operational dashboard (SSE realtime)
├── ai-runtime/              # Python — FastAPI, LangGraph, Ollama inference
├── infra/
│   ├── terraform/           # SQS, S3, DLQ provisioning (LocalStack)
│   ├── database/migrations/ # PostgreSQL schema (9 migrations)
│   └── observability/       # Prometheus, Grafana, Tempo, Loki, OTEL Collector
├── tests/integration/       # E2E tests against real services (no mocks)
├── cmd/
│   ├── loadtest/            # Load testing tool with reporting
│   └── chaos/               # Chaos testing framework (6 scenarios)
├── scripts/                 # Bootstrap and smoke test
├── docker-compose.yml       # Main orchestration
├── Makefile                 # All commands
└── .github/workflows/       # 5 CI/CD pipelines
```

## License

MIT License — see [LICENSE](LICENSE) for details.
