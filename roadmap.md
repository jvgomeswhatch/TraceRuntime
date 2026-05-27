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

# Phase 7 — Terraform

Provision with Terraform:

- SQS
- DLQs
- SNS
- S3

Goal:

- reproducible infrastructure
- minimal IaC

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

# Final Result

The platform should resemble:

- distributed runtime infrastructure
- internal platform tooling
- operational control systems

NOT:

- a chatbot clone
- a CRUD SaaS
- an AI wrapper app
