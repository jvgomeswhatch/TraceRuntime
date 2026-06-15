---
name: python-ai:new-inference-endpoint
description: Cria endpoint FastAPI completo para inferência com Ollama — streaming, validação Pydantic, métricas de latência e unload automático. Use ao adicionar qualquer capacidade de IA ao projeto.
---

# Skill: python-ai:new-inference-endpoint

## Input necessário
1. Nome do endpoint (ex: `classify-order`, `analyze-fraud`)
2. Campos do request
3. Modelo Ollama a usar (default: `llama3.2:1b`)
4. Comportamento esperado: classificação, extração, geração livre?

## O que gerar

### `app/routers/{endpoint_name}.py`
```python
from fastapi import APIRouter
from fastapi.responses import StreamingResponse
from pydantic import BaseModel, Field
import httpx
import json
import time

router = APIRouter(prefix="/{endpoint_name}", tags=["{endpoint_name}"])

class InferRequest(BaseModel):
    # Adaptar campos ao domínio
    text: str = Field(..., max_length=4096)
    model: str = Field(default="llama3.2:1b")

class InferResponse(BaseModel):
    result: str
    latency_ms: float
    tokens_per_second: float | None = None

PROMPT_TEMPLATE = """
Você é um assistente especializado em {domínio}.

Input: {text}

Responda de forma concisa e estruturada.
""".strip()

@router.post("/stream")
async def infer_stream(request: InferRequest) -> StreamingResponse:
    prompt = PROMPT_TEMPLATE.format(text=request.text)
    return StreamingResponse(
        _stream_ollama(prompt, request.model),
        media_type="text/event-stream",
        headers={"Cache-Control": "no-cache"},
    )

@router.post("/", response_model=InferResponse)
async def infer(request: InferRequest) -> InferResponse:
    prompt = PROMPT_TEMPLATE.format(text=request.text)
    start = time.perf_counter()
    result = ""
    
    async for chunk in _stream_ollama(prompt, request.model):
        result += chunk
    
    latency_ms = (time.perf_counter() - start) * 1000
    return InferResponse(result=result.strip(), latency_ms=latency_ms)

async def _stream_ollama(prompt: str, model: str):
    async with httpx.AsyncClient(timeout=60.0) as client:
        async with client.stream(
            "POST",
            "http://ollama:11434/api/generate",
            json={
                "model": model,
                "prompt": prompt,
                "stream": True,
                "options": {
                    "num_ctx": 2048,
                    "num_predict": 256,  # conservador — memória restrita
                    "temperature": 0.1,   # determinístico para classificação
                },
            },
        ) as response:
            async for line in response.aiter_lines():
                if not line:
                    continue
                chunk = json.loads(line)
                yield chunk["response"]
                if chunk.get("done"):
                    break
```

### `app/main.py` (registrar router)
```python
from fastapi import FastAPI
from app.routers import {endpoint_name}

app = FastAPI(title="AI Service", docs_url=None)  # sem Swagger em prod

app.include_router({endpoint_name}.router)

@app.get("/health")
async def health():
    return {"status": "ok"}

@app.get("/metrics")
async def metrics():
    # Expor métricas simples — Prometheus pode scrape aqui
    return {"active_requests": 0}  # implementar com contextvars
```

## Checklist pós-geração
- [ ] Modelo correto definido (max 3B para este hardware)
- [ ] `num_predict` conservador (256-512 tokens)
- [ ] Endpoint `/health` funcionando
- [ ] `mem_limit: 512M` no container python-ai
- [ ] Ollama tem o modelo baixado (`ollama pull {model}`)
- [ ] Teste com `curl -N http://localhost:8000/{endpoint}/stream`
