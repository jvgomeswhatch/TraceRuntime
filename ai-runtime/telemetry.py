import os

from opentelemetry import trace
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.sdk.resources import Resource
from opentelemetry.propagate import set_global_textmap
from opentelemetry.trace.propagation.tracecontext import TraceContextTextMapPropagator
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter


def init_telemetry(service_name: str = "traceruntime-ai-runtime") -> TracerProvider:
    endpoint = os.getenv("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4317")
    exporter = OTLPSpanExporter(endpoint=endpoint, insecure=True)

    resource = Resource.create({"service.name": service_name, "service.version": "0.1.0"})
    provider = TracerProvider(resource=resource)
    provider.add_span_processor(BatchSpanProcessor(exporter))
    trace.set_tracer_provider(provider)
    set_global_textmap(TraceContextTextMapPropagator())
    return provider


def get_tracer(name: str):
    return trace.get_tracer(name)


def trace_fields() -> dict:
    """Retorna trace_id e span_id do span ativo, ou dict vazio se não houver span."""
    span = trace.get_current_span()
    if not span.is_recording():
        return {}
    sc = span.get_span_context()
    return {
        "trace_id": format(sc.trace_id, "032x"),
        "span_id": format(sc.span_id, "016x"),
    }
