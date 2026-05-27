package telemetry

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestWithTrace_ValidSpan(t *testing.T) {
	// TracerProvider real com span recorder — sem noop
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sdktrace.NewSimpleSpanProcessor(exp)))
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "test-span")
	defer span.End()

	sc := span.SpanContext()
	if !sc.IsValid() {
		t.Fatal("span context must be valid")
	}

	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	slog.SetDefault(logger)

	Info(ctx, "test message", "key", "value")

	out := buf.String()
	if !strings.Contains(out, "trace_id="+sc.TraceID().String()) {
		t.Errorf("expected trace_id=%s in log, got: %s", sc.TraceID().String(), out)
	}
	if !strings.Contains(out, "span_id="+sc.SpanID().String()) {
		t.Errorf("expected span_id=%s in log, got: %s", sc.SpanID().String(), out)
	}
	if !strings.Contains(out, "key=value") {
		t.Errorf("expected key=value in log, got: %s", out)
	}
}

func TestWithTrace_NoSpan(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	slog.SetDefault(logger)

	Info(context.Background(), "no span message", "key", "value")

	out := buf.String()
	if strings.Contains(out, "trace_id") {
		t.Errorf("expected no trace_id when no span active, got: %s", out)
	}
	if strings.Contains(out, "span_id") {
		t.Errorf("expected no span_id when no span active, got: %s", out)
	}
	if !strings.Contains(out, "key=value") {
		t.Errorf("expected key=value in log, got: %s", out)
	}
}
