---
name: otel:instrument-python
description: Adiciona SDK OpenTelemetry a um serviço Python/FastAPI — TracerProvider, middleware automático FastAPI, spans manuais em nós LangGraph, propagação W3C via headers HTTP e SQS. Exporta para OTEL Collector via gRPC.
---

# Skill: otel:instrument-python

## Input necessário
1. Nome do serviço (ex: `ai-runtime`)
2. O serviço usa FastAPI? LangGraph? SQS client (boto3)?
3. Endpoint do Collector (default `otel-collector:4317`)

## O que gerar

### 1. `app/otelsetup.py`
```python
import os
from opentelemetry import trace
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.instrumentation.fastapi import FastAPIInstrumentor
from opentelemetry.propagate import set_global_textmap
from opentelemetry.propagators.b3 import B3MultiFormat
from opentelemetry.sdk.resources import SERVICE_NAME, Resource
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator

OTEL_ENDPOINT = os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "otel-collector:4317")

def init_tracer(service_name: str, app=None) -> trace.Tracer:
    """
    Inicializa TracerProvider e opcionalmente instrumenta FastAPI app.
    Chamar antes de registrar routes.
    """
    resource = Resource(attributes={SERVICE_NAME: service_name})

    exporter = OTLPSpanExporter(
        endpoint=OTEL_ENDPOINT,
        insecure=True,
    )

    provider = TracerProvider(resource=resource)
    provider.add_span_processor(BatchSpanProcessor(exporter))
    trace.set_tracer_provider(provider)

    # W3C TraceContext — compatível com Go services
    set_global_textmap(TraceContextTextMapPropagator())

    if app is not None:
        FastAPIInstrumentor.instrument_app(app)

    return trace.get_tracer(service_name)
```

### 2. Usar no `app/main.py`
```python
from contextlib import asynccontextmanager
from fastapi import FastAPI
from app.otelsetup import init_tracer

tracer = None

@asynccontextmanager
async def lifespan(app: FastAPI):
    global tracer
    tracer = init_tracer("ai-runtime", app)
    yield
    # cleanup se necessário

app = FastAPI(lifespan=lifespan)
```

### 3. Span manual em nó LangGraph
```python
from opentelemetry import trace

tracer = trace.get_tracer("ai-runtime")

def ollama_node(state: InferenceState) -> InferenceState:
    trace_id = state["trace_id"]

    with tracer.start_as_current_span("ollama.inference") as span:
        span.set_attribute("trace_id", trace_id)
        span.set_attribute("model", "mistral:7b-instruct-q4_K_M")

        try:
            output = run_inference(state["input"])
            span.set_attribute("output_length", len(output))
            return {**state, "output": output}
        except Exception as e:
            span.set_status(trace.StatusCode.ERROR, str(e))
            span.record_exception(e)
            return {**state, "error": str(e)}
```

### 4. Extrair trace context de header HTTP (para propagação)
```python
from opentelemetry.propagate import extract

@app.post("/infer")
async def infer(request: Request, body: InferRequest):
    # Extrair contexto do header traceparent enviado pelo Go service
    ctx = extract(dict(request.headers))
    with tracer.start_as_current_span("api.infer", context=ctx) as span:
        span.set_attribute("trace_id", body.trace_id)
        # ...
```

## Dependências (requirements.txt)
```
opentelemetry-sdk==1.24.0
opentelemetry-exporter-otlp-proto-grpc==1.24.0
opentelemetry-instrumentation-fastapi==0.45b0
opentelemetry-propagator-b3==1.24.0
```

## Checklist pós-geração
- [ ] `OTEL_EXPORTER_OTLP_ENDPOINT` no docker-compose do ai-runtime
- [ ] `init_tracer` chamado no lifespan antes de qualquer request
- [ ] Spans em nós críticos do LangGraph (inference, validation)
- [ ] `traceparent` header extraído corretamente nos endpoints
- [ ] Trace linkado ao trace do serviço Go chamador no Tempo
- [ ] Container reiniciado e health check OK após instrumentação
