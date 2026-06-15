---
name: langgraph:ollama-node
description: Nó LangGraph de inferência Ollama com streaming controlado, timeout explícito, controle de memória e propagação de trace_id. Modelo fixo em mistral:7b-instruct-q4_K_M ou phi3:mini. Nunca acumula resposta completa em RAM.
---

# Skill: langgraph:ollama-node

## Input necessário
1. Nome do modelo Ollama (default: `mistral:7b-instruct-q4_K_M`)
2. Timeout desejado em segundos (default: 120s)
3. O nó deve fazer streaming via SSE ou retornar string completa?

## O que gerar

### `app/nodes/ollama.py`
```python
"""
Nó Ollama para LangGraph.

Requisitos de memória:
- Modelo mistral:7b-instruct-q4_K_M: ~4.1GB RAM quando carregado
- Manter apenas 1 instância Ollama ativa (garantido pelo Ollama server)
- Streaming: não acumular tokens além do necessário
"""
import time
import logging
from typing import Iterator

import httpx
from langchain_community.llms import Ollama

logger = logging.getLogger(__name__)

OLLAMA_BASE_URL = "http://ollama:11434"
OLLAMA_MODEL    = "mistral:7b-instruct-q4_K_M"
INFERENCE_TIMEOUT_S = 120  # nunca omitir — Ollama pode travar silenciosamente

# Singleton do cliente — não recriar por request
_llm: Ollama | None = None

def _get_llm() -> Ollama:
    global _llm
    if _llm is None:
        _llm = Ollama(
            base_url=OLLAMA_BASE_URL,
            model=OLLAMA_MODEL,
            timeout=INFERENCE_TIMEOUT_S,
            temperature=0.1,     # determinismo > criatividade em runtime operacional
            num_predict=512,     # limitar output — sem resposta infinita
        )
    return _llm


def ollama_node(state: dict) -> dict:
    """
    Nó síncrono para uso em StateGraph.compile().
    Para streaming SSE, use ollama_stream_node.
    """
    trace_id = state.get("trace_id", "unknown")
    inp      = state.get("input", "")

    logger.info("ollama_node.start",
        extra={"trace_id": trace_id, "input_len": len(inp)})

    start = time.monotonic()

    try:
        llm = _get_llm()
        output = llm.invoke(inp)
        duration_ms = int((time.monotonic() - start) * 1000)

        logger.info("ollama_node.done",
            extra={"trace_id": trace_id, "duration_ms": duration_ms,
                   "output_len": len(output)})

        return {**state, "output": output, "duration_ms": duration_ms,
                "_has_error": False}

    except Exception as e:
        duration_ms = int((time.monotonic() - start) * 1000)
        logger.error("ollama_node.error",
            extra={"trace_id": trace_id, "error": str(e),
                   "duration_ms": duration_ms})
        return {**state, "error": str(e), "_has_error": True}


def ollama_stream_node(state: dict) -> Iterator[dict]:
    """
    Versão streaming para uso em grafos com stream=True.
    Yield de estado parcial a cada chunk — não acumula em memória.
    """
    trace_id = state.get("trace_id", "unknown")
    inp      = state.get("input", "")

    logger.info("ollama_stream_node.start", extra={"trace_id": trace_id})

    start  = time.monotonic()
    chunks = []

    try:
        llm = _get_llm()
        for chunk in llm.stream(inp):
            chunks.append(chunk)
            # yield estado parcial — frontend recebe via SSE
            yield {**state, "output": "".join(chunks), "_has_error": False}

        duration_ms = int((time.monotonic() - start) * 1000)
        logger.info("ollama_stream_node.done",
            extra={"trace_id": trace_id, "duration_ms": duration_ms,
                   "total_chunks": len(chunks)})

        yield {**state, "output": "".join(chunks),
               "duration_ms": duration_ms, "_has_error": False}

    except Exception as e:
        logger.error("ollama_stream_node.error",
            extra={"trace_id": trace_id, "error": str(e)})
        yield {**state, "error": str(e), "_has_error": True}


async def check_ollama_health() -> bool:
    """
    Verificar se Ollama está up e modelo carregado.
    Chamar no lifespan do FastAPI — não aceitar requests sem isso.
    """
    try:
        async with httpx.AsyncClient(timeout=5) as client:
            r = await client.get(f"{OLLAMA_BASE_URL}/api/tags")
            if r.status_code != 200:
                return False
            models = [m["name"] for m in r.json().get("models", [])]
            if OLLAMA_MODEL not in models:
                logger.warning("ollama.model_not_loaded",
                    extra={"model": OLLAMA_MODEL, "available": models})
                return False
            return True
    except Exception as e:
        logger.error("ollama.health_check_failed", extra={"error": str(e)})
        return False
```

### Registrar health check no lifespan (FastAPI)
```python
@asynccontextmanager
async def lifespan(app: FastAPI):
    if not await check_ollama_health():
        logger.error("ollama.not_ready — recusando startup")
        raise RuntimeError("Ollama not available or model not loaded")
    logger.info("ollama.ready", extra={"model": OLLAMA_MODEL})
    yield
```

## Limites de memória por modelo (referência)
| Modelo | VRAM/RAM | q4_K_M aprox |
|---|---|---|
| mistral:7b | ~14GB fp16 | ~4.1GB |
| phi3:mini (3.8B) | ~7.5GB fp16 | ~2.3GB |
| llama3:8b | ~16GB fp16 | ~4.9GB |

Usar apenas `mistral:7b-instruct-q4_K_M` ou `phi3:mini` — nunca modelos maiores.

## Checklist pós-geração
- [ ] `OLLAMA_BASE_URL` via env var no docker-compose
- [ ] `check_ollama_health()` no lifespan — falha rápida se modelo não disponível
- [ ] `num_predict=512` — sem geração infinita
- [ ] `timeout=120` — nunca omitir
- [ ] `_llm` singleton — `_get_llm()` chamado uma vez
- [ ] Teste manual: `curl -X POST localhost:8001/infer -d '{"trace_id":"test","input":"Hello"}'`
