# Phase 9C.3 — CI Hardening Design

## Objective

Expand the CI pipeline to catch regressions that the current Quality Gate and Infrastructure jobs cannot detect: end-to-end service integration, watchdog behavior, and smoke-level chaos validation. The pipeline must remain fast (< 15 minutes total) and reliable (no flaky Ollama or GPU dependencies).

## Constraints

- No Ollama in CI — adds RAM, instability, and validates nothing about infrastructure
- GitHub Actions runners: 7 GB RAM, 2 vCPUs (ubuntu-latest)
- Total pipeline target: < 15 minutes (all jobs combined)
- No deployment automation (no staging, no registry, no remote environment)
- AI Runtime mock at HTTP level (same mock from 9C.2 integration tests)

---

## Current CI State

### Existing Jobs

| Job | Purpose | Duration | Status |
|---|---|---|---|
| Quality Gate | Build, test, lint (Go, Python, Frontend, Terraform, Docker) | ~3 min | ✅ Working |
| Skill Security Scan | SkillSpector SARIF scan for Claude skills/agents | ~1 min | ✅ Working |
| Integration | Terraform apply + smoke test + DB schema validation | ~3 min | ✅ Working |

### Gaps

- API and Worker are never built or run as containers in CI
- No end-to-end test: task creation → processing → completion
- No watchdog validation in CI
- Smoke test has stale assertion: `visibility_timeout = 150` (should be `360` after Phase 9A calibration)
- No integration test execution in CI
- No chaos-level validation (worker crash recovery)

---

## Proposed CI Architecture

### Job Structure (4 jobs total)

```
┌─────────────────┐  ┌─────────────────────┐  ┌──────────────────┐
│  Quality Gate   │  │ Skill Security Scan │  │   Integration    │
│  (unchanged)    │  │   (unchanged)       │  │  (expanded)      │
│  ~3 min         │  │   ~1 min            │  │  ~8 min          │
└─────────────────┘  └─────────────────────┘  └──────────────────┘
         │                                              │
         └──────────────────┬───────────────────────────┘
                            ▼
                   ┌─────────────────────┐
                   │  E2E Validation     │
                   │  (new, needs QG)    │
                   │  ~10 min            │
                   └─────────────────────┘
```

- Quality Gate, Skill Security Scan, and Integration run in parallel (unchanged)
- E2E Validation runs after Quality Gate passes (needs compiled Go code to be valid)

---

## Job Changes

### Job 1: Quality Gate (unchanged)

No changes. Already validates build, test, lint for all languages.

### Job 2: Skill Security Scan (unchanged)

No changes. SkillSpector SARIF scan.

### Job 3: Integration (expanded)

**Current scope + fixes:**

1. Fix stale smoke test assertion: `visibility_timeout = 150` → `360`
2. Add watchdog build + basic validation

**Steps (additions in bold):**

1. Terraform apply (existing)
2. Database migration (existing)
3. **Smoke test — fix VT assertion to 360** (existing step, corrected)
4. Database schema validation (existing)
5. **Validate watchdog builds:** `cd services/watchdog && go build ./...`
6. **Watchdog test:** `cd services/watchdog && go test -race -count=1 ./...`

### Job 4: E2E Validation (new)

**Purpose:** Build API + Worker as containers, run them in CI with LocalStack + PostgreSQL, execute integration tests from Phase 9C.2.

**Service containers:**
- LocalStack 3.4 (SQS + S3)
- PostgreSQL 16-alpine

**Steps:**

1. Checkout code
2. Setup Go (for running integration tests)
3. Setup Terraform, apply infrastructure (SQS queues, S3 bucket)
4. Run database migrations
5. Build API container: `docker build -t traceruntime-api ./services/api`
6. Build Worker container: `docker build -t traceruntime-worker ./services/worker`
7. Build Watchdog container: `docker build -t traceruntime-watchdog ./services/watchdog`
8. Start Mock AI Runtime (simple Go HTTP server, built from test helpers)
9. Start API container with environment pointing to LocalStack + PostgreSQL + Mock AI Runtime
10. Start Worker container with same environment
11. Start Watchdog container
12. Wait for all containers healthy (health check polling)
13. Run integration tests: `cd tests/integration && go test -v -count=1 -timeout=5m ./...`
14. Collect test results

**Environment for containers:**

```yaml
API_URL: http://localhost:8082
DATABASE_URL: postgres://traceruntime:traceruntime@localhost:5432/traceruntime?sslmode=disable
SQS_QUEUE_URL: http://localhost:4566/000000000000/traceruntime-tasks
SQS_DLQ_URL: http://localhost:4566/000000000000/traceruntime-tasks-dlq
S3_ENDPOINT: http://localhost:4566
AI_RUNTIME_URL: http://localhost:8000
INTERNAL_TOKEN: ci-test-token-32chars-minimum-ok
QUEUE_BACKEND: sqs
```

