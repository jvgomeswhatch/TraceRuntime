# Phase 3 — AI Runtime Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Integrate a FastAPI + LangGraph + Ollama AI runtime into the existing Go event-driven pipeline with continuous distributed tracing from `POST /tasks` through `ollama.generate`.

**Architecture:** The Go worker calls `POST /infer` synchronously (30s timeout, W3C traceparent propagated via header). FastAPI executes a 3-node LangGraph pipeline (`classify → generate → validate`) with manual OTEL spans per node. The worker remains the sole owner of task lifecycle and SSE publication.

**Tech Stack:** Python 3.11+, FastAPI, LangGraph, opentelemetry-sdk, prometheus-client, httpx, Go 1.22+

---

## File Map

### New files — `ai-runtime/`

| File | Responsibility |
|---|---|
| `ai-runtime/state.py` | `GraphState` and `ExecutionProfile` TypedDicts |
| `ai-runtime/telemetry.py` | OTEL TracerProvider + W3C propagator setup |
| `ai-runtime/metrics.py` | Prometheus counters, histograms, gauge |
| `ai-runtime/ollama_client.py` | Thin httpx wrapper for `POST /api/generate` with span |
| `ai-runtime/graph.py` | LangGraph `StateGraph`: classify → generate → validate |
| `ai-runtime/main.py` | FastAPI app: `/infer`, `/health`, `/ready`, `/metrics` |
| `ai-runtime/requirements.txt` | Python dependencies |

### New files — Go

| File | Responsibility |
|---|---|
| `services/api/internal/ai/client.go` | `ai.Client`: HTTP call with timeout, traceparent, deadline |
| `services/api/internal/ai/client_test.go` | Tests for `ai.Client` |

### Modified files — Go

| File | Change |
|---|---|
| `services/api/internal/worker/worker.go` | Replace sleep with `ai.Client` call; enrich SSE events with output/metadata |

---

## Task 1: Python project scaffold + state types

**Files:**
- Create: `ai-runtime/requirements.txt`
- Create: `ai-runtime/state.py`

- [ ] **Step 1: Create `requirements.txt`**

```
fastapi==0.115.12
uvicorn[standard]==0.34.2
langgraph==0.4.5
opentelemetry-api==1.33.1
opentelemetry-sdk==1.33.1
prometheus-client==0.22.1
httpx==0.28.1
```

- [ ] **Step 2: Install dependencies**

```bash
cd ai-runtime
python -m venv .venv
# Windows:
.venv\Scripts\activate
pip install -r requirements.txt
```

Expected: all packages install without errors.

- [ ] **Step 3: Create `state.py`**

```python
from typing import TypedDict


class ExecutionProfile(TypedDict):
    task_type: str        # "general" | "coding"
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

- [ ] **Step 4: Verify import**

```bash
python -c "from state import GraphState, ExecutionProfile; print('ok')"
```

Expected: `ok`

- [ ] **Step 5: Commit**

```bash
git add ai-runtime/requirements.txt ai-runtime/state.py
git commit -m "feat(phase3): scaffold ai-runtime with state types"
```

---

## Task 2: OTEL telemetry setup

**Files:**
- Create: `ai-runtime/telemetry.py`

- [ ] **Step 1: Create `telemetry.py`**

```python
import os
from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor, ConsoleSpanExporter
from opentelemetry.sdk.resources import Resource
from opentelemetry.propagators.b3 import B3Format
from opentelemetry.propagate import set_global_textmap
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator


def init_telemetry(service_name: str = "eventdrive-ai-runtime") -> TracerProvider:
    resource = Resource.create({"service.name": service_name, "service.version": "0.1.0"})
    provider = TracerProvider(resource=resource)
    provider.add_span_processor(BatchSpanProcessor(ConsoleSpanExporter()))
    trace.set_tracer_provider(provider)
    set_global_textmap(TraceContextTextMapPropagator())
    return provider


def get_tracer(name: str):
    return trace.get_tracer(name)
```

- [ ] **Step 2: Verify import**

```bash
python -c "from telemetry import init_telemetry, get_tracer; init_telemetry(); print('ok')"
```

Expected: `ok` (may print OTEL setup info).

- [ ] **Step 3: Commit**

```bash
git add ai-runtime/telemetry.py
git commit -m "feat(phase3): add OTEL telemetry setup for ai-runtime"
```

---

## Task 3: Prometheus metrics

**Files:**
- Create: `ai-runtime/metrics.py`

- [ ] **Step 1: Create `metrics.py`**

```python
from prometheus_client import Counter, Histogram, Gauge

ai_requests_total = Counter(
    "ai_requests_total",
    "Total inference requests",
    ["execution_status", "task_type"],
)

ai_request_duration_seconds = Histogram(
    "ai_request_duration_seconds",
    "Total request duration including graph execution",
    ["task_type"],
    buckets=[0.1, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0],
)

ai_inference_duration_seconds = Histogram(
    "ai_inference_duration_seconds",
    "Ollama inference duration",
    ["model"],
    buckets=[0.1, 0.5, 1.0, 2.5, 5.0, 10.0, 30.0],
)

