# Phase 3 — AI Runtime Design

**Date:** 2026-05-27  
**Status:** Approved  
**Phase:** 3 of 9

---

## Objective

Integrate a Python AI runtime (FastAPI + LangGraph + Ollama) into the existing event-driven pipeline. The goal is NOT to deliver AI features — the goal is to demonstrate distributed tracing across heterogeneous runtime boundaries, observable orchestration, and operationally sound execution under resource constraints.

The deliverable is a continuous, causally correct trace:

```
POST /tasks
 └── queue.enqueue
     └── worker.process
         └── ai-runtime.request
             └── graph.classify
             └── graph.generate
                 └── ollama.generate
             └── graph.validate
```

---

## Architecture Principles (frozen)

These principles are part of the architecture contract for Phase 3 and must not be violated by framework choices or future additions without explicit revision.

### Execution Model

- Synchronous inference: worker blocks on `POST /infer` until response
- Single orchestration authority: only the Go worker manages task lifecycle
- Single inference provider: Ollama only
- Inference execution is strictly serialized per worker instance in Phase 3
- No token streaming
- No callback orchestration
- No distributed choreography

### Observability-First

Tracing semantics are part of the architecture contract. Auto-generated spans must not replace explicit orchestration spans. Each LangGraph node opens and closes its own OTEL span manually. Framework callbacks must not shadow or duplicate these spans.

### Bounded Execution

All runtime components must enforce bounded resource usage:
- Bounded queue (already in place)
- Bounded SSE payload (already in place)
- Bounded output: `MAX_OUTPUT_CHARS=8000` (enforced in `generate` node)
- Bounded timeout: worker uses `context.WithTimeout(ctx, 30s)`
- Bounded inference: `remaining_budget_ms` passed to Ollama request

### Degradation Semantics

Degraded execution is considered operationally successful when the runtime remains healthy and returns bounded output. `execution_status: degraded` is not an alert condition — it is a first-class operational state. Only `execution_status: failed` triggers failure metrics and `task.failed` SSE events.

---

## Ownership Boundaries

```
Worker (Go)
  owns: task lifecycle, retries, queue semantics,
        SSE publication, orchestration timeout

FastAPI Runtime (Python)
  owns: graph execution, inference coordination,
        execution-local validation, output truncation

LangGraph
  owns: state machine, node sequencing, state transitions

Ollama
  owns: token inference only
```

The FastAPI runtime does NOT publish SSE events, does NOT own task state, and does NOT make retry decisions. These concerns belong exclusively to the worker.

---

## Components

### Go Worker — `ai.Client`

New internal package `services/api/internal/ai`. Wraps the HTTP call to FastAPI with:
- `context.WithTimeout(ctx, 30s)`
- W3C `traceparent` propagation via request header
- `deadline_unix_ms` in request body (derived from the context deadline)
- Structured error classification on non-2xx or timeout

The worker publishes SSE events at these lifecycle points:
- `task.processing` — immediately on dequeue
- `task.completed` — on successful response (includes output + metadata)
- `task.failed` — on timeout, runtime unavailable, or `execution_status: failed`

`task.inference.started` is intentionally omitted — it would require the FastAPI to emit or the worker to pre-publish before the HTTP response, adding coupling without observability benefit given that spans already provide this timing.

### FastAPI Runtime — `ai-runtime/`

Single-file entrypoint `main.py` with:
- `POST /infer` — executes the LangGraph pipeline
- `GET /health` — process alive check
- `GET /ready` — Ollama reachable + models available (used by orchestration/restart policies)
- `GET /metrics` — Prometheus exposition

**Request:**
```json
{ "task_id": "uuid", "input": "user input", "deadline_unix_ms": 1748390000000 }
```
Header: `traceparent: 00-<trace_id>-<span_id>-01`

**Response:**
```json
{
  "task_id": "uuid",
  "execution_status": "completed",
  "execution_profile": {
    "task_type": "general",
    "model": "qwen2.5:3b",
    "timeout_ms": 28500,
    "max_output_chars": 8000
  },
  "output": "...",
  "output_ref": null,
  "validation_status": "ok",
  "inference_duration_ms": 1240,
  "total_duration_ms": 1255
}
```