**Container networking:**
- Explicit Docker network `traceruntime-ci` created in CI (not `--network host`)
- Containers communicate by name: `api`, `worker`, `watchdog`, `mock-ai-runtime`
- Service containers (LocalStack, PostgreSQL) use GitHub Actions port mapping, accessed via `localhost` from the host and via `host.docker.internal` or the CI network gateway from containers
- No Docker Compose in CI — individual `docker run` commands for control and debugging
- This matches the real Docker Compose networking model more closely than `--network host`

---

## Smoke Test Fix

**File:** `scripts/smoke-test.sh`

**Change:** Line 59, assertion value

```bash
# Current (stale from Phase 7A):
if [ "$VTIMEOUT" = "150" ]; then

# Fixed (Phase 9A calibration):
if [ "$VTIMEOUT" = "360" ]; then
```

This is a bug — the Terraform config sets `360`, but the smoke test still asserts `150`.

---

## Mock AI Runtime for CI

A minimal HTTP server that mimics the `/infer` and `/health` endpoints. Built as a small Go binary in `tests/integration/cmd/mock-ai-runtime/`.

```
tests/integration/
└── cmd/
    └── mock-ai-runtime/
        └── main.go     ← Standalone HTTP server: /health, /infer, /ready
```

Endpoints:
- `GET /health` → `{"status": "ok"}`
- `GET /ready` → `{"status": "ok", "ollama": "mocked"}`
- `POST /infer` → Returns canned response or 503 based on input

Same logic as the `httptest.Server` mock from 9C.2, but as a standalone binary that can be run in CI.

---

## CI Workflow Changes

### New job in `.github/workflows/ci.yml`

```yaml
e2e:
  name: E2E Validation
  runs-on: ubuntu-latest
  timeout-minutes: 15
  needs: [quality-gate]
  strategy:
    fail-fast: false

  services:
    localstack:
      image: localstack/localstack:3.4
      ports:
        - 4566:4566
      env:
        SERVICES: sqs,s3
        DEFAULT_REGION: us-east-1
      options: >-
        --health-cmd "curl -f http://localhost:4566/_localstack/health"
        --health-interval 10s
        --health-timeout 5s
        --health-retries 10
        --health-start-period 20s

    postgres:
      image: postgres:16-alpine
      ports:
        - 5432:5432
      env:
        POSTGRES_DB: traceruntime
        POSTGRES_USER: traceruntime
        POSTGRES_PASSWORD: traceruntime
      options: >-
        --health-cmd "pg_isready -U traceruntime -d traceruntime"
        --health-interval 10s
        --health-timeout 5s
        --health-retries 5

  steps:
    - uses: actions/checkout@v4

    - uses: actions/setup-go@v5
      with:
        go-version-file: services/api/go.mod

    - uses: hashicorp/setup-terraform@v3
      with:
        terraform_version: "1.12.0"

    - name: Provision infrastructure
      run: |
        terraform -chdir=infra/terraform init -input=false
        terraform -chdir=infra/terraform apply -auto-approve
      env:
        TF_VAR_localstack_endpoint: http://localhost:4566

    - name: Run migrations
      run: |
        docker run --rm --network host \
          -v ${{ github.workspace }}/infra/database/migrations:/migrations:ro \
          migrate/migrate:v4.18.1 \
          -path=/migrations \
          -database="postgres://traceruntime:traceruntime@localhost:5432/traceruntime?sslmode=disable" \
          up

    - name: Build services
      run: |
        docker build -t traceruntime-api ./services/api
        docker build -t traceruntime-worker ./services/worker
        docker build -t traceruntime-watchdog ./services/watchdog

    - name: Create CI network
      run: docker network create traceruntime-ci

    - name: Build and start mock AI runtime
      run: |
        cd tests/integration/cmd/mock-ai-runtime && go build -o /tmp/mock-ai-runtime .
        docker run -d --name mock-ai-runtime --network traceruntime-ci \
          -v /tmp/mock-ai-runtime:/app/mock-ai-runtime:ro \
          --entrypoint /app/mock-ai-runtime \
          golang:1.22-alpine

    - name: Start services
      env:
        CI_DB_URL: "postgres://traceruntime:traceruntime@host.docker.internal:5432/traceruntime?sslmode=disable"
        CI_SQS_ENDPOINT: "http://host.docker.internal:4566"
        CI_SQS_QUEUE: "http://host.docker.internal:4566/000000000000/traceruntime-tasks"
        CI_SQS_DLQ: "http://host.docker.internal:4566/000000000000/traceruntime-tasks-dlq"
        CI_TOKEN: "ci-e2e-token-minimum-32-characters-long"
      run: |
        docker run -d --name api --network traceruntime-ci -p 8082:8082 \
          -e DATABASE_URL="${CI_DB_URL}" \
          -e SQS_QUEUE_URL="${CI_SQS_QUEUE}" \
          -e SQS_ENDPOINT="${CI_SQS_ENDPOINT}" \
          -e AI_RUNTIME_URL="http://mock-ai-runtime:8000" \
          -e INTERNAL_TOKEN="${CI_TOKEN}" \
          -e QUEUE_BACKEND=sqs \
          -e AWS_ACCESS_KEY_ID=test \
          -e AWS_SECRET_ACCESS_KEY=test \
          -e AWS_DEFAULT_REGION=us-east-1 \
          --add-host=host.docker.internal:host-gateway \
          traceruntime-api

        docker run -d --name worker --network traceruntime-ci -p 9091:9091 \
          -e DATABASE_URL="${CI_DB_URL}" \
          -e SQS_QUEUE_URL="${CI_SQS_QUEUE}" \
          -e SQS_DLQ_URL="${CI_SQS_DLQ}" \
          -e SQS_ENDPOINT="${CI_SQS_ENDPOINT}" \
          -e S3_ENDPOINT="${CI_SQS_ENDPOINT}" \
          -e AI_RUNTIME_URL="http://mock-ai-runtime:8000" \
          -e API_URL="http://api:8082" \
          -e INTERNAL_TOKEN="${CI_TOKEN}" \
          -e AWS_ACCESS_KEY_ID=test \
          -e AWS_SECRET_ACCESS_KEY=test \
          -e AWS_DEFAULT_REGION=us-east-1 \
          --add-host=host.docker.internal:host-gateway \
          traceruntime-worker

        docker run -d --name watchdog --network traceruntime-ci -p 9093:9093 \
          -e DATABASE_URL="${CI_DB_URL}" \
          -e SQS_QUEUE_URL="${CI_SQS_QUEUE}" \
          -e SQS_DLQ_URL="${CI_SQS_DLQ}" \
          -e SQS_ENDPOINT="${CI_SQS_ENDPOINT}" \
          -e API_URL="http://api:8082" \
          -e INTERNAL_TOKEN="${CI_TOKEN}" \
          -e WORKER_HEALTH_URL="http://worker:9091/health" \
          -e AWS_ACCESS_KEY_ID=test \
          -e AWS_SECRET_ACCESS_KEY=test \
          -e AWS_DEFAULT_REGION=us-east-1 \
          --add-host=host.docker.internal:host-gateway \
          traceruntime-watchdog

    - name: Wait for services healthy
      run: |
        for i in $(seq 1 30); do
          if curl -sf http://localhost:8082/health > /dev/null 2>&1; then
            echo "API healthy"
            break
          fi
          sleep 2
        done
        curl -sf http://localhost:8082/health || (echo "API not healthy" && docker logs api && exit 1)

    - name: Run integration tests
      run: cd tests/integration && go test -v -count=1 -timeout=5m ./...
      env:
        API_URL: http://localhost:8082
        DATABASE_URL: postgres://traceruntime:traceruntime@localhost:5432/traceruntime?sslmode=disable
        SQS_ENDPOINT: http://localhost:4566
        SQS_QUEUE_URL: http://localhost:4566/000000000000/traceruntime-tasks
        SQS_DLQ_URL: http://localhost:4566/000000000000/traceruntime-tasks-dlq
        S3_ENDPOINT: http://localhost:4566

    - name: Collect service logs
      if: always()
      run: |
        mkdir -p /tmp/ci-logs
        docker logs api > /tmp/ci-logs/api.log 2>&1 || true
        docker logs worker > /tmp/ci-logs/worker.log 2>&1 || true
        docker logs watchdog > /tmp/ci-logs/watchdog.log 2>&1 || true
        docker logs mock-ai-runtime > /tmp/ci-logs/mock-ai-runtime.log 2>&1 || true

    - name: Upload service logs
      if: always()
      uses: actions/upload-artifact@v4
      with:
        name: e2e-service-logs
        path: /tmp/ci-logs/
        retention-days: 7
```