ai_inference_in_flight = Gauge(
    "ai_inference_in_flight",
    "Number of Ollama inference calls currently in progress",
)

ai_validation_status_total = Counter(
    "ai_validation_status_total",
    "Output validation outcomes",
    ["validation_status"],
)

ai_failures_total = Counter(
    "ai_failures_total",
    "Inference failures by reason",
    ["reason"],
)

ai_output_chars = Histogram(
    "ai_output_chars",
    "Output size in characters",
    ["model", "truncated"],
    buckets=[100, 500, 1000, 2000, 4000, 8000],
)
```

- [ ] **Step 2: Verify import**

```bash
python -c "from metrics import ai_inference_in_flight; print('ok')"
```

Expected: `ok`

- [ ] **Step 3: Commit**

```bash
git add ai-runtime/metrics.py
git commit -m "feat(phase3): add Prometheus metrics for ai-runtime"
```

---

## Task 4: Ollama client

**Files:**
- Create: `ai-runtime/ollama_client.py`

- [ ] **Step 1: Create `ollama_client.py`**

```python
import time
import os
import httpx
from opentelemetry import trace
from opentelemetry.trace import Status, StatusCode

from metrics import ai_inference_in_flight, ai_inference_duration_seconds, ai_output_chars
from telemetry import get_tracer

OLLAMA_HOST = os.getenv("OLLAMA_HOST", "http://localhost:11434")
MAX_OUTPUT_CHARS = int(os.getenv("MAX_OUTPUT_CHARS", "8000"))

_tracer = get_tracer("eventdrive-ai-runtime/ollama")


def generate(model: str, prompt: str, timeout_ms: int, task_type: str) -> tuple[str, bool]:
    """
    Call Ollama /api/generate. Returns (output, truncated).
    Opens an 'ollama.generate' child span with llm.* attributes.
    Raises httpx.TimeoutException on timeout, httpx.HTTPError on provider error.
    """
    timeout_s = timeout_ms / 1000.0

    with _tracer.start_as_current_span("ollama.generate") as span:
        span.set_attribute("llm.provider", "ollama")
        span.set_attribute("llm.model", model)
        span.set_attribute("llm.task_type", task_type)
        span.set_attribute("llm.timeout_ms", timeout_ms)
        span.set_attribute("llm.prompt_chars", len(prompt))

        ai_inference_in_flight.inc()
        start = time.monotonic()
        try:
            resp = httpx.post(
                f"{OLLAMA_HOST}/api/generate",
                json={"model": model, "prompt": prompt, "stream": False},
                timeout=timeout_s,
            )
            resp.raise_for_status()
            raw = resp.json().get("response", "")
        except Exception:
            span.set_status(Status(StatusCode.ERROR))
            raise
        finally:
            ai_inference_in_flight.dec()
            elapsed_ms = int((time.monotonic() - start) * 1000)
            ai_inference_duration_seconds.labels(model=model).observe(elapsed_ms / 1000.0)

        truncated = len(raw) > MAX_OUTPUT_CHARS
        output = raw[:MAX_OUTPUT_CHARS] if truncated else raw

        span.set_attribute("llm.output_chars", len(output))
        span.set_attribute("llm.truncated", truncated)

        ai_output_chars.labels(model=model, truncated=str(truncated).lower()).observe(len(output))

        return output, truncated
```

- [ ] **Step 2: Smoke-test against live Ollama**

Make sure Ollama is running, then:

```bash
python -c "
from telemetry import init_telemetry
init_telemetry()
from ollama_client import generate
out, trunc = generate('qwen2.5:3b', 'Say hello in one word.', 15000, 'general')
print('output:', out[:80])
print('truncated:', trunc)
"
```

Expected: short response printed, `truncated: False`.

- [ ] **Step 3: Commit**

```bash
git add ai-runtime/ollama_client.py
git commit -m "feat(phase3): add Ollama client with OTEL span and metrics"
```

---

## Task 5: LangGraph pipeline

**Files:**
- Create: `ai-runtime/graph.py`

- [ ] **Step 1: Create `graph.py`**

```python
import os
import time
import logging
import httpx
from opentelemetry.trace import Status, StatusCode

from langgraph.graph import StateGraph, END
from state import GraphState, ExecutionProfile
from telemetry import get_tracer
import ollama_client

log = logging.getLogger(__name__)
_tracer = get_tracer("eventdrive-ai-runtime/graph")

GENERAL_MODEL = os.getenv("GENERAL_MODEL", "qwen2.5:3b")
CODING_MODEL = os.getenv("CODING_MODEL", "deepseek-coder:6.7b")
_CODING_KEYWORDS = {"code", "function", "def ", "class ", "bug", "debug", "implement", "algorithm", "script"}


