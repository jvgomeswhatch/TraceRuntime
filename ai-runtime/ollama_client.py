import time
import os
import httpx
from opentelemetry.trace import Status, StatusCode

from metrics import ai_inference_in_flight, ai_inference_duration_seconds, ai_output_chars
from telemetry import get_tracer

OLLAMA_HOST = os.getenv("OLLAMA_HOST", "http://localhost:11434")
MAX_OUTPUT_CHARS = int(os.getenv("MAX_OUTPUT_CHARS", "8000"))

_tracer = get_tracer("traceruntime-ai-runtime/ollama")


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
                json={"model": model, "prompt": prompt, "stream": False, "keep_alive": "30m"},
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
