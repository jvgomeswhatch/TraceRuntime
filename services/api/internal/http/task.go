package http

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	sqssdk "github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"

	"github.com/runtime-platform/services/api/internal/event"
	"github.com/runtime-platform/services/api/internal/metrics"
	"github.com/runtime-platform/services/api/internal/queue"
	"github.com/runtime-platform/services/api/internal/telemetry"
)

var taskTracer = otel.Tracer("traceruntime-api/task")

// Publisher enqueues tasks — implemented by QueuePublisher (inmemory) or SQSPublisher.
type Publisher interface {
	Publish(ctx context.Context, taskID, traceID, traceparent, payload string) (bool, error)
}

type SQSPublisher struct {
	client   *sqssdk.Client
	queueURL string
}

func NewSQSPublisher(client *sqssdk.Client, queueURL string) *SQSPublisher {
	return &SQSPublisher{client: client, queueURL: queueURL}
}

func (p *SQSPublisher) Publish(ctx context.Context, taskID, traceID, traceparent, payload string) (bool, error) {
	body, err := json.Marshal(map[string]string{
		"task_id":     taskID,
		"trace_id":    traceID,
		"traceparent": traceparent,
		"payload":     payload,
	})
	if err != nil {
		return false, err
	}

	_, err = p.client.SendMessage(ctx, &sqssdk.SendMessageInput{
		QueueUrl:    aws.String(p.queueURL),
		MessageBody: aws.String(string(body)),
		MessageAttributes: map[string]sqstypes.MessageAttributeValue{
			"traceparent": {
				DataType:    aws.String("String"),
				StringValue: aws.String(traceparent),
			},
		},
	})
	if err != nil {
		return false, err
	}
	return true, nil
}

type QueuePublisher struct {
	q *queue.Queue
}

func NewQueuePublisher(q *queue.Queue) *QueuePublisher {
	return &QueuePublisher{q: q}
}

func (p *QueuePublisher) Publish(_ context.Context, taskID, traceID, traceparent, payload string) (bool, error) {
	ok := p.q.Enqueue(queue.Task{
		ID:          taskID,
		TraceID:     traceID,
		Traceparent: traceparent,
		Payload:     payload,
	})
	return ok, nil
}

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
	broker    *event.Broker
	publisher Publisher
}

func NewTaskHandler(broker *event.Broker, publisher Publisher) *TaskHandler {
	return &TaskHandler{broker: broker, publisher: publisher}
}

func (h *TaskHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx, span := taskTracer.Start(r.Context(), "task.create")
	defer span.End()

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

	spanCtx := span.SpanContext()
	traceID := spanCtx.TraceID().String()
	traceparent := fmt.Sprintf("00-%s-%s-01",
		spanCtx.TraceID().String(),
		spanCtx.SpanID().String(),
	)

	taskID := uuid.New().String()

	_, enqueueSpan := taskTracer.Start(ctx, "task.enqueue")
	ok, err := h.publisher.Publish(ctx, taskID, traceID, traceparent, req.Input)
	if err != nil {
		enqueueSpan.End()
		telemetry.Error(ctx, "failed to publish task", "task_id", taskID, "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !ok {
		enqueueSpan.End()
		metrics.QueueRejected.Inc()
		telemetry.Warn(ctx, "task rejected — queue full", "task_id", taskID)
		jsonError(w, "queue_full", http.StatusServiceUnavailable)
		return
	}
	metrics.QueueEnqueued.Inc()
	enqueueSpan.End()

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

	telemetry.Info(ctx, "task created", "task_id", taskID, "event_type", "task.created")

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
