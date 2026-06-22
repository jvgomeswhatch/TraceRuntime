import time
import os
import httpx
from opentelemetry.trace import Status, StatusCode

from metrics import (
    ai_inference_in_flight, ai_inference_duration_seconds, ai_output_chars,
    llm_prompt_tokens_total, llm_completion_tokens_total, llm_tokens_total,
    llm_output_tokens, llm_inference_tokens_per_second,
)
from telemetry import get_tracer

OLLAMA_HOST = os.getenv("OLLAMA_HOST", "http://localhost:11434")
MAX_OUTPUT_CHARS = int(os.getenv("MAX_OUTPUT_CHARS", "8000"))

_tracer = get_tracer("traceruntime-ai-runtime/ollama")


def _safe_int(val) -> int:
    if isinstance(val, int) and val >= 0:
        return val
    return 0


def generate(model: str, prompt: str, timeout_ms: int, task_type: str) -> tuple[str, bool, dict]:
    """
    Call Ollama /api/generate. Returns (output, truncated, token_info).
    token_info contains prompt_tokens, completion_tokens, tokens_per_second.
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
            data = resp.json()
            raw = data.get("response", "")
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

        prompt_tokens = _safe_int(data.get("prompt_eval_count"))
        completion_tokens = _safe_int(data.get("eval_count"))
        eval_duration_ns = _safe_int(data.get("eval_duration"))
        total_tokens = prompt_tokens + completion_tokens

        tokens_per_second = 0.0
        if eval_duration_ns > 0 and completion_tokens > 0:
            tokens_per_second = completion_tokens / (eval_duration_ns / 1e9)

        labels = {"model": model, "task_type": task_type}
        llm_prompt_tokens_total.labels(**labels).inc(prompt_tokens)
        llm_completion_tokens_total.labels(**labels).inc(completion_tokens)
        llm_tokens_total.labels(**labels).inc(total_tokens)
        llm_output_tokens.labels(**labels).observe(completion_tokens)
        if tokens_per_second > 0:
            llm_inference_tokens_per_second.labels(**labels).observe(tokens_per_second)

        span.set_attribute("llm.prompt_tokens", prompt_tokens)
        span.set_attribute("llm.completion_tokens", completion_tokens)
        span.set_attribute("llm.total_tokens", total_tokens)
        span.set_attribute("llm.tokens_per_second", round(tokens_per_second, 2))

        token_info = {
            "prompt_tokens": prompt_tokens,
            "completion_tokens": completion_tokens,
            "tokens_per_second": round(tokens_per_second, 2),
        }

        return output, truncated, token_info