def _classify_node(state: GraphState) -> dict:
    with _tracer.start_as_current_span("graph.classify") as span:
        inp = state["input"].lower()
        if any(kw in inp for kw in _CODING_KEYWORDS):
            task_type, model = "coding", CODING_MODEL
        else:
            task_type, model = "general", GENERAL_MODEL

        deadline_ms = state["deadline_unix_ms"]
        now_ms = int(time.time() * 1000)
        remaining_ms = max(deadline_ms - now_ms, 1000)

        profile: ExecutionProfile = {
            "task_type": task_type,
            "model": model,
            "timeout_ms": remaining_ms,
            "max_output_chars": int(os.getenv("MAX_OUTPUT_CHARS", "8000")),
        }
        span.set_attribute("classify.task_type", task_type)
        span.set_attribute("classify.model", model)
        span.set_attribute("classify.remaining_budget_ms", remaining_ms)

        log.info("classify node", extra={"task_id": state["task_id"], "task_type": task_type, "model": model})
        return {"execution_profile": profile}


def _generate_node(state: GraphState) -> dict:
    profile = state["execution_profile"]
    start = time.monotonic()

    with _tracer.start_as_current_span("graph.generate") as span:
        span.set_attribute("generate.model", profile["model"])
        span.set_attribute("generate.timeout_ms", profile["timeout_ms"])

        try:
            output, truncated = ollama_client.generate(
                model=profile["model"],
                prompt=state["input"],
                timeout_ms=profile["timeout_ms"],
                task_type=profile["task_type"],
            )
            elapsed_ms = int((time.monotonic() - start) * 1000)

            validation_status = "truncated" if truncated else ""
            execution_status = "degraded" if truncated else "completed"

            log.info("generate node", extra={
                "task_id": state["task_id"],
                "model": profile["model"],
                "elapsed_ms": elapsed_ms,
                "truncated": truncated,
            })

            return {
                "output": output,
                "execution_status": execution_status,
                "validation_status": validation_status,
                "inference_duration_ms": elapsed_ms,
            }

        except httpx.TimeoutException:
            span.set_status(Status(StatusCode.ERROR))
            elapsed_ms = int((time.monotonic() - start) * 1000)
            log.error("generate timeout", extra={"task_id": state["task_id"], "elapsed_ms": elapsed_ms})
            return {
                "output": "",
                "execution_status": "failed",
                "validation_status": "",
                "inference_duration_ms": elapsed_ms,
            }

        except httpx.HTTPError as e:
            span.set_status(Status(StatusCode.ERROR))
            elapsed_ms = int((time.monotonic() - start) * 1000)
            log.error("generate provider error", extra={"task_id": state["task_id"], "error": str(e)})
            return {
                "output": "",
                "execution_status": "failed",
                "validation_status": "",
                "inference_duration_ms": elapsed_ms,
            }


def _validate_node(state: GraphState) -> dict:
    with _tracer.start_as_current_span("graph.validate") as span:
        output = state.get("output", "")
        execution_status = state.get("execution_status", "failed")
        validation_status = state.get("validation_status", "")

        if execution_status == "failed":
            span.set_attribute("validate.result", "skipped_on_failure")
            return {}

        if not output.strip():
            validation_status = "empty"
            execution_status = "degraded"
        elif not validation_status:
            validation_status = "ok"
            execution_status = "completed"

        span.set_attribute("validate.validation_status", validation_status)
        span.set_attribute("validate.execution_status", execution_status)

        log.info("validate node", extra={
            "task_id": state["task_id"],
            "validation_status": validation_status,
            "execution_status": execution_status,
        })

        return {"validation_status": validation_status, "execution_status": execution_status}


def build_graph() -> StateGraph:
    g = StateGraph(GraphState)
    g.add_node("classify", _classify_node)
    g.add_node("generate", _generate_node)
    g.add_node("validate", _validate_node)
    g.set_entry_point("classify")
    g.add_edge("classify", "generate")
    g.add_edge("generate", "validate")
    g.add_edge("validate", END)
    return g.compile()
```

- [ ] **Step 2: Smoke-test the graph**

```bash
python -c "
from telemetry import init_telemetry
init_telemetry()
from graph import build_graph
import time

graph = build_graph()
result = graph.invoke({
    'task_id': 'test-001',
    'input': 'Say hello in one sentence.',
    'deadline_unix_ms': int(time.time() * 1000) + 30000,
    'execution_profile': {},
    'output': '',
    'execution_status': '',
    'validation_status': '',
    'inference_duration_ms': 0,
})
print('execution_status:', result['execution_status'])
print('validation_status:', result['validation_status'])
print('model:', result['execution_profile']['model'])
print('output[:80]:', result['output'][:80])
"
```

Expected: `execution_status: completed`, `validation_status: ok`, `model: qwen2.5:3b`.

- [ ] **Step 3: Test coding routing**

```bash
python -c "
from telemetry import init_telemetry
init_telemetry()
from graph import build_graph
import time

