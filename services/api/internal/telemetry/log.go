package telemetry

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

func Info(ctx context.Context, msg string, args ...any) {
	slog.Info(msg, withTrace(ctx, args)...)
}

func Warn(ctx context.Context, msg string, args ...any) {
	slog.Warn(msg, withTrace(ctx, args)...)
}

func Error(ctx context.Context, msg string, args ...any) {
	slog.Error(msg, withTrace(ctx, args)...)
}

func withTrace(ctx context.Context, args []any) []any {
	sc := trace.SpanFromContext(ctx).SpanContext()
	if !sc.IsValid() {
		return args
	}
	return append([]any{
		"trace_id", sc.TraceID().String(),
		"span_id", sc.SpanID().String(),
	}, args...)
}
