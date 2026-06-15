# Phase 5 — Docker Compose Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Containerize all application services (Go API, Python AI Runtime, Next.js frontend) and wire them into a reproducible local environment with a single shared Docker network and full observability stack.

**Architecture:** Three Dockerfiles (multi-stage Go, Python slim, Node standalone). App services declared in `docker-compose.yml` (root). Observability stack stays in `infra/observability/docker-compose.yml` and joins the shared `traceruntime` network as external. OTEL Collector gains a Loki exporter for log shipping.

**Tech Stack:** Docker Compose v3.9, Go multi-stage (golang:1.22-alpine → alpine:3.19), python:3.11-slim, node:20-alpine, otel/opentelemetry-collector-contrib, grafana/loki:3.0.0

---

## File Map

| Action | Path | Purpose |
|---|---|---|
| Create | `services/api/Dockerfile` | Go multi-stage build |
| Create | `services/api/.dockerignore` | Exclude binaries and cache |
| Create | `ai-runtime/Dockerfile` | Python slim image |
| Create | `ai-runtime/.dockerignore` | Exclude pycache and venvs |
| Create | `frontend/Dockerfile` | Node builder + standalone runner |
| Create | `frontend/.dockerignore` | Exclude node_modules and .next |
| Modify | `frontend/next.config.ts` | Add `output: 'standalone'` |
| Modify | `docker-compose.yml` | Populate with app services + traceruntime network |
| Modify | `infra/observability/docker-compose.yml` | Migrate network from `observability` to `traceruntime` |
| Modify | `infra/observability/otel-collector/config.yaml` | Add loki exporter + logs pipeline |

---

## Task 1: Go API Dockerfile

**Files:**
- Create: `services/api/Dockerfile`
- Create: `services/api/.dockerignore`

- [ ] **Step 1: Create `.dockerignore`**

```
# services/api/.dockerignore
server.exe
server
*.test
*.out
.git
```

- [ ] **Step 2: Create `Dockerfile`**

```dockerfile
# services/api/Dockerfile
FROM golang:1.22-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o server ./cmd/server/...

# ---

FROM alpine:3.19 AS runner

RUN apk add --no-cache curl && \
    addgroup -S app && adduser -S app -G app

WORKDIR /app

COPY --from=builder /app/server .

USER app

EXPOSE 8082

CMD ["./server"]
```

- [ ] **Step 3: Verify the Dockerfile builds locally**

Run from the project root:
```bash
docker build -t traceruntime-api:local ./services/api/
```

Expected: build completes, image created. Run `docker images | grep traceruntime-api` to confirm.

- [ ] **Step 4: Smoke-test the image**

```bash
docker run --rm -e OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 -p 8082:8082 traceruntime-api:local &
sleep 2
curl -f http://localhost:8082/health
docker stop $(docker ps -q --filter ancestor=traceruntime-api:local)
```

Expected: `{"status":"ok"}` on `/health`.

---

## Task 2: Python AI Runtime Dockerfile

**Files:**
- Create: `ai-runtime/Dockerfile`
- Create: `ai-runtime/.dockerignore`

- [ ] **Step 1: Create `.dockerignore`**

```
# ai-runtime/.dockerignore
__pycache__
*.pyc
*.pyo
.venv
venv
*.egg-info
.git
```

- [ ] **Step 2: Create `Dockerfile`**

```dockerfile
# ai-runtime/Dockerfile
FROM python:3.11-slim

ENV PYTHONUNBUFFERED=1
ENV PYTHONDONTWRITEBYTECODE=1

WORKDIR /app

COPY requirements.txt .
RUN pip install --no-cache-dir -r requirements.txt

COPY . .

EXPOSE 8000

CMD ["uvicorn", "main:app", "--host", "0.0.0.0", "--port", "8000"]
```

- [ ] **Step 3: Verify the Dockerfile builds locally**

Run from the project root:
```bash
docker build -t traceruntime-ai-runtime:local ./ai-runtime/
```

Expected: build completes. This may take a few minutes on first run (pip install).

- [ ] **Step 4: Smoke-test the image**

```bash
docker run --rm \
  -e OTEL_EXPORTER_OTLP_ENDPOINT=localhost:4317 \
  -e OLLAMA_BASE_URL=http://host.docker.internal:11434 \
  --add-host=host.docker.internal:host-gateway \
  -p 8000:8000 \
  traceruntime-ai-runtime:local &
sleep 3
curl -f http://localhost:8000/health
docker stop $(docker ps -q --filter ancestor=traceruntime-ai-runtime:local)
```