graph = build_graph()
result = graph.invoke({
    'task_id': 'test-002',
    'input': 'Write a Python function to reverse a string.',
    'deadline_unix_ms': int(time.time() * 1000) + 30000,
    'execution_profile': {},
    'output': '',
    'execution_status': '',
    'validation_status': '',
    'inference_duration_ms': 0,
})
print('model:', result['execution_profile']['model'])
"
```

Expected: `model: deepseek-coder:6.7b`.

- [ ] **Step 4: Commit**

```bash
git add ai-runtime/graph.py
git commit -m "feat(phase3): add LangGraph pipeline with classify/generate/validate nodes"
```

---

## Task 6: FastAPI application

**Files:**
- Create: `ai-runtime/main.py`

- [ ] **Step 1: Create `main.py`**

```python
import logging
import os
import time
from contextlib import asynccontextmanager

import httpx
from fastapi import FastAPI, Request, Response
from fastapi.responses import JSONResponse
from opentelemetry import trace
from opentelemetry.propagate import extract
from prometheus_client import generate_latest, CONTENT_TYPE_LATEST

from telemetry import init_telemetry, get_tracer
from metrics import (
    ai_requests_total,
    ai_request_duration_seconds,
    ai_validation_status_total,
    ai_failures_total,
)
from graph import build_graph
from state import GraphState

logging.basicConfig(
    level=logging.INFO,
    format='{"time": "%(asctime)s", "level": "%(levelname)s", "msg": "%(message)s", "name": "%(name)s"}',
)
log = logging.getLogger(__name__)

OLLAMA_HOST = os.getenv("OLLAMA_HOST", "http://localhost:11434")
_graph = None
_tracer = None


@asynccontextmanager
async def lifespan(app: FastAPI):
    global _graph, _tracer
    init_telemetry("eventdrive-ai-runtime")
    _tracer = get_tracer("eventdrive-ai-runtime")
    _graph = build_graph()
    log.info("ai-runtime started", extra={"ollama_host": OLLAMA_HOST})
    yield
    log.info("ai-runtime shutting down")


app = FastAPI(lifespan=lifespan)


@app.get("/health")
def health():
    return {"status": "ok"}


@app.get("/ready")
def ready():
    try:
        httpx.get(f"{OLLAMA_HOST}/api/tags", timeout=3.0)
        ollama_status = "reachable"
        status = "ok"
    except Exception:
        ollama_status = "unreachable"
        status = "degraded"
    return {"status": status, "ollama": ollama_status}


@app.get("/metrics")
def metrics():
    return Response(generate_latest(), media_type=CONTENT_TYPE_LATEST)


class InferRequest:
    def __init__(self, task_id: str, input: str, deadline_unix_ms: int):
        self.task_id = task_id
        self.input = input
        self.deadline_unix_ms = deadline_unix_ms


from pydantic import BaseModel

class InferRequestModel(BaseModel):
    task_id: str
    input: str
    deadline_unix_ms: int


@app.post("/infer")
async def infer(body: InferRequestModel, request: Request):
    # Restore W3C trace context from incoming traceparent header.
    carrier = dict(request.headers)
    ctx = extract(carrier)
    token = trace.context_api.attach(ctx)

    start = time.monotonic()
    task_id = body.task_id
    log.info("infer request received", extra={"task_id": task_id})

    try:
        initial_state: GraphState = {
            "task_id": task_id,
            "input": body.input,
            "deadline_unix_ms": body.deadline_unix_ms,
            "execution_profile": {},
            "output": "",
            "execution_status": "",
            "validation_status": "",
            "inference_duration_ms": 0,
        }

        result = _graph.invoke(initial_state)

        execution_status = result["execution_status"]
        validation_status = result.get("validation_status", "")
        profile = result["execution_profile"]
        task_type = profile.get("task_type", "unknown")

        total_ms = int((time.monotonic() - start) * 1000)

        ai_requests_total.labels(execution_status=execution_status, task_type=task_type).inc()
        ai_request_duration_seconds.labels(task_type=task_type).observe(total_ms / 1000.0)
        if validation_status:
            ai_validation_status_total.labels(validation_status=validation_status).inc()

        if execution_status == "failed":
            ai_failures_total.labels(reason="graph_failed").inc()

        log.info("infer request completed", extra={
            "task_id": task_id,
            "execution_status": execution_status,
            "validation_status": validation_status,
            "model": profile.get("model"),
            "total_ms": total_ms,
        })

        http_status = 503 if execution_status == "failed" and not result.get("output") else 200

        return JSONResponse(
            status_code=http_status,
            content={
                "task_id": task_id,
                "execution_status": execution_status,
                "execution_profile": profile,
                "output": result.get("output", ""),
                "output_ref": None,
                "validation_status": validation_status,
                "inference_duration_ms": result.get("inference_duration_ms", 0),
                "total_duration_ms": total_ms,
            },
        )

    except Exception as e:
        total_ms = int((time.monotonic() - start) * 1000)
        ai_failures_total.labels(reason="runtime_error").inc()
        log.error("infer unhandled error", extra={"task_id": task_id, "error": str(e)})
        return JSONResponse(status_code=500, content={"error": "internal runtime error"})

    finally:
        trace.context_api.detach(token)
