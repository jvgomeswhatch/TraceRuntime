package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"

	"github.com/runtime-platform/services/api/internal/event"
	"github.com/runtime-platform/services/api/internal/metrics"
	"github.com/runtime-platform/services/api/internal/queue"
	"github.com/runtime-platform/services/api/internal/telemetry"
)

var taskTracer = otel.Tracer("traceruntime-api/task")

type taskRequest struct {
	Input string `json:"input"`
}

type taskResponse struct {
	TaskID      string `json:"task_id"`
	TraceID     string `json:"trace_id"`
	Traceparent string `json:"traceparent"`
}

type sseEvent struct {
	EventID     string `json:"event_id"`
	EventType   string `json:"event_type"`
	TraceID     string `json:"trace_id"`
	Traceparent string `json:"traceparent"`
	TaskID      string `json:"task_id"`
	Timestamp   string `json:"timestamp"`
	Source      string `json:"source"`
}

type TaskHandler struct {
	broker *event.Broker
	queue  *queue.Queue
}

func NewTaskHandler(broker *event.Broker, q *queue.Queue) *TaskHandler {
	return &TaskHandler{broker: broker, queue: q}
}

func (h *TaskHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, span := taskTracer.Start(r.Context(), "task.create")
	defer span.End()

	// child span: validate
	_, validateSpan := taskTracer.Start(ctx, "task.validate")
	var req taskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		validateSpan.End()
		jsonError(w, "invalid json", http.StatusBadRequest)
		return
	}
	if req.Input == "" {
		validateSpan.End()
		jsonError(w, "input is required", http.StatusBadRequest)
		return
	}
	validateSpan.End()

	// Extract real trace context from the OTEL span.
	spanCtx := span.SpanContext()
	traceID := spanCtx.TraceID().String()
	traceparent := fmt.Sprintf("00-%s-%s-01",
		spanCtx.TraceID().String(),
		spanCtx.SpanID().String(),
	)

	taskID := uuid.New().String()

	// child span: enqueue
	_, enqueueSpan := taskTracer.Start(ctx, "task.enqueue")
	t := queue.Task{
		ID:          taskID,
		TraceID:     traceID,
		Traceparent: traceparent,
		Payload:     req.Input,
	}
	if !h.queue.Enqueue(t) {
		enqueueSpan.End()
		metrics.QueueRejected.Inc()
		telemetry.Warn(ctx, "task rejected — queue full",
			"task_id", taskID,
			"queue_depth", h.queue.Depth(),
		)
		jsonError(w, "queue_full", http.StatusServiceUnavailable)
		return
	}
	metrics.QueueEnqueued.Inc()
	enqueueSpan.End()

	// Publish task.created SSE event so the frontend sees it immediately.
	_, publishSpan := taskTracer.Start(ctx, "task.publish")
	ev := sseEvent{
		EventID:     uuid.New().String(),
		EventType:   "task.created",
		TraceID:     traceID,
		Traceparent: traceparent,
		TaskID:      taskID,
		Timestamp:   time.Now().UTC().Format(time.RFC3339),
		Source:      "api",
	}
	evBytes, err := json.Marshal(ev)
	if err != nil {
		publishSpan.End()
		telemetry.Error(ctx, "failed to marshal sse event", "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	h.broker.Publish(evBytes)
	publishSpan.End()

	telemetry.Info(ctx, "task created",
		"task_id", taskID,
		"event_type", "task.created",
	)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(taskResponse{
		TaskID:      taskID,
		TraceID:     traceID,
		Traceparent: traceparent,
	})
}

func jsonError(w http.ResponseWriter, msg string, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
