# TraceRuntime

Local-first distributed AI runtime platform with full observability, distributed tracing, auto-healing, and realtime operational visibility.

## Stack

| Layer | Technology |
|---|---|
| Frontend | Next.js 14, TypeScript, Tailwind, shadcn/ui, SSE |
| Backend | Go 1.26, net/http, chi |
| AI Runtime | Python 3.11, FastAPI, LangGraph, Ollama |
| Queue | SQS (LocalStack) |
| Database | PostgreSQL 16 (pgx, no ORM) |
| Infrastructure | Docker Compose, LocalStack, Terraform |
| Observability | OpenTelemetry, Prometheus, Grafana, Tempo, Loki |

## Architecture

```
HTTP Request
  -> Go API           (trace_id generated, span opened)
  -> SQS              (message enqueued via LocalStack)
  -> Go Worker         (trace propagated via W3C carrier)
  -> AI Runtime        (FastAPI -> LangGraph -> Ollama)
  -> S3                (output persisted)
  -> SSE stream        (frontend updated in realtime)
  -> Prometheus        (metrics exposed)
  -> OTEL Collector    (spans -> Tempo, logs -> Loki)
```

## Requirements

- Docker Desktop (with Docker Compose v2)
- 14 GB RAM minimum
- Go 1.26+
- Node.js 22+
- Python 3.11+
- Terraform 1.12+ (or use containerized version via `make bootstrap`)
- AWS CLI (for queue inspection commands)

No GPU required. No cloud APIs. Everything runs locally.

## Quick Start

```bash
# First-time setup (provisions infrastructure + starts services)
make bootstrap

# Open the dashboard
open http://localhost:3001
```

`make bootstrap` is idempotent and safe to run multiple times. It:

1. Creates the Docker network
2. Starts LocalStack + PostgreSQL
3. Runs Terraform to provision SQS queues and S3 bucket
4. Runs database migrations
5. Starts API, Worker, Watchdog, and Frontend

## Daily Development

```bash
make up          # Start all services (no rebuild, no Terraform)
make up-full     # Same as up, but includes AI Runtime (Ollama)
make build       # Build all container images (no start)
make rebuild     # Build + start all services
make down        # Stop all services + observability
make restart     # down + up
make reset       # Destroy volumes + bootstrap from scratch
```

**Important:** `make up` does NOT rebuild containers. If you changed Go, Python, or frontend code, run `make rebuild`.

## Monitoring & Operations

```bash
make health       # Check health of all services
make ps           # Show running containers
make logs         # Tail logs from all services
make queue-stats  # Show SQS queue depth and inflight messages
```

## Testing

### Unit Tests

```bash
make test          # Run all unit tests (Go + Python)
make test-go       # Go tests only (api, worker, watchdog)
make test-python   # Python tests only (ai-runtime)
```

### Linting

```bash
make lint             # Run all linters
make lint-go          # golangci-lint on all Go services
make lint-frontend    # ESLint + TypeScript type checking
make lint-python      # ruff on ai-runtime
make lint-terraform   # terraform fmt + validate
```

### Formatting

```bash
make fmt           # Format Go and Terraform files
```

### Integration Tests (E2E)

Runs against real services (PostgreSQL, LocalStack, SQS, S3). No mocks.

```bash
# Start services in test mode
make test-integration-up

# Run integration tests
make test-integration

# Stop test environment
make test-integration-down

# Run everything (unit + integration)
make test-all
```

### Load Testing

```bash
make loadtest                           # 50 tasks, rate 2/s, concurrency 4
make loadtest TASKS=100 RATE=5          # Custom parameters
make loadtest CONCURRENCY=8            # More concurrent workers
```

Results are saved to `results/` and displayed in the "Last Benchmark" dashboard card.

### Chaos Testing

Controlled failure injection to validate resilience and auto-healing.

```bash
make chaos-up                          # Start services in chaos mode
make chaos                             # Run all chaos scenarios
make chaos SCENARIO=worker-crash       # Run specific scenario
make chaos LIST=true                   # List available scenarios
make chaos-down                        # Stop chaos environment
make chaos-reset                       # Full reset + re-bootstrap
```

## Infrastructure

### Terraform