```

- [ ] **Step 2: Start the FastAPI server**

```bash
cd ai-runtime
uvicorn main:app --host 0.0.0.0 --port 8001 --reload
```

Expected: `Application startup complete.` with no errors.

- [ ] **Step 3: Test `/health`**

```bash
curl -s http://localhost:8001/health | python -m json.tool
```

Expected: `{"status": "ok"}`

- [ ] **Step 4: Test `/ready` with Ollama running**

```bash
curl -s http://localhost:8001/ready | python -m json.tool
```

Expected: `{"status": "ok", "ollama": "reachable"}`

- [ ] **Step 5: Test `POST /infer` end-to-end**

```bash
curl -s -X POST http://localhost:8001/infer \
  -H "Content-Type: application/json" \
  -d "{\"task_id\": \"manual-001\", \"input\": \"Say hello in one sentence.\", \"deadline_unix_ms\": $(($(date +%s%3N) + 30000))}" \
  | python -m json.tool
```

Expected: JSON with `execution_status: "completed"`, `validation_status: "ok"`, `output` non-empty, `execution_profile.model: "qwen2.5:3b"`.

- [ ] **Step 6: Test coding routing**

```bash
curl -s -X POST http://localhost:8001/infer \
  -H "Content-Type: application/json" \
  -d "{\"task_id\": \"manual-002\", \"input\": \"Write a Python function to reverse a string.\", \"deadline_unix_ms\": $(($(date +%s%3N) + 30000))}" \
  | python -m json.tool
```

Expected: `execution_profile.model: "deepseek-coder:6.7b"`.

- [ ] **Step 7: Commit**

```bash
git add ai-runtime/main.py
git commit -m "feat(phase3): add FastAPI app with /infer, /health, /ready, /metrics"
```

---

## Task 7: Go `ai.Client`

**Files:**
- Create: `services/api/internal/ai/client.go`
- Create: `services/api/internal/ai/client_test.go`

- [ ] **Step 1: Write the failing test**

Create `services/api/internal/ai/client_test.go`:

```go
package ai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/runtime-platform/services/api/internal/ai"
)

func TestClient_CallsInferEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/infer" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("traceparent") == "" {
			t.Error("expected traceparent header")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task_id":              "task-001",
			"execution_status":    "completed",
			"execution_profile":   map[string]any{"task_type": "general", "model": "qwen2.5:3b"},
			"output":              "hello world",
			"output_ref":          nil,
			"validation_status":   "ok",
			"inference_duration_ms": 500,
			"total_duration_ms":   510,
		})
	}))
	defer srv.Close()

	c := ai.NewClient(srv.URL, 10*time.Second)
	resp, err := c.Infer(context.Background(), ai.InferRequest{
		TaskID:          "task-001",
		Input:           "hello",
		DeadlineUnixMs:  time.Now().Add(30*time.Second).UnixMilli(),
		Traceparent:     "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ExecutionStatus != "completed" {
		t.Errorf("expected completed, got %q", resp.ExecutionStatus)
	}
	if resp.Output != "hello world" {
		t.Errorf("expected 'hello world', got %q", resp.Output)
	}
}

