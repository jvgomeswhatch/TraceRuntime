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
from pydantic import BaseModel

from telemetry import init_telemetry, get_tracer, trace_fields
from metrics import (
    ai_requests_total,
    ai_request_duration_seconds,
    ai_validation_status_total,
    ai_failures_total,
)
from graph import build_graph
from state import GraphState

class _JsonFormatter(logging.Formatter):
    def format(self, record: logging.LogRecord) -> str:
        import json
        fields = {
            "time": self.formatTime(record),
            "level": record.levelname,
            "msg": record.getMessage(),
            "name": record.name,
        }
        for key in ("task_id", "trace_id", "span_id", "execution_status",
                    "validation_status", "model", "total_ms", "error"):
            if hasattr(record, key):
                fields[key] = getattr(record, key)
        return json.dumps(fields)

_handler = logging.StreamHandler()
_handler.setFormatter(_JsonFormatter())
logging.basicConfig(level=logging.INFO, handlers=[_handler])
log = logging.getLogger(__name__)

OLLAMA_HOST = os.getenv("OLLAMA_HOST", "http://localhost:11434")
_graph = None
_infer_tracer = None


@asynccontextmanager
async def lifespan(app: FastAPI):
    global _graph, _infer_tracer
    init_telemetry("traceruntime-ai-runtime")
    _infer_tracer = get_tracer("traceruntime-ai-runtime")
    _graph = build_graph()
    log.info("ai-runtime started")
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
        return {"status": "ok", "ollama": "reachable"}
    except Exception:
        return {"status": "degraded", "ollama": "unreachable"}


@app.get("/metrics")
def metrics():
    return Response(generate_latest(), media_type=CONTENT_TYPE_LATEST)


class InferRequestModel(BaseModel):
    task_id: str
    input: str
    deadline_unix_ms: int


@app.post("/infer")
async def infer(body: InferRequestModel, request: Request):
    carrier = dict(request.headers)
    ctx = extract(carrier)
    token = trace.context_api.attach(ctx)

    start = time.monotonic()
    task_id = body.task_id

    try:
        with _infer_tracer.start_as_current_span("ai.infer") as span:
            span.set_attribute("task_id", task_id)
            log.info("infer request received", extra={"task_id": task_id, **trace_fields()})

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

                span.set_attribute("execution_status", execution_status)
                span.set_attribute("task_type", task_type)

                log.info("infer request completed", extra={
                    "task_id": task_id,
                    "execution_status": execution_status,
                    "validation_status": validation_status,
                    "model": profile.get("model"),
                    "total_ms": total_ms,
                    **trace_fields(),
                })

                http_status = 503 if execution_status == "failed" else 200

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
                log.error("infer unhandled error", extra={"task_id": task_id, "error": str(e), **trace_fields()})
                return JSONResponse(status_code=500, content={"error": "internal runtime error"})

    finally:
        trace.context_api.detach(token)