### LangGraph Pipeline

`StateGraph` with typed state. Three nodes, each manually instrumented with OTEL spans.

```python
class ExecutionProfile(TypedDict):
    task_type: str          # "general" | "coding"
    model: str
    timeout_ms: int
    max_output_chars: int

class GraphState(TypedDict):
    task_id: str
    input: str
    deadline_unix_ms: int
    execution_profile: ExecutionProfile
    output: str
    execution_status: str   # completed | degraded | failed
    validation_status: str  # ok | empty | truncated | invalid_output
    inference_duration_ms: int
```

**Node: `classify`**

Role: execution policy selector. Heuristic-only — no LLM call.

```python
keywords = ["code", "function", "def ", "class ", "bug", "debug", "implement"]
if any(k in input.lower() for k in keywords):
    model = "deepseek-coder:6.7b"
    task_type = "coding"
else:
    model = "qwen2.5:3b"
    task_type = "general"
```

Span: `graph.classify`. Sets `execution_profile` in state.

Future evolution point: routing by RAM pressure, priority tiers, provider selection — without refactoring the runtime.

**Node: `generate`**

Role: inference coordination. Only node that touches Ollama.

- Derives `remaining_budget_ms = deadline_unix_ms - now_unix_ms`
- Calls `POST http://localhost:11434/api/generate` with that timeout
- Enforces `MAX_OUTPUT_CHARS=8000` — truncates and sets `validation_status: truncated` if exceeded
- Span: `graph.generate` with child span `ollama.generate`

OTEL attributes on `ollama.generate`:
```
llm.provider         = "ollama"
llm.model            = "deepseek-coder:6.7b"
llm.task_type        = "coding"
llm.timeout_ms       = 28500
llm.output_chars     = 4210
llm.truncated        = false
llm.prompt_chars     = 312
```

**Node: `validate`**

Role: output sanity check. No LLM call. Deterministic and near-zero cost.

Checks: empty output → `empty`, Ollama error payload → `invalid_output`, anything else that passed through `generate` → `ok` or `truncated` (already set).

Sets final `execution_status`: `completed` if validation passes or degraded, `failed` only on unrecoverable condition.

Span: `graph.validate`.

---

## Models

| Route | Model | Rationale |
|---|---|---|
| `general` | `qwen2.5:3b` | Lighter residency, faster cold start, lower page cache pressure |
| `coding` | `deepseek-coder:6.7b` | Meaningful routing tier; heavier but justified by task type |

Both models configured via environment variables (`GENERAL_MODEL`, `CODING_MODEL`) to allow downgrades without code changes if RAM pressure demands it.

Ollama runs on the host machine outside Docker Compose for Phase 3. FastAPI accesses it at `http://localhost:11434` (or `OLLAMA_HOST` env var). Containerization happens in Phase 5.

---

## Execution Status / Validation Status Separation

| Field | Values | Meaning |
|---|---|---|
| `execution_status` | `completed`, `degraded`, `failed` | Did the runtime complete its contract? |
| `validation_status` | `ok`, `empty`, `truncated`, `invalid_output` | What is the quality of the output? |

Infrastructure failures (timeout, provider_error) map to `execution_status`, not `validation_status`. This keeps dashboards, alerting, and retry logic semantically clean.

---

## Metrics

### FastAPI (`/metrics`)

| Metric | Type | Labels |
|---|---|---|
| `ai_requests_total` | Counter | `execution_status`, `task_type` |
| `ai_request_duration_seconds` | Histogram | `task_type` |
| `ai_inference_duration_seconds` | Histogram | `model` |
| `ai_inference_in_flight` | Gauge | — |
| `ai_validation_status_total` | Counter | `validation_status` |
| `ai_failures_total` | Counter | `reason` (timeout, provider_unavailable, invalid_output, runtime_error) |
| `ai_output_chars` | Histogram | `model`, `truncated` |

