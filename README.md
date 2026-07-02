# TraceRuntime

Local-first distributed AI runtime with full observability, distributed tracing, auto-healing, chaos testing, and realtime operational control.

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
![Python](https://img.shields.io/badge/Python-3.11-3776AB?logo=python&logoColor=white)
![Next.js](https://img.shields.io/badge/Next.js-16-000000?logo=next.js&logoColor=white)
![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?logo=postgresql&logoColor=white)
![Terraform](https://img.shields.io/badge/Terraform-1.12-844FBA?logo=terraform&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-Compose-2496ED?logo=docker&logoColor=white)
![OpenTelemetry](https://img.shields.io/badge/OpenTelemetry-OTEL-F5A800?logo=opentelemetry&logoColor=white)

---

## Stack

| Layer | Technology |
|---|---|
| Frontend | Next.js 16 · TypeScript · Tailwind · shadcn/ui · SSE |
| Backend | Go 1.26 · net/http · chi |
| AI Runtime | Python 3.11 · FastAPI · LangGraph · Ollama |
| Queue | SQS via LocalStack |
| Database | PostgreSQL 16 · pgx (no ORM) |
| Infrastructure | Docker Compose · LocalStack · Terraform |
| Observability | OpenTelemetry · Prometheus · Grafana · Tempo · Loki |
| CI/CD | GitHub Actions (5 workflows, SHA-pinned) |

## Architecture

```
HTTP Request
  → Go API           (trace_id generated, span opened)
  → SQS              (message enqueued via LocalStack)
  → Go Worker         (trace propagated via W3C carrier)
  → AI Runtime        (FastAPI → LangGraph → Ollama)
  → S3                (output persisted)
  → SSE stream        (frontend updated in realtime)
  → Prometheus        (metrics exposed)
  → OTEL Collector    (spans → Tempo, logs → Loki)
```

The same `trace_id` propagates end-to-end — from HTTP request to frontend.

## Features

**Operational Dashboard** — realtime SSE-powered UI with task management, trace visualization, KPI cards, system timeline, and capacity reports.

**Alert Center** — filterable alert list with severity levels, acknowledgment workflow, and auto-refresh.

**DLQ Explorer** — inspect dead-letter queue messages, view payloads, retry or delete individual messages.

**Replay** — re-execute failed or completed tasks with full trace lineage preservation.

**Chaos Testing** — controlled failure injection (worker crash, AI failure, queue flood, latency spikes) with scenario runner, report generation, and dedicated dashboard.

**Auto-Healing** — watchdog service with heartbeat monitoring, stale worker detection, queue lag alerting, and healing event visibility via SSE.

**Distributed Tracing** — full W3C Trace Context propagation across HTTP → SQS → Worker → AI Runtime, visualized via Tempo and the trace detail page.

## Requirements

- Docker Desktop (with Compose v2)
- 14 GB RAM minimum
- Go 1.26+
- Node.js 22+
- Python 3.11+
- Terraform 1.12+

No GPU required. No cloud APIs. Everything runs locally.

## Quick Start

```bash
# First-time setup
make bootstrap

# Open the dashboard
open http://localhost:3001
```

`make bootstrap` provisions infrastructure (LocalStack, PostgreSQL, Terraform, migrations) and starts all services.

## Development

```bash
make up          # Start all services
make up-full     # Start with AI Runtime (Ollama)
make rebuild     # Build + start all services
make down        # Stop everything
make restart     # down + up
make reset       # Destroy volumes + bootstrap from scratch
```

## Testing

```bash
make test                # Unit tests (Go + Python)
make test-go             # Go tests only
make test-python         # Python tests only
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
```

### Load Testing

```bash
make loadtest                        # 50 tasks, rate 2/s
make loadtest TASKS=100 RATE=5       # Custom parameters
```

### Chaos Testing

```bash
make chaos                           # Run all chaos scenarios
make chaos SCENARIO=worker-crash     # Specific scenario
make chaos LIST=true                 # List available scenarios
```

## Services

| Service | Port | Health |
|---|---|---|
| API | 8082 | `GET /health` |
| Worker | 9091 | `GET /health` |
| Watchdog | 9093 | `GET /health` |
| Frontend | 3001 | Dashboard UI |
| AI Runtime | 8001 | `GET /health` |
| LocalStack | 4566 | `GET /_localstack/health` |
| PostgreSQL | 5432 | `pg_isready` |

### Observability

| Service | Port | URL |
|---|---|---|
| Grafana | 3000 | http://localhost:3000 |
| Prometheus | 9090 | http://localhost:9090 |
| Tempo | 3200 | http://localhost:3200 |
| Loki | 3100 | http://localhost:3100 |
| OTEL Collector | 4317/4318 | gRPC / HTTP |

Grafana ships with 4 pre-built dashboards: Runtime Overview, Queue & Worker, AI Pipeline, and LLM Capacity.

## AI Runtime

The AI Runtime handles inference via a simple HTTP contract. The Worker calls `POST /infer` and receives a structured response — whatever happens inside (Ollama, OpenAI, chain of models) is invisible to the rest of the system.

### Current provider: Ollama (local)

| Task Type | Model | Trigger |
|---|---|---|
| Coding | `deepseek-coder:6.7b` | Input contains code-related keywords |
| General | `qwen2.5:3b` | Everything else |

Configurable via: `GENERAL_MODEL`, `CODING_MODEL`, `OLLAMA_HOST`.

## CI/CD Pipeline

5 GitHub Actions workflows, all SHA-pinned:

| Workflow | Trigger | Purpose |
|---|---|---|
| CI — Validation | push + PR | Build, test, lint (Go/Frontend/Python), Terraform validate, Hadolint |
| Security | push + PR | CodeQL SAST, Gitleaks, govulncheck, pip-audit, npm audit |
| Integration | push + PR | Terraform apply, migrations, smoke test, schema validation |
| E2E — PR | PR only | Build containers, start services, run integration tests |
| E2E — Full | push only | Same as PR + Trivy container scan, SBOM generation, artifacts |

## Infrastructure

```bash
make infra-init          # Initialize Terraform
make infra-plan          # Preview changes
make infra-apply         # Apply (SQS queues, S3 bucket, DLQ)
make infra-destroy       # Tear down
make infra-smoke         # Verify queues + bucket exist
```

## Project Structure

```
TraceRuntime/
├── services/
│   ├── api/                 # Go — HTTP API, SSE, task management
│   ├── worker/              # Go — SQS consumer, task processing, S3 output
│   └── watchdog/            # Go — auto-healing, heartbeat monitoring
├── frontend/                # Next.js — operational dashboard (SSE realtime)
├── ai-runtime/              # Python — FastAPI, LangGraph, Ollama inference
├── infra/
│   ├── terraform/           # SQS, S3, DLQ provisioning (LocalStack)
│   ├── database/migrations/ # PostgreSQL schema (9 migrations)
│   └── observability/       # Prometheus, Grafana, Tempo, Loki, OTEL Collector
├── tests/integration/       # E2E tests against real services
├── cmd/
│   ├── loadtest/            # Load testing tool
│   └── chaos/               # Chaos testing framework
├── scripts/                 # Bootstrap and smoke test
├── docker-compose.yml       # Main orchestration
├── Makefile                 # All commands
└── .github/workflows/       # 5 CI/CD pipelines
```

## License

All rights reserved.