func TestClient_ReturnsErrorOn5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := ai.NewClient(srv.URL, 5*time.Second)
	_, err := c.Infer(context.Background(), ai.InferRequest{
		TaskID: "task-002", Input: "hi",
		DeadlineUnixMs: time.Now().Add(30*time.Second).UnixMilli(),
		Traceparent:    "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	})
	if err == nil {
		t.Fatal("expected error on 5xx, got nil")
	}
}

func TestClient_ReturnsErrorOnTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := ai.NewClient(srv.URL, 50*time.Millisecond)
	_, err := c.Infer(context.Background(), ai.InferRequest{
		TaskID: "task-003", Input: "hi",
		DeadlineUnixMs: time.Now().Add(30*time.Second).UnixMilli(),
		Traceparent:    "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}
```

- [ ] **Step 2: Run tests — verify they fail**

```bash
cd services/api
go test ./internal/ai/... -v
```

Expected: `cannot find package` or `no Go files` — confirms tests exist before implementation.

- [ ] **Step 3: Create `client.go`**

Create `services/api/internal/ai/client.go`:

```go
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// InferRequest is sent from the Go worker to the FastAPI AI runtime.
type InferRequest struct {
	TaskID         string `json:"task_id"`
	Input          string `json:"input"`
	DeadlineUnixMs int64  `json:"deadline_unix_ms"`
	Traceparent    string `json:"-"` // sent as header, not body
}

// ExecutionProfile mirrors the Python ExecutionProfile TypedDict.
type ExecutionProfile struct {
	TaskType       string `json:"task_type"`
	Model          string `json:"model"`
	TimeoutMs      int    `json:"timeout_ms"`
	MaxOutputChars int    `json:"max_output_chars"`
}

// InferResponse is the parsed response from POST /infer.
type InferResponse struct {
	TaskID              string           `json:"task_id"`
	ExecutionStatus     string           `json:"execution_status"`
	ExecutionProfile    ExecutionProfile `json:"execution_profile"`
	Output              string           `json:"output"`
	OutputRef           *string          `json:"output_ref"`
	ValidationStatus    string           `json:"validation_status"`
	InferenceDurationMs int              `json:"inference_duration_ms"`
	TotalDurationMs     int              `json:"total_duration_ms"`
}

// Client calls the FastAPI AI runtime.
type Client struct {
	baseURL string
	timeout time.Duration
	http    *http.Client
}

// NewClient creates an ai.Client. timeout is the per-call deadline.
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		timeout: timeout,
		http:    &http.Client{Timeout: timeout},
	}
}

// Infer calls POST /infer on the AI runtime.
// It propagates the W3C traceparent via request header.
func (c *Client) Infer(ctx context.Context, req InferRequest) (*InferResponse, error) {
	body, err := json.Marshal(map[string]any{
		"task_id":          req.TaskID,
		"input":            req.Input,
		"deadline_unix_ms": req.DeadlineUnixMs,
	})
	if err != nil {
		return nil, fmt.Errorf("ai client: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/infer", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ai client: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if req.Traceparent != "" {
		httpReq.Header.Set("traceparent", req.Traceparent)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ai client: request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("ai client: runtime error: HTTP %d", resp.StatusCode)
	}

	var result InferResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ai client: decode response: %w", err)
	}
	return &result, nil
}
```

- [ ] **Step 4: Run tests — verify they pass**

```bash
cd services/api
go test ./internal/ai/... -v
```

Expected: all three tests `PASS`.

- [ ] **Step 5: Commit**

```bash
git add services/api/internal/ai/
git commit -m "feat(phase3): add ai.Client for Go worker → FastAPI communication"
```

---

## Task 8: Wire `ai.Client` into the Go worker

**Files:**
- Modify: `services/api/internal/worker/worker.go`

The current worker does a `time.Sleep` (simulated work). Replace it with a real `ai.Client` call. The `sseEvent` struct needs two new optional fields: `Output` and `Model`.

- [ ] **Step 1: Write failing test for worker with AI client**

Add to `services/api/internal/worker/worker_test.go` (create file):

```go
package worker_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/runtime-platform/services/api/internal/ai"
	"github.com/runtime-platform/services/api/internal/event"
	"github.com/runtime-platform/services/api/internal/queue"
	"github.com/runtime-platform/services/api/internal/worker"
)

func TestWorker_PublishesCompletedWithOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task_id":               "task-001",
			"execution_status":      "completed",
			"execution_profile":     map[string]any{"task_type": "general", "model": "qwen2.5:3b"},
			"output":                "the answer",
			"output_ref":            nil,
			"validation_status":     "ok",
			"inference_duration_ms": 500,
			"total_duration_ms":     510,
		})
	}))
	defer srv.Close()

	broker := event.NewBroker()
	ch := broker.Subscribe("test")
	defer broker.Unsubscribe("test")

	q := queue.NewQueue(1)
	aiClient := ai.NewClient(srv.URL, 5*time.Second)
	w := worker.New(q, broker, aiClient)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go w.Run(ctx)

	q.Enqueue(queue.Task{
		ID:          "task-001",
		TraceID:     "abc123",
		Traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		Payload:     "hello",
	})

	var completed []byte
	for i := 0; i < 5; i++ {
		select {
		case msg := <-ch:
			if strings.Contains(string(msg), "task.completed") {
				completed = msg
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for task.completed")
		}
		if completed != nil {
			break
		}
	}

	if completed == nil {
		t.Fatal("never received task.completed event")
	}
	if !strings.Contains(string(completed), "the answer") {
		t.Errorf("expected output in task.completed, got: %s", completed)
	}
}

func TestWorker_PublishesFailedOnRuntimeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	broker := event.NewBroker()
	ch := broker.Subscribe("test")
	defer broker.Unsubscribe("test")

	q := queue.NewQueue(1)
	aiClient := ai.NewClient(srv.URL, 2*time.Second)
	w := worker.New(q, broker, aiClient)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go w.Run(ctx)

	q.Enqueue(queue.Task{
		ID: "task-002", TraceID: "abc123",
		Traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		Payload:     "hello",
	})

	for i := 0; i < 5; i++ {
		select {
		case msg := <-ch:
			if strings.Contains(string(msg), "task.failed") {
				return // pass
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for task.failed")
		}
	}
	t.Fatal("never received task.failed event")
}
```

- [ ] **Step 2: Run tests — verify they fail**

```bash
cd services/api
go test ./internal/worker/... -v
```

Expected: compile error — `worker.New` signature mismatch (no `aiClient` param yet).

- [ ] **Step 3: Rewrite `worker.go`**

Replace the entire file:

```go
package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/runtime-platform/services/api/internal/ai"
	"github.com/runtime-platform/services/api/internal/event"
	"github.com/runtime-platform/services/api/internal/queue"
)

var workerTracer = otel.Tracer("eventdrive-api/worker")