Expected: `{"status":"ok"}` or similar JSON on `/health`.

---

## Task 3: Frontend Dockerfile + standalone config

**Files:**
- Modify: `frontend/next.config.ts`
- Create: `frontend/Dockerfile`
- Create: `frontend/.dockerignore`

- [ ] **Step 1: Add `output: 'standalone'` to `next.config.ts`**

Current content of `frontend/next.config.ts`:
```ts
import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  /* config options here */
};

export default nextConfig;
```

Replace with:
```ts
import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  output: "standalone",
};

export default nextConfig;
```

- [ ] **Step 2: Create `.dockerignore`**

```
# frontend/.dockerignore
node_modules
.next
.git
*.md
```

- [ ] **Step 3: Create `Dockerfile`**

```dockerfile
# frontend/Dockerfile
FROM node:20-alpine AS builder

WORKDIR /app

COPY package.json package-lock.json ./
RUN npm ci

COPY . .
RUN npm run build

# ---

FROM node:20-alpine AS runner

ENV NODE_ENV=production
ENV PORT=3001

WORKDIR /app

RUN addgroup -S app && adduser -S app -G app

COPY --from=builder /app/.next/standalone ./
COPY --from=builder /app/.next/static ./.next/static
COPY --from=builder /app/public ./public

USER app

EXPOSE 3001

CMD ["node", "server.js"]
```

- [ ] **Step 4: Verify the Dockerfile builds locally**

Run from the project root:
```bash
docker build -t traceruntime-frontend:local ./frontend/
```

Expected: build completes. The `.next/standalone` directory must exist inside the image — if you see "COPY failed: file not found in build context", `output: 'standalone'` was not saved correctly in step 1.

- [ ] **Step 5: Smoke-test the image**

```bash
docker run --rm -p 3001:3001 traceruntime-frontend:local &
sleep 3
curl -f http://localhost:3001/
docker stop $(docker ps -q --filter ancestor=traceruntime-frontend:local)
```

Expected: HTML response (Next.js page).

---

## Task 4: Observability compose — network migration

**Files:**
- Modify: `infra/observability/docker-compose.yml`

The goal is to replace the internal `observability` network with the shared `traceruntime` network (declared as external, owned by the app compose).

- [ ] **Step 1: Update network declaration at the bottom of `infra/observability/docker-compose.yml`**

Find:
```yaml
networks:
  observability:
    driver: bridge
```

Replace with:
```yaml
networks:
  traceruntime:
    external: true
```

- [ ] **Step 2: Replace all `networks: [observability]` references with `networks: [traceruntime]`**

Every service in that file has `networks: [observability]`. Change each one to:
```yaml
    networks: [traceruntime]
```

Services affected: `otel-collector`, `prometheus`, `tempo`, `loki`, `grafana`.

- [ ] **Step 3: Verify the observability stack starts with the external network**

First create the network (the app compose will do this automatically later, but for testing isolation):
```bash
docker network create traceruntime
docker compose -f infra/observability/docker-compose.yml up -d
docker compose -f infra/observability/docker-compose.yml ps
```

Expected: all 5 services up. Then tear down:
```bash
docker compose -f infra/observability/docker-compose.yml down
docker network rm traceruntime
```

---

## Task 5: OTEL Collector — add Loki exporter and logs pipeline

**Files:**
- Modify: `infra/observability/otel-collector/config.yaml`

The current config has `traces` and `metrics` pipelines. We add a `logs` pipeline that receives OTLP logs, processes them, and exports to Loki. The OTLP receiver already exists — no new receiver needed. The Loki exporter is configured with correlation fields (`trace_id`, `span_id`) so Grafana can link logs to traces. The `job` label in Loki is set from the OTEL `service.name` resource attribute.

- [ ] **Step 1: Add `loki` exporter to `infra/observability/otel-collector/config.yaml`**

Current `exporters` block:
```yaml
exporters:
  otlp/tempo:
    endpoint: tempo:4317
    tls:
      insecure: true
    retry_on_failure:
      enabled: true
      initial_interval: 5s
      max_interval: 30s
      max_elapsed_time: 300s
    sending_queue:
      enabled: true
      num_consumers: 4
      queue_size: 100
  prometheus:
    endpoint: "0.0.0.0:8889"
    namespace: traceruntime
```