---

## Makefile Changes

```makefile
# Add to existing targets
test-all: test test-integration

test-integration:
	@docker compose ps --format '{{.Service}}' | head -1 > /dev/null 2>&1 || \
		(echo "ERROR: services not running. Run 'make up' first." && exit 1)
	cd tests/integration && go test -v -count=1 -timeout=5m ./...
```

---

## What This Phase Does NOT Include

- Chaos validation in CI (deferred — requires Docker-in-Docker or privileged containers)
- Performance/load testing in CI (loadtest is operational, not CI-appropriate)
- Deployment automation (no staging, no registry)
- Code coverage thresholds (can be added later with minimal effort)
- Frontend E2E tests (Playwright/Cypress — separate concern)

---

## Risks and Mitigations

| Risk | Mitigation |
|---|---|
| Container build time in CI | Docker layer caching via `docker/build-push-action` if needed |
| Service startup timing | Health check polling with 60s timeout + log collection on failure |
| Flaky tests from timing | Conservative timeouts in integration tests, polling with WaitFor |
| GitHub Actions RAM limit (7 GB) | No Ollama, mock AI Runtime, lightweight containers |
| Network issues between containers | Explicit `traceruntime-ci` network, `--add-host` for service container access |
| Debugging CI failures | Service logs uploaded as artifacts (retained 7 days) |

---

## Timeline

| Step | Duration |
|---|---|
| Quality Gate (parallel) | ~3 min |
| Integration (parallel) | ~3 min |
| E2E Validation (sequential) | ~10 min |
| **Total pipeline** | **~13 min** |