type sseEvent struct {
	EventID             string `json:"event_id"`
	EventType           string `json:"event_type"`
	TraceID             string `json:"trace_id"`
	Traceparent         string `json:"traceparent"`
	TaskID              string `json:"task_id"`
	Timestamp           string `json:"timestamp"`
	Source              string `json:"source"`
	Output              string `json:"output,omitempty"`
	Model               string `json:"model,omitempty"`
	ExecutionStatus     string `json:"execution_status,omitempty"`
	ValidationStatus    string `json:"validation_status,omitempty"`
	InferenceDurationMs int    `json:"inference_duration_ms,omitempty"`
	ErrorReason         string `json:"error_reason,omitempty"`
}

type Worker struct {
	queue    *queue.Queue
	broker   *event.Broker
	aiClient *ai.Client
}

func New(q *queue.Queue, b *event.Broker, aiClient *ai.Client) *Worker {
	return &Worker{queue: q, broker: b, aiClient: aiClient}
}

func (w *Worker) Run(ctx context.Context) {
	slog.Info("worker started")
	for {
		select {
		case <-ctx.Done():
			slog.Info("worker stopped")
			return
		case t := <-w.queue.Receive():
			w.process(ctx, t)
		}
	}
}

func (w *Worker) process(ctx context.Context, t queue.Task) {
	carrier := propagation.MapCarrier{"traceparent": t.Traceparent}
	parentCtx := otel.GetTextMapPropagator().Extract(ctx, carrier)

	_, span := workerTracer.Start(parentCtx, "task.process")
	defer span.End()

	spanCtx := span.SpanContext()
	traceparent := "00-" + spanCtx.TraceID().String() + "-" + spanCtx.SpanID().String() + "-01"
	traceID := spanCtx.TraceID().String()

	w.publish(sseEvent{
		EventType:   "task.processing",
		TraceID:     traceID,
		Traceparent: traceparent,
		TaskID:      t.ID,
		Source:      "worker",
	})

	deadline := time.Now().Add(30 * time.Second)
	inferCtx, cancel := context.WithDeadline(parentCtx, deadline)
	defer cancel()

	resp, err := w.aiClient.Infer(inferCtx, ai.InferRequest{
		TaskID:         t.ID,
		Input:          t.Payload,
		DeadlineUnixMs: deadline.UnixMilli(),
		Traceparent:    traceparent,
	})

	if err != nil {
		reason := "ai_runtime_error"
		if inferCtx.Err() == context.DeadlineExceeded {
			reason = "timeout"
		}
		slog.Error("task failed",
			"task_id", t.ID,
			"trace_id", traceID,
			"error", err,
			"reason", reason,
		)
		w.publish(sseEvent{
			EventType:   "task.failed",
			TraceID:     traceID,
			Traceparent: traceparent,
			TaskID:      t.ID,
			Source:      "worker",
			ErrorReason: reason,
		})
		return
	}

	if resp.ExecutionStatus == "failed" {
		slog.Error("task failed — runtime returned failed status",
			"task_id", t.ID,
			"trace_id", traceID,
			"validation_status", resp.ValidationStatus,
		)
		w.publish(sseEvent{
			EventType:        "task.failed",
			TraceID:          traceID,
			Traceparent:      traceparent,
			TaskID:           t.ID,
			Source:           "worker",
			ErrorReason:      "ai_runtime_failed",
			ExecutionStatus:  resp.ExecutionStatus,
			ValidationStatus: resp.ValidationStatus,
		})
		return
	}

	slog.Info("task completed",
		"task_id", t.ID,
		"trace_id", traceID,
		"model", resp.ExecutionProfile.Model,
		"execution_status", resp.ExecutionStatus,
		"inference_duration_ms", resp.InferenceDurationMs,
	)

	w.publish(sseEvent{
		EventType:           "task.completed",
		TraceID:             traceID,
		Traceparent:         traceparent,
		TaskID:              t.ID,
		Source:              "worker",
		Output:              resp.Output,
		Model:               resp.ExecutionProfile.Model,
		ExecutionStatus:     resp.ExecutionStatus,
		ValidationStatus:    resp.ValidationStatus,
		InferenceDurationMs: resp.InferenceDurationMs,
	})
}

func (w *Worker) publish(ev sseEvent) {
	ev.EventID = uuid.New().String()
	ev.Timestamp = time.Now().UTC().Format(time.RFC3339)
	b, err := json.Marshal(ev)
	if err != nil {
		slog.Error("failed to marshal worker sse event", "error", err)
		return
	}
	w.broker.Publish(b)
}
```

- [ ] **Step 4: Fix `main.go` — update `worker.New` call**

In `services/api/cmd/server/main.go`, the `worker.New` call currently passes `delay`. Replace it:

```go
// Remove: workerDelay and WORKER_DELAY_MS entirely
// Remove: w := worker.New(q, broker, workerDelay)
// Add:

aiRuntimeURL := os.Getenv("AI_RUNTIME_URL")
if aiRuntimeURL == "" {
    aiRuntimeURL = "http://localhost:8001"
}
aiClient := ai.NewClient(aiRuntimeURL, 30*time.Second)
w := worker.New(q, broker, aiClient)
```

Also add the import:
```go
"github.com/runtime-platform/services/api/internal/ai"
```

And remove the `workerDelay`/`WORKER_DELAY_MS` lines from `main.go` — they are no longer needed.

- [ ] **Step 5: Run all tests**

```bash
cd services/api
go test ./... -v
```

Expected: all tests pass. The worker tests use an `httptest.Server` — no real Ollama needed.

- [ ] **Step 6: Commit**

```bash
git add services/api/internal/worker/ services/api/cmd/server/main.go
git commit -m "feat(phase3): wire ai.Client into worker — replace sleep with real inference call"
```

---

## Task 9: End-to-end validation

No code changes. Manual verification that the full trace propagates correctly.

- [ ] **Step 1: Start Ollama (if not already running)**

Ollama should be running on the host. Verify:

```bash
curl -s http://localhost:11434/api/tags | python -m json.tool
```

Expected: JSON with `models` list including `qwen2.5:3b` and `deepseek-coder:6.7b`.

- [ ] **Step 2: Start the FastAPI runtime**

```bash
cd ai-runtime
.venv\Scripts\activate   # Windows
uvicorn main:app --host 0.0.0.0 --port 8001
```

Expected: `Application startup complete.`

- [ ] **Step 3: Start the Go API**

```bash
cd services/api
go run ./cmd/server
```

Expected: `{"level":"INFO","msg":"api server starting","addr":":8080",...}`

- [ ] **Step 4: Start the frontend**

```bash
cd frontend
npm run dev
```

Expected: Next.js dev server on `http://localhost:3000`.

- [ ] **Step 5: Send a general task**

```bash
curl -s -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -d '{"input": "Explain what a distributed trace is in two sentences."}'
```

Expected response: `{"task_id": "...", "trace_id": "...", "traceparent": "..."}`.

Watch Go API stdout — you should see:
1. `task.created` log
2. `task processed` with `model: qwen2.5:3b`

Watch FastAPI stdout — you should see OTEL spans printed for `graph.classify`, `graph.generate`, `graph.validate`, `ollama.generate`.

- [ ] **Step 6: Verify span hierarchy in FastAPI stdout**

In the FastAPI terminal output, look for spans in this order (each is a JSON block):
- `ollama.generate` (deepest child, printed first by BatchSpanProcessor)
- `graph.generate` (parent of ollama.generate)
- `graph.classify`
- `graph.validate`

Each should share the same `traceId` as the one returned by `POST /tasks`.

- [ ] **Step 7: Send a coding task**

```bash
curl -s -X POST http://localhost:8080/tasks \
  -H "Content-Type: application/json" \
  -d '{"input": "Write a Python function that sorts a list using bubble sort."}'
```

In FastAPI stdout, verify: `classify.model = deepseek-coder:6.7b`.

- [ ] **Step 8: Verify frontend**

Open `http://localhost:3000` in a browser. Submit a task via the form. Verify:
- `task.processing` event appears in the event feed
- `task.completed` event appears with `output` and `model` fields visible

- [ ] **Step 9: Test degradation — stop FastAPI mid-run**

Start a long task (use `deepseek-coder:6.7b`), then stop the FastAPI server (`Ctrl+C`) while it's processing.

Expected: Go worker receives connection error, logs `task failed`, publishes `task.failed` to SSE, frontend shows `task.failed` event.

- [ ] **Step 10: Verify `/ready` degraded state**

Stop FastAPI, then:

```bash
curl -s http://localhost:8001/ready
```

Expected: `{"status": "degraded", "ollama": "unreachable"}` — only if Ollama is also stopped. Otherwise test by stopping Ollama.

- [ ] **Step 11: Verify metrics**

```bash
curl -s http://localhost:8001/metrics | grep ai_
```

Expected: `ai_requests_total`, `ai_inference_in_flight`, `ai_inference_duration_seconds` counters/histograms present.

- [ ] **Step 12: Commit final validation marker**

```bash
git commit --allow-empty -m "chore(phase3): end-to-end validation complete"
```

---

## Self-Review Checklist

- [x] Spec coverage: all sections covered — execution model, ownership, bounded execution, degradation semantics, error handling tables, metrics, health/ready separation
- [x] No placeholders: all steps contain actual code or exact commands
- [x] Type consistency: `ai.InferRequest`, `ai.InferResponse`, `ai.ExecutionProfile` defined in Task 7 and used correctly in Task 8; `GraphState`/`ExecutionProfile` defined in Task 1 and used in Tasks 4/5/6
- [x] `worker.New` signature change is reflected in both `worker_test.go` (Task 8) and `main.go` (Task 8 Step 4)
- [x] `WORKER_DELAY_MS` env var removed from `main.go` — no longer needed
- [x] `qwen2.5:3b` and `deepseek-coder:6.7b` are the models used throughout — consistent with spec and available in Ollama
