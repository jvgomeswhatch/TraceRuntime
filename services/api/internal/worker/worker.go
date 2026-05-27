package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/runtime-platform/services/api/internal/ai"
	"github.com/runtime-platform/services/api/internal/event"
	"github.com/runtime-platform/services/api/internal/metrics"
	"github.com/runtime-platform/services/api/internal/queue"
	"github.com/runtime-platform/services/api/internal/telemetry"
)

var workerTracer = otel.Tracer("traceruntime-api/worker")

type sseEvent struct {
	EventID             string `json:"event_id"`
	EventType           string `json:"event_type"`
	TraceID             string `json:"trace_id"`
	Traceparent         string `json:"traceparent"`
	TaskID              string `json:"task_id"`
	Timestamp           string `json:"timestamp"`
	Source              string `json:"source"`
	Output              string `json:"output,omitempty"`
	Model               string `json:"model,omitempty"`
	ExecutionStatus     string `json:"execution_status,omitempty"`
	ValidationStatus    string `json:"validation_status,omitempty"`
	InferenceDurationMs int    `json:"inference_duration_ms,omitempty"`
	ErrorReason         string `json:"error_reason,omitempty"`
}

type Worker struct {
	queue    *queue.Queue
	broker   *event.Broker
	aiClient *ai.Client
}

func New(q *queue.Queue, b *event.Broker, aiClient *ai.Client) *Worker {
	return &Worker{queue: q, broker: b, aiClient: aiClient}
}

func (w *Worker) Run(ctx context.Context) {
	slog.Info("worker started")
	for {
		select {
		case <-ctx.Done():
			slog.Info("worker stopped")
			return
		case t := <-w.queue.Receive():
			metrics.QueueProcessed.Inc()
			metrics.WorkerActiveTasks.Inc()
			w.process(ctx, t)
			metrics.WorkerActiveTasks.Dec()
		}
	}
}

func (w *Worker) process(ctx context.Context, t queue.Task) {
	carrier := propagation.MapCarrier{"traceparent": t.Traceparent}
	parentCtx := otel.GetTextMapPropagator().Extract(ctx, carrier)

	ctx, span := workerTracer.Start(parentCtx, "task.process")
	defer span.End()

	spanCtx := span.SpanContext()
	traceparent := "00-" + spanCtx.TraceID().String() + "-" + spanCtx.SpanID().String() + "-01"
	traceID := spanCtx.TraceID().String()

	w.publish(sseEvent{
		EventType:   "task.processing",
		TraceID:     traceID,
		Traceparent: traceparent,
		TaskID:      t.ID,
		Source:      "worker",
	})

	deadline := time.Now().Add(120 * time.Second)
	inferCtx, cancel := context.WithDeadline(parentCtx, deadline)
	defer cancel()

	resp, err := w.aiClient.Infer(inferCtx, ai.InferRequest{
		TaskID:         t.ID,
		Input:          t.Payload,
		DeadlineUnixMs: deadline.UnixMilli(),
		Traceparent:    traceparent,
	})

	if err != nil {
		reason := "ai_runtime_error"
		if inferCtx.Err() == context.DeadlineExceeded {
			reason = "timeout"
		}
		telemetry.Error(ctx, "task failed",
			"task_id", t.ID,
			"error", err,
			"reason", reason,
		)
		w.publish(sseEvent{
			EventType:   "task.failed",
			TraceID:     traceID,
			Traceparent: traceparent,
			TaskID:      t.ID,
			Source:      "worker",
			ErrorReason: reason,
		})
		return
	}

	if resp.ExecutionStatus == "failed" {
		telemetry.Error(ctx, "task failed — runtime returned failed status",
			"task_id", t.ID,
			"validation_status", resp.ValidationStatus,
		)
		w.publish(sseEvent{
			EventType:        "task.failed",
			TraceID:          traceID,
			Traceparent:      traceparent,
			TaskID:           t.ID,
			Source:           "worker",
			ErrorReason:      "ai_runtime_failed",
			ExecutionStatus:  resp.ExecutionStatus,
			ValidationStatus: resp.ValidationStatus,
		})
		return
	}

	telemetry.Info(ctx, "task completed",
		"task_id", t.ID,
		"model", resp.ExecutionProfile.Model,
		"execution_status", resp.ExecutionStatus,
		"inference_duration_ms", resp.InferenceDurationMs,
	)

	w.publish(sseEvent{
		EventType:           "task.completed",
		TraceID:             traceID,
		Traceparent:         traceparent,
		TaskID:              t.ID,
		Source:              "worker",
		Output:              resp.Output,
		Model:               resp.ExecutionProfile.Model,
		ExecutionStatus:     resp.ExecutionStatus,
		ValidationStatus:    resp.ValidationStatus,
		InferenceDurationMs: resp.InferenceDurationMs,
	})
}

func (w *Worker) publish(ev sseEvent) {
	ev.EventID = uuid.New().String()
	ev.Timestamp = time.Now().UTC().Format(time.RFC3339)
	b, err := json.Marshal(ev)
	if err != nil {
		slog.Error("failed to marshal worker sse event", "error", err)
		return
	}
	w.broker.Publish(b)
}
