---
name: python-ai:langgraph-pipeline
description: Pipeline LangGraph completo com nó Ollama, streaming, propagação de trace_id via OTEL, timeout explícito e endpoint FastAPI SSE. Versão pronta para produção local — integra validate → ollama → output com error handling em cada nó.
---

# Skill: python-ai:langgraph-pipeline

## Input necessário
1. Modelo Ollama a usar (`mistral:7b-instruct-q4_K_M` ou `phi3:mini`)
2. Endpoint do serviço vai receber trace_id via header `traceparent`? Sim/Não
3. Precisar de contexto adicional no estado (ex: histórico, RAG)? Sim/Não

## O que gerar

### Estrutura de diretórios completa
```
services/ai-runtime/
├── app/
│   ├── main.py
│   ├── otelsetup.py          # usar skill otel:instrument-python
│   ├── graphs/
│   │   └── inference.py      # StateGraph compilado
│   ├── nodes/
│   │   ├── validate.py
│   │   ├── ollama.py         # nó de inferência
│   │   └── output.py
│   └── api/
│       └── routes.py         # endpoints FastAPI
├── requirements.txt
└── Dockerfile
```

### `app/graphs/inference.py` — grafo completo
```python
from typing import Optional
from typing_extensions import TypedDict
from langgraph.graph import StateGraph, END

from app.nodes.validate import validate_node
from app.nodes.ollama import ollama_node
from app.nodes.output import format_output_node


class InferenceState(TypedDict):
    trace_id:    str
    request_id:  str
    input:       str
    context:     Optional[str]
    output:      Optional[str]
    error:       Optional[str]
    duration_ms: Optional[int]
    _has_error:  bool


def _route(state: InferenceState) -> str:
    return "error" if state.get("_has_error") or state.get("error") else "continue"


def build_graph():
    g = StateGraph(InferenceState)

    g.add_node("validate",  validate_node)
    g.add_node("inference", ollama_node)
    g.add_node("output",    format_output_node)

    g.set_entry_point("validate")
    g.add_conditional_edges("validate",  _route, {"continue": "inference", "error": END})
    g.add_conditional_edges("inference", _route, {"continue": "output",    "error": END})
    g.add_edge("output", END)

    return g.compile()


# Compilar uma vez na inicialização do módulo
inference_graph = build_graph()
```

### `app/nodes/ollama.py` — nó com timeout e trace
```python
import time
import logging
from opentelemetry import trace

from langchain_community.llms import Ollama

logger = logging.getLogger(__name__)
tracer = trace.get_tracer("ai-runtime")

_OLLAMA_URL     = "http://ollama:11434"
_MODEL          = "mistral:7b-instruct-q4_K_M"
_TIMEOUT        = 120
_MAX_TOKENS     = 512

_llm: Ollama | None = None

def _get_llm() -> Ollama:
    global _llm
    if _llm is None:
        _llm = Ollama(
            base_url=_OLLAMA_URL,
            model=_MODEL,
            timeout=_TIMEOUT,
            temperature=0.1,
            num_predict=_MAX_TOKENS,
        )
    return _llm


def ollama_node(state: dict) -> dict:
    trace_id = state.get("trace_id", "unknown")

    with tracer.start_as_current_span("ollama.inference") as span:
        span.set_attribute("trace_id", trace_id)
        span.set_attribute("model", _MODEL)
        span.set_attribute("input_length", len(state.get("input", "")))

        start = time.monotonic()
        try:
            output = _get_llm().invoke(state["input"])
            duration_ms = int((time.monotonic() - start) * 1000)

            span.set_attribute("duration_ms", duration_ms)
            span.set_attribute("output_length", len(output))

            logger.info("ollama.done",
                extra={"trace_id": trace_id, "duration_ms": duration_ms})

            return {**state, "output": output, "duration_ms": duration_ms,
                    "_has_error": False}

        except Exception as e:
            duration_ms = int((time.monotonic() - start) * 1000)
            span.record_exception(e)

            logger.error("ollama.error",
                extra={"trace_id": trace_id, "error": str(e),
                       "duration_ms": duration_ms})

            return {**state, "error": str(e), "_has_error": True}
```

