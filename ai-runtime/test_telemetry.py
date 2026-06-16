from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export.in_memory_span_exporter import InMemorySpanExporter
from opentelemetry.sdk.trace.export import SimpleSpanProcessor

from telemetry import trace_fields


def _make_provider():
    exp = InMemorySpanExporter()
    provider = TracerProvider()
    provider.add_span_processor(SimpleSpanProcessor(exp))
    return provider


def test_trace_fields_with_active_span():
    provider = _make_provider()
    tracer = provider.get_tracer("test")

    with tracer.start_as_current_span("test-span") as span:
        fields = trace_fields()

    sc = span.get_span_context()
    assert "trace_id" in fields
    assert "span_id" in fields
    assert fields["trace_id"] == format(sc.trace_id, "032x")
    assert fields["span_id"] == format(sc.span_id, "016x")
    assert len(fields["trace_id"]) == 32
    assert len(fields["span_id"]) == 16


def test_trace_fields_without_span():
    # fora de qualquer span — deve retornar dict vazio
    fields = trace_fields()
    assert fields == {}


def test_trace_fields_inside_exception_handler():
    """log.error dentro do except ainda está dentro do with span — trace_fields deve ser válido."""
    provider = _make_provider()
    tracer = provider.get_tracer("test")

    captured = {}

    with tracer.start_as_current_span("test-span") as span:
        try:
            raise ValueError("boom")
        except ValueError:
            captured = trace_fields()

    sc = span.get_span_context()
    assert captured != {}, "trace_fields deve retornar campos dentro do except que está no with span"
    assert captured["trace_id"] == format(sc.trace_id, "032x")
    assert captured["span_id"] == format(sc.span_id, "016x")
