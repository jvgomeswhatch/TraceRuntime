# Phase 5 — Docker Compose Design

**Date:** 2026-05-27
**Phase:** 5 of 9

---

## Objective

Containerize all application services and wire them into a reproducible local environment using Docker Compose. The observability stack already exists in `infra/observability/docker-compose.yml` and remains unchanged except for network migration. The application services (API+Worker, AI Runtime, Frontend) each get their own Dockerfile. Both compose files share a single Docker network.

---

## Constraints

- Total RAM budget: 14GB across all containers
- No GPU
- Ollama runs on the host (not containerized) — all containers reach it via `host.docker.internal:11434`
- One compose file per concern: app vs observability
- OTEL exporters use gRPC (port 4317), not HTTP

---

## File Structure

```
docker-compose.yml                        ← app services (populated in this phase)
infra/observability/docker-compose.yml    ← unchanged except network name

services/api/Dockerfile                   ← Go multi-stage build
services/api/.dockerignore

ai-runtime/Dockerfile                     ← Python slim
ai-runtime/.dockerignore

frontend/Dockerfile                       ← Node builder + standalone runner
frontend/.dockerignore
frontend/next.config.ts                   ← requires output: 'standalone'
```

---

## Network Architecture

A single shared bridge network `traceruntime` is declared in the app compose file. The observability compose references it as external.

```
docker-compose.yml          → creates network: traceruntime
infra/observability/        → external: true (joins traceruntime)
```

All services — app and observability — resolve each other by service name on this network. No second network needed.

---

## Application Services

### `api` (Go API + Worker)

- **Build:** `services/api/`
- **Port:** 8082
- **Memory:** 256m
- **CPU:** 0.5
- **Healthcheck:** `curl -f http://localhost:8082/health`
- **depends_on:** `otel-collector` (service_healthy)
- **restart:** unless-stopped
- **init:** true
- **stop_grace_period:** 10s

### `ai-runtime` (Python FastAPI + LangGraph)

- **Build:** `ai-runtime/`
- **Port:** 8000
- **Memory:** 768m
- **CPU:** 1.0
- **Healthcheck:** `curl -f http://localhost:8000/health`
- **depends_on:** `otel-collector` (service_healthy)
- **restart:** unless-stopped
- **init:** true
- **stop_grace_period:** 10s
- **extra_hosts:** `host.docker.internal:host-gateway`

### `frontend` (Next.js standalone)

- **Build:** `frontend/`
- **Port:** 3001
- **Memory:** 256m (monitor — pode precisar de 384m dependendo do bundle)
- **CPU:** 0.5
- **Healthcheck:** `curl -f http://localhost:3001/`
- **depends_on:** `api` (service_healthy)
- **restart:** unless-stopped
- **init:** true

---

## Environment Variables

All app services share these base envs:

```
OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317
```

`api`:
```
AI_RUNTIME_URL=http://ai-runtime:8000
```

`ai-runtime`:
```
OLLAMA_BASE_URL=http://host.docker.internal:11434
```

---

## Memory Budget

| Service | Limit |
|---|---|
| api | 256m |
| ai-runtime | 768m |
| frontend | 256m |
| otel-collector | 384m |
| prometheus | 512m |
| tempo | 512m |
| loki | 512m |
| grafana | 256m |
| **Total** | **~3.4GB** |

Leaves ~10GB for Ollama model on host + OS + buffer.

---

## Dockerfiles

### Go API — `services/api/Dockerfile`

Two stages:

1. **builder:** `golang:1.22-alpine` — `CGO_ENABLED=0 go build -ldflags="-s -w" ./cmd/server/...`
2. **runner:** `alpine:3.19` — minimal, has curl for healthcheck, runs as non-root user

### Python AI Runtime — `ai-runtime/Dockerfile`

Single stage: `python:3.11-slim`

Required env vars baked in:
```
ENV PYTHONUNBUFFERED=1
ENV PYTHONDONTWRITEBYTECODE=1
```

Entrypoint: `uvicorn main:app --host 0.0.0.0 --port 8000`

### Frontend — `frontend/Dockerfile`

Two stages:

1. **builder:** `node:20-alpine` — `npm ci && npm run build`
2. **runner:** `node:20-alpine` — copies `.next/standalone` only

**Required:** `next.config.ts` must include `output: 'standalone'`. Without this the standalone directory is not generated and the container fails to start.

---

## Observability Compose — Network Change

`infra/observability/docker-compose.yml` currently declares its own `observability` network. In this phase it is updated to use `traceruntime` instead:

```yaml
networks:
  traceruntime:
    external: true
```

All service `networks:` references inside that file change from `observability` to `traceruntime`.

---

## How to Run

```bash
docker compose \
  -f docker-compose.yml \
  -f infra/observability/docker-compose.yml \
  up --build
```

To bring down:

```bash
docker compose \
  -f docker-compose.yml \
  -f infra/observability/docker-compose.yml \
  down
```

---

## Validation Checklist

1. `docker compose ... up --build` completes without errors
2. All containers reach `healthy` status (`docker ps`)
3. `curl http://localhost:8082/health` → 200
4. `curl http://localhost:8000/health` → 200
5. `curl http://localhost:3001/` → 200
6. Frontend loads in browser and SSE stream connects
7. Create a task via UI — trace visible in Grafana/Tempo
8. Metrics visible in Prometheus/Grafana
9. Logs appear in Loki (via OTEL Collector loki exporter)
10. Kill one container — `restart: unless-stopped` brings it back

---

## Logs Pipeline — OTEL Collector → Loki

Logs are shipped via the OTEL Collector using its `loki` exporter. No Promtail, no Docker logging driver.

**Flow:**
```
app logs (stdout JSON)
→ OTEL Collector (otlp receiver)
→ loki exporter
→ Loki
```

**Why OTEL Collector:**
- Already present in the stack — no extra container
- `trace_id` flows through logs automatically (W3C context)
- Enables Grafana trace↔log correlation out of the box
- Less moving parts than Promtail

**Required changes to `infra/observability/otel-collector/config.yaml`:**
- Add `loki` exporter pointing to `http://loki:3100/loki/api/v1/push`
- Add logs pipeline: `receivers: [otlp] → processors: [batch] → exporters: [loki]`

**Required changes to app services:**
- Go API and Python AI Runtime must export logs via OTEL SDK (structured JSON to stdout is sufficient for now — the OTEL Collector picks them up via the otlp receiver if the SDK is wired, or a file/stdout log receiver can be added)

**Simplest viable approach for Phase 5:** configure the OTEL Collector with a `loki` exporter and add it to the existing pipeline. Services already emit structured JSON to stdout — if direct OTEL log export is not yet wired in the SDKs, defer log correlation to Phase 6 and treat this as infrastructure-ready.

---

## Definition of Done

- All containers healthy
- Traces visible in Tempo end-to-end
- Logs visible in Loki (OTEL Collector pipeline active)
- Metrics in Grafana
- Frontend reflects state in real time
- Memory limits respected (no OOMKill — monitor frontend RSS under load)
- `docker compose down && docker compose up --build` is fully reproducible
