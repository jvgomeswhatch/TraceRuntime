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

# Phase 7.5 — Task Persistence (PostgreSQL + S3)

Integrate PostgreSQL into the worker runtime:

- worker writes task state transitions to PostgreSQL (`pending → processing → completed/failed`)
- S3 key recorded in PostgreSQL after artifact write — no artifact exists without a database record
- PostgreSQL becomes the system of record for task state and artifact metadata
- S3 remains the artifact store for large or binary outputs (PDFs, images, audio)

Goal:

- tasks survive restarts — state is durable, not in-memory
- full traceability: task → trace → artifact → S3 key
- operational queries become possible: "which tasks failed?", "which tasks generated artifacts?"

Note: This phase resolves the known gap from Phase 6 where task state lives exclusively in memory/SQS and S3 has no corresponding PostgreSQL record.

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

# Final Result

The platform should resemble:

- distributed runtime infrastructure
- internal platform tooling
- operational control systems

NOT:

- a chatbot clone
- a CRUD SaaS
- an AI wrapper app