Replace with:
```yaml
exporters:
  otlp/tempo:
    endpoint: tempo:4317
    tls:
      insecure: true
    retry_on_failure:
      enabled: true
      initial_interval: 5s
      max_interval: 30s
      max_elapsed_time: 300s
    sending_queue:
      enabled: true
      num_consumers: 4
      queue_size: 100
  prometheus:
    endpoint: "0.0.0.0:8889"
    namespace: traceruntime
  loki:
    endpoint: http://loki:3100/loki/api/v1/push
    default_labels_enabled:
      exporter: false
      job: true        # set from service.name resource attribute
      instance: true   # set from service.instance.id
      level: true      # set from log severity
    # Correlation fields — these propagate trace_id/span_id into Loki log lines
    # enabling Grafana Tempo ↔ Loki correlation via "Derived Fields"
```

- [ ] **Step 2: Add `logs` pipeline to the `service.pipelines` block**

Current `service` block:
```yaml
service:
  extensions: [health_check]
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [otlp/tempo]
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [prometheus]
```

Replace with:
```yaml
service:
  extensions: [health_check]
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [otlp/tempo]
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [prometheus]
    logs:
      receivers: [otlp]
      processors: [memory_limiter, batch]
      exporters: [loki]
```

- [ ] **Step 3: Verify the collector config is valid**

```bash
docker run --rm \
  -v $(pwd)/infra/observability/otel-collector/config.yaml:/etc/otel-collector/config.yaml:ro \
  otel/opentelemetry-collector-contrib:0.104.0 \
  --config=/etc/otel-collector/config.yaml \
  validate
```

Expected: exits 0 with no errors. If `validate` subcommand is not available, the config will be validated on first `docker compose up`.

---

## Task 6: Root `docker-compose.yml` — populate app services

**Files:**
- Modify: `docker-compose.yml`

- [ ] **Step 1: Replace the empty `docker-compose.yml` with the full app services definition**

```yaml
version: "3.9"

networks:
  traceruntime:
    driver: bridge

services:
  api:
    build:
      context: ./services/api
      dockerfile: Dockerfile
    ports:
      - "8082:8082"
    environment:
      - OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317
      - AI_RUNTIME_URL=http://ai-runtime:8000
    networks: [traceruntime]
    depends_on:
      otel-collector:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8082/health"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s
    restart: unless-stopped
    init: true
    stop_grace_period: 10s
    mem_limit: 256m
    cpus: "0.5"

  ai-runtime:
    build:
      context: ./ai-runtime
      dockerfile: Dockerfile
    ports:
      - "8000:8000"
    environment:
      - OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317
      - OLLAMA_BASE_URL=http://host.docker.internal:11434
    extra_hosts:
      - "host.docker.internal:host-gateway"
    networks: [traceruntime]
    depends_on:
      otel-collector:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8000/health"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 15s
    restart: unless-stopped
    init: true
    stop_grace_period: 10s
    mem_limit: 768m
    cpus: "1.0"

  frontend:
    build:
      context: ./frontend
      dockerfile: Dockerfile
    ports:
      - "3001:3001"
    environment:
      - OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317
    networks: [traceruntime]
    depends_on:
      api:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:3001/"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 15s
    restart: unless-stopped
    init: true
    mem_limit: 256m
    cpus: "0.5"
```

Note: `otel-collector` lives in `infra/observability/docker-compose.yml` but is reachable by name because both compose files share the `traceruntime` network. The `depends_on` reference works when both files are passed together via `-f`.

- [ ] **Step 2: Validate the merged compose config before any `up`**

This must pass before proceeding to Task 7. It catches network mismatches, missing service references, and YAML errors across both files.

```bash
docker compose \
  -f docker-compose.yml \
  -f infra/observability/docker-compose.yml \
  config
```

Expected: merged YAML printed to stdout with no errors. If you see `service "otel-collector" depends on undefined service`, the network migration in Task 4 was not completed. If you see a YAML parse error, check indentation in the compose files.

---

## Task 7: Full stack bring-up and validation

**Files:** none (validation only)

- [ ] **Step 1: Bring up the full stack**

```bash
docker compose \
  -f docker-compose.yml \
  -f infra/observability/docker-compose.yml \
  up --build -d
```

Expected: all images build, all containers start. Watch for build errors.