```bash
make infra-init       # Initialize Terraform
make infra-plan       # Preview changes
make infra-apply      # Apply (creates SQS queues, S3 bucket)
make infra-destroy    # Tear down all resources
make infra-fmt        # Format Terraform files
make infra-validate   # Validate configuration
make infra-smoke      # Smoke test (verify queues + bucket exist)
```

## Services

| Service | Port | Health Check |
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

## Docker Compose Profiles

| Profile | Services |
|---|---|
| `core` | localstack, postgres, migrate, api, worker, frontend |
| `no-ai` | Same as core (development without AI runtime) |
| `full` | core + ai-runtime |
| `infra` | localstack + terraform (provisioning only) |

## AI Runtime

The AI Runtime is a Python service (FastAPI + LangGraph) that handles inference. The Worker communicates with it via a simple HTTP contract:

**Request:** `POST /infer`
```json
{
  "task_id": "uuid",
  "input": "user prompt text",
  "deadline_unix_ms": 1719500000000
}
```

**Response:**
```json
{
  "output": "model response",
  "execution_status": "completed",
  "inference_duration_ms": 1200,
  "prompt_tokens": 45,
  "completion_tokens": 120,
  "tokens_per_second": 25.5,
  "execution_profile": { "model": "qwen2.5:3b" }
}
```

### Current provider: Ollama (local)

By default, the AI Runtime uses Ollama to run models locally with zero cloud dependency:

| Task Type | Model | Trigger |
|---|---|---|
| Coding | `deepseek-coder:6.7b` | Input contains keywords like "code", "function", "debug" |
| General | `qwen2.5:3b` | Everything else |

Models are configurable via environment variables: `GENERAL_MODEL`, `CODING_MODEL`, `OLLAMA_HOST`.

### Can I use OpenAI, Gemini, or other cloud providers?

**Not out of the box.** The current implementation calls Ollama's `/api/generate` endpoint directly in `ollama_client.py`. To use a cloud provider you would need to:

1. Create a new client (e.g. `openai_client.py`) that implements the same `generate()` interface
2. Update `graph.py` to use the new client instead of `ollama_client`
3. The Worker does not need changes — it only knows the `/infer` HTTP contract

The Worker-to-AI-Runtime boundary is fully decoupled via HTTP. Whatever happens inside the AI Runtime (Ollama, OpenAI, Gemini, a chain of models) is invisible to the rest of the system, as long as `/infer` returns the expected JSON response.

## CI Pipeline

The GitHub Actions pipeline runs on every push/PR to `main` and `developer`:

| Job | What it validates |
|---|---|
| Quality Gate | Go build + tests + lint, frontend lint + typecheck + build, Python lint + tests, Terraform validation, Docker Compose config |
| Skill Security Scan | Static analysis of AI agent/skill definitions (SkillSpector) |
| Integration | Terraform provisioning, database migrations, infrastructure smoke test |
| E2E Validation | Full system test with all containers running against real infrastructure |

## Project Structure

```
TraceRuntime/
├── services/
│   ├── api/                 # Go — HTTP API, SSE, task management, metrics
│   ├── worker/              # Go — SQS consumer, task processing, S3 output
│   └── watchdog/            # Go — auto-healing, heartbeat monitoring, fault detection
├── frontend/                # Next.js — operational dashboard (SSE realtime)
├── ai-runtime/              # Python — FastAPI, LangGraph, Ollama inference
├── infra/
│   ├── terraform/           # SQS, S3, DLQ provisioning (LocalStack)
│   ├── database/
│   │   └── migrations/      # PostgreSQL schema migrations
│   └── observability/       # Prometheus, Grafana, Tempo, Loki, OTEL Collector
├── tests/
│   └── integration/         # E2E tests (Go, runs against real services)
│       └── cmd/
│           └── mock-ai-runtime/  # Mock inference server for CI
├── cmd/
│   ├── loadtest/            # Load testing tool
│   └── chaos/               # Chaos testing framework
├── results/                 # Load test and chaos test reports
├── scripts/                 # Bootstrap and smoke test scripts
├── docker-compose.yml       # Main orchestration
├── Makefile                 # All commands documented above
└── .github/
    └── workflows/
        └── ci.yml           # CI pipeline (Quality Gate, Integration, E2E)
```
