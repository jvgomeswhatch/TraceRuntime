import os
import time
import logging
import httpx
from opentelemetry.trace import Status, StatusCode

from langgraph.graph import StateGraph, END
from state import GraphState, ExecutionProfile
from telemetry import get_tracer, trace_fields
import ollama_client

log = logging.getLogger(__name__)
_tracer = get_tracer("traceruntime-ai-runtime/graph")

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

        log.info("classify node", extra={"task_id": state["task_id"], "task_type": task_type, "model": model, **trace_fields()})
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
                **trace_fields(),
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
            log.error("generate timeout", extra={"task_id": state["task_id"], "elapsed_ms": elapsed_ms, **trace_fields()})
            return {
                "output": "",
                "execution_status": "failed",
                "validation_status": "",
                "inference_duration_ms": elapsed_ms,
            }

        except httpx.HTTPError as e:
            span.set_status(Status(StatusCode.ERROR))
            elapsed_ms = int((time.monotonic() - start) * 1000)
            log.error("generate provider error", extra={"task_id": state["task_id"], "error": str(e), **trace_fields()})
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
            **trace_fields(),
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