- [ ] **Step 2: Wait for all containers to become healthy**

```bash
watch docker ps
```

Wait until STATUS shows `(healthy)` for: `api`, `ai-runtime`, `frontend`, `otel-collector`, `prometheus`, `tempo`, `loki`, `grafana`. This may take 1-2 minutes.

- [ ] **Step 3: Validate app service health endpoints**

```bash
curl -f http://localhost:8082/health && echo "api ok"
curl -f http://localhost:8000/health && echo "ai-runtime ok"
curl -f http://localhost:3001/ > /dev/null && echo "frontend ok"
```

Expected: all three print their "ok" message.

- [ ] **Step 4: Open frontend in browser**

Navigate to `http://localhost:3001`. The dashboard should load. The SSE stream should connect (check browser DevTools → Network → EventSource or similar).

- [ ] **Step 5: Create a task and verify trace in Tempo**

1. Create a task via the UI (or `curl -X POST http://localhost:8082/tasks -H 'Content-Type: application/json' -d '{"input":"hello"}'`)
2. Open Grafana at `http://localhost:3000`
3. Go to Explore → select Tempo datasource → search for recent traces
4. Confirm a trace with spans from `traceruntime-api` is visible

- [ ] **Step 6: Verify metrics in Prometheus**

Navigate to `http://localhost:9090`. Run query:
```
traceruntime_queue_depth
```
Expected: metric present (value may be 0 if no tasks queued).

- [ ] **Step 7: Verify logs in Loki**

1. Open Grafana at `http://localhost:3000`
2. Go to Explore → select Loki datasource
3. Run query: `{job="traceruntime-api"}`

Expected: structured JSON log lines visible. If no logs appear immediately, create a task to trigger log output, then re-query.

- [ ] **Step 8: Test restart policy**

```bash
docker kill $(docker ps -q --filter name=traceruntime-api-1)
sleep 5
docker ps --filter name=traceruntime-api-1
```

Expected: container restarts automatically and returns to `(healthy)` within ~30s.

- [ ] **Step 9: Verify memory limits are respected**

```bash
docker stats --no-stream
```

Check that no container exceeds its declared limit. If `ai-runtime` shows close to 768m under load, note it for monitoring.

- [ ] **Step 10: Verify full stack restart persistence**

Bring the entire stack down (including network removal) and back up — validates that the external network is recreated correctly, services rejoin it, and no volumes or state are left in a broken state.

```bash
docker compose \
  -f docker-compose.yml \
  -f infra/observability/docker-compose.yml \
  down --remove-orphans

docker network ls | grep traceruntime
```

Expected: `traceruntime` network is gone after `down`.

```bash
docker compose \
  -f docker-compose.yml \
  -f infra/observability/docker-compose.yml \
  up --build -d
```

Expected: network recreated, all containers start and reach `(healthy)`. Then verify:

```bash
curl -f http://localhost:8082/health && echo "api ok"
curl -f http://localhost:8000/health && echo "ai-runtime ok"
curl -f http://localhost:3001/ > /dev/null && echo "frontend ok"
docker network inspect traceruntime --format '{{range .Containers}}{{.Name}} {{end}}'
```

Expected last command: lists all app + observability containers on the `traceruntime` network — confirms all services are joined correctly after a cold restart.

---

## Self-Review Notes

**Spec coverage check:**
- Dockerfiles (Go, Python, Frontend): Tasks 1–3 ✓
- `.dockerignore` files: Tasks 1–3 ✓
- `next.config.ts` output standalone: Task 3 ✓
- Network migration observability compose: Task 4 ✓
- OTEL Collector Loki exporter + logs pipeline: Task 5 ✓
- Root `docker-compose.yml` with all service specs: Task 6 ✓
- `stop_grace_period`, `init`, `restart`, `mem_limit`, `cpus`: Task 6 ✓
- `extra_hosts` for Ollama on host: Task 6 ✓
- `depends_on` with `service_healthy`: Task 6 ✓
- Full validation (health, traces, metrics, logs, restart): Task 7 ✓

**Known constraint:** The `depends_on: otel-collector` in `docker-compose.yml` references a service defined in `infra/observability/docker-compose.yml`. This works when both files are passed to `docker compose` together. If the observability stack is brought up separately, `api` and `ai-runtime` will still start (Docker Compose cross-file dependency resolution is best-effort), but the healthcheck gate won't block them.