### `app/api/routes.py` — endpoints completos
```python
import asyncio
import json
import uuid
import logging

from fastapi import APIRouter, Request
from fastapi.responses import StreamingResponse
from pydantic import BaseModel, Field
from opentelemetry.propagate import extract

from app.graphs.inference import inference_graph, InferenceState

router = APIRouter()
logger = logging.getLogger(__name__)

TIMEOUT_S = 130


class InferRequest(BaseModel):
    input:    str  = Field(..., min_length=1, max_length=4096)
    trace_id: str  = Field(default_factory=lambda: str(uuid.uuid4()))


def sse(event: str, data: dict) -> str:
    return f"event: {event}\ndata: {json.dumps(data)}\n\n"


@router.post("/infer/stream")
async def infer_stream(body: InferRequest, request: Request):
    # Propagar trace context do chamador Go
    ctx = extract(dict(request.headers))

    async def generate():
        yield sse("start", {"trace_id": body.trace_id})
        try:
            async with asyncio.timeout(TIMEOUT_S):
                async for chunk in inference_graph.astream(
                    InferenceState(
                        trace_id=body.trace_id,
                        request_id=str(uuid.uuid4()),
                        input=body.input,
                        context=None,
                        output=None,
                        error=None,
                        duration_ms=None,
                        _has_error=False,
                    ),
                    stream_mode="updates",
                ):
                    if await request.is_disconnected():
                        return
                    for node, out in chunk.items():
                        if out.get("_has_error"):
                            yield sse("error", {"trace_id": body.trace_id, "error": out.get("error")})
                            return
                        if node == "inference" and out.get("output"):
                            yield sse("token", {"trace_id": body.trace_id, "content": out["output"]})

            yield sse("done", {"trace_id": body.trace_id})
        except asyncio.TimeoutError:
            yield sse("error", {"trace_id": body.trace_id, "error": "timeout"})
        except Exception as e:
            yield sse("error", {"trace_id": body.trace_id, "error": str(e)})

    return StreamingResponse(generate(), media_type="text/event-stream",
        headers={"Cache-Control": "no-cache", "X-Accel-Buffering": "no"})


@router.get("/health")
async def health():
    return {"status": "ok"}
```

### `app/main.py`
```python
import logging
from contextlib import asynccontextmanager

import httpx
from fastapi import FastAPI

from app.api.routes import router
from app.otelsetup import init_tracer
from app.nodes.ollama import _MODEL, _OLLAMA_URL

logging.basicConfig(level=logging.INFO,
    format='{"time":"%(asctime)s","level":"%(levelname)s","msg":"%(message)s"}')


@asynccontextmanager
async def lifespan(app: FastAPI):
    init_tracer("ai-runtime", app)

    # Verificar Ollama antes de aceitar requests
    async with httpx.AsyncClient(timeout=5) as client:
        r = await client.get(f"{_OLLAMA_URL}/api/tags")
        models = [m["name"] for m in r.json().get("models", [])]
        if _MODEL not in models:
            raise RuntimeError(f"Model {_MODEL} not loaded in Ollama. Run: ollama pull {_MODEL}")

    yield


app = FastAPI(title="EventDrive AI Runtime", lifespan=lifespan)
app.include_router(router)
```

### `requirements.txt`
```
fastapi==0.111.0
uvicorn[standard]==0.29.0
langchain-community==0.2.0
langgraph==0.2.0
httpx==0.27.0
opentelemetry-sdk==1.24.0
opentelemetry-exporter-otlp-proto-grpc==1.24.0
opentelemetry-instrumentation-fastapi==0.45b0
```

## Checklist pós-geração
- [ ] `inference_graph = build_graph()` compilado na inicialização do módulo — não por request
- [ ] `ollama pull mistral:7b-instruct-q4_K_M` executado antes de subir o serviço
- [ ] `lifespan` verifica modelo carregado — falha rápida no startup
- [ ] `TIMEOUT_S = 130 > _TIMEOUT = 120` — sempre maior que timeout Ollama
- [ ] `extract(dict(request.headers))` propaga trace do Go service
- [ ] `docker compose up ai-runtime` e `curl http://localhost:8001/health` retorna 200
- [ ] Teste streaming: `curl -N -X POST localhost:8001/infer/stream -H "Content-Type: application/json" -d '{"input":"Hello"}'`