### Go Worker (additions to existing `/metrics`)

| Metric | Type | Labels |
|---|---|---|
| `worker_task_duration_seconds` | Histogram | `status` |
| `worker_ai_call_duration_seconds` | Histogram | `status` |

---

## Error Handling

### Worker → FastAPI

| Scenario | Worker action | SSE event |
|---|---|---|
| Context timeout (30s) | cancel, classify as timeout | `task.failed { error_reason: "timeout" }` |
| HTTP 5xx | classify as runtime error | `task.failed { error_reason: "ai_runtime_error" }` |
| FastAPI unreachable | classify as unavailable | `task.failed { error_reason: "ai_runtime_unavailable" }` |
| `execution_status: degraded` | treat as success | `task.completed` with validation_status |
| `execution_status: failed` | treat as failure | `task.failed` |

### FastAPI internal

| Scenario | execution_status | validation_status | HTTP |
|---|---|---|---|
| Normal completion | completed | ok | 200 |
| Output truncated | degraded | truncated | 200 |
| Output empty | degraded | empty | 200 |
| Ollama timeout | failed | — | 200 |
| Ollama unreachable | failed | — | 503 |
| Invalid Ollama output | degraded | invalid_output | 200 |
| Unhandled exception | failed | — | 500 |

HTTP 5xx is reserved for FastAPI runtime failures. Business-level execution outcomes return 200 with `execution_status` as the signal. This keeps the worker as the lifecycle authority.

### Health vs Readiness

```
GET /health → { "status": "ok" }
  Process is alive. No Ollama check.

GET /ready  → { "status": "ok" | "degraded", "ollama": "reachable" | "unreachable" }
  Inference capability check. Used by restart policies and orchestration.
```

---

## Files to Create

```
ai-runtime/
  main.py              # FastAPI app, lifespan, /infer, /health, /ready, /metrics
  graph.py             # LangGraph StateGraph: classify → generate → validate
  state.py             # GraphState, ExecutionProfile TypedDicts
  ollama_client.py     # Thin wrapper: POST /api/generate with timeout + span
  telemetry.py         # OTEL setup: tracer, propagator, stdout exporter (Phase 4 switches to collector)
  metrics.py           # Prometheus gauges, counters, histograms
  requirements.txt

services/api/internal/ai/
  client.go            # ai.Client: HTTP call with timeout, traceparent, deadline
  client_test.go

services/api/internal/worker/
  worker.go            # Modified: call ai.Client, enrich task.completed with output
```

---

## Validation Checklist (Definition of Done)

- [ ] FastAPI starts and `/health` returns `ok`
- [ ] `/ready` returns `degraded` when Ollama is offline, `ok` when online
- [ ] `POST /infer` returns valid response with all fields populated
- [ ] Worker calls FastAPI and publishes `task.completed` with output in SSE
- [ ] Frontend displays output in event feed
- [ ] Trace is continuous from `POST /tasks` through `ollama.generate` (verify in stdout spans)
- [ ] `graph.classify`, `graph.generate`, `graph.validate` appear as separate child spans
- [ ] `ollama.generate` appears as child of `graph.generate` with all `llm.*` attributes
- [ ] `qwen2.5:3b` selected for non-code input, `deepseek-coder:6.7b` for code input
- [ ] Output truncated at 8000 chars, `validation_status: truncated` returned
- [ ] `task.failed` published when FastAPI is stopped mid-flight (timeout scenario)
- [ ] `ai_inference_in_flight` gauge correctly reflects 0 at rest, 1 during inference
- [ ] Structured logs visible in FastAPI with `task_id`, `model`, `execution_status`

---

## Out of Scope (Phase 3)

- S3 persistence (`output_ref` always null)
- Token streaming
- Circuit breaker
- Multiple concurrent workers
- Model fallback on RAM pressure
- Docker containerization of AI runtime (Phase 5)
- OTEL Collector (Phase 4 — stdout exporter for now)
- LangChain callbacks or auto-instrumentation
