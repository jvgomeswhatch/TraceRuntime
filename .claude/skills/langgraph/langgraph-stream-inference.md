---
name: langgraph:stream-inference
description: Endpoint FastAPI SSE que faz streaming do grafo LangGraph token a token para o cliente. Inclui gestão de conexão, timeout, propagação de trace_id e formato de evento SSE compatível com o hook useSSE do frontend Next.js deste projeto.
---

# Skill: langgraph:stream-inference

## Input necessário
1. Rota do endpoint (ex: `/infer/stream`)
2. O grafo já existe? (se não, usar `langgraph:new-graph` antes)
3. O frontend já tem o hook SSE? (se não, usar `frontend:sse-hook` depois)

## O que gerar

### `app/api/routes.py` — endpoint SSE
```python
import asyncio
import json
import uuid
import logging
from typing import AsyncIterator

from fastapi import APIRouter, Request
from fastapi.responses import StreamingResponse
from pydantic import BaseModel, Field

from app.graphs.inference import inference_graph

router = APIRouter()
logger = logging.getLogger(__name__)

STREAM_TIMEOUT_S = 130  # ligeiramente maior que timeout Ollama (120s)


class InferRequest(BaseModel):
    input: str = Field(..., min_length=1, max_length=4096)
    trace_id: str = Field(default_factory=lambda: str(uuid.uuid4()))


def sse_event(event: str, data: dict) -> str:
    """Formata evento SSE no padrão deste projeto."""
    return f"event: {event}\ndata: {json.dumps(data)}\n\n"


async def stream_graph(trace_id: str, request_id: str, input_text: str) -> AsyncIterator[str]:
    """
    Generator assíncrono que faz stream do grafo LangGraph via SSE.
    Cada chunk é um evento SSE formatado.
    """
    logger.info("stream.start", extra={"trace_id": trace_id})

    yield sse_event("start", {"trace_id": trace_id, "request_id": request_id})

    initial_state = {
        "trace_id":   trace_id,
        "request_id": request_id,
        "input":      input_text,
        "context":    None,
        "_has_error": False,
    }

    try:
        # astream: yield de updates do grafo a cada nó completado
        async for chunk in inference_graph.astream(initial_state, stream_mode="updates"):
            for node_name, node_output in chunk.items():
                if node_output.get("_has_error"):
                    yield sse_event("error", {
                        "trace_id": trace_id,
                        "node": node_name,
                        "error": node_output.get("error", "unknown error"),
                    })
                    return

                if node_name == "inference" and node_output.get("output"):
                    yield sse_event("token", {
                        "trace_id": trace_id,
                        "content":  node_output["output"],
                        "node":     node_name,
                    })

        yield sse_event("done", {
            "trace_id": trace_id,
            "duration_ms": node_output.get("duration_ms"),
        })

    except asyncio.TimeoutError:
        logger.error("stream.timeout", extra={"trace_id": trace_id})
        yield sse_event("error", {"trace_id": trace_id, "error": "inference timeout"})

    except Exception as e:
        logger.error("stream.error", extra={"trace_id": trace_id, "error": str(e)})
        yield sse_event("error", {"trace_id": trace_id, "error": str(e)})

    finally:
        logger.info("stream.end", extra={"trace_id": trace_id})


@router.post("/infer/stream")
async def infer_stream(body: InferRequest, request: Request):
    request_id = str(uuid.uuid4())

    async def generate():
        try:
            async with asyncio.timeout(STREAM_TIMEOUT_S):
                async for chunk in stream_graph(body.trace_id, request_id, body.input):
                    # Verificar se cliente ainda está conectado
                    if await request.is_disconnected():
                        logger.info("stream.client_disconnected",
                            extra={"trace_id": body.trace_id})
                        break
                    yield chunk
        except asyncio.TimeoutError:
            yield sse_event("error", {"trace_id": body.trace_id, "error": "total timeout"})

    return StreamingResponse(
        generate(),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "X-Accel-Buffering": "no",  # desabilitar buffering no nginx
            "Connection": "keep-alive",
        },
    )


@router.post("/infer")
async def infer_sync(body: InferRequest):
    """
    Endpoint síncrono (sem streaming) para debugging e testes.
    Em produção, preferir /infer/stream.
    """
    request_id = str(uuid.uuid4())
    result = await asyncio.wait_for(
        inference_graph.ainvoke({
            "trace_id":   body.trace_id,
            "request_id": request_id,
            "input":      body.input,
            "context":    None,
            "_has_error": False,
        }),
        timeout=STREAM_TIMEOUT_S,
    )
    if result.get("_has_error"):
        return {"error": result.get("error"), "trace_id": body.trace_id}
    return {"output": result.get("output"), "trace_id": body.trace_id,
            "duration_ms": result.get("duration_ms")}
```

## Formato de eventos SSE (para o frontend)
```
event: start
data: {"trace_id": "abc", "request_id": "xyz"}

event: token
data: {"trace_id": "abc", "content": "partial output...", "node": "inference"}

event: done
data: {"trace_id": "abc", "duration_ms": 3420}

event: error
data: {"trace_id": "abc", "error": "inference timeout"}
```

## Checklist pós-geração
- [ ] `router` registrado no `app/main.py` com `app.include_router(router)`
- [ ] `STREAM_TIMEOUT_S` > `INFERENCE_TIMEOUT_S` do nó Ollama
- [ ] `is_disconnected()` verificado — sem processar após cliente fechar
- [ ] Eventos SSE têm `trace_id` — frontend pode correlacionar com Tempo
- [ ] Teste: `curl -N -X POST localhost:8001/infer/stream -H "Content-Type: application/json" -d '{"input":"test"}'`
- [ ] Frontend hook useSSE conecta em `/infer/stream` e não em `/infer`
