package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	sqssdk "github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"

	"github.com/runtime-platform/services/api/internal/db"
	"github.com/runtime-platform/services/api/internal/event"
)

var replayTracer = otel.Tracer("traceruntime-api/replay")

type ReplayHandler struct {
	db        *db.DB
	broker    *event.Broker
	sqsClient *sqssdk.Client
	sqsURL    string
}

func NewReplayHandler(database *db.DB, broker *event.Broker, sqsClient *sqssdk.Client, sqsURL string) *ReplayHandler {
	return &ReplayHandler{db: database, broker: broker, sqsClient: sqsClient, sqsURL: sqsURL}
}

type replayRequest struct {
	OverridePayload string `json:"override_payload"`
}

func (h *ReplayHandler) Replay(w http.ResponseWriter, r *http.Request) {
	ctx, span := replayTracer.Start(r.Context(), "task.replay")
	defer span.End()

	taskID := chi.URLParam(r, "taskID")

	task, err := h.db.GetTaskForReplay(ctx, taskID)
	if err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}
	if task == nil {
		jsonError(w, "task not found", http.StatusNotFound)
		return
	}

	if task.Status != "completed" && task.Status != "failed" {
		jsonError(w, fmt.Sprintf("task still in progress (status: %s)", task.Status), http.StatusConflict)
		return
	}

	var req replayRequest
	_ = json.NewDecoder(r.Body).Decode(&req)

	var payload string
	if req.OverridePayload != "" {
		payload = req.OverridePayload
	} else if task.InputPayload != nil && *task.InputPayload != "" {
		payload = *task.InputPayload
	} else {
		jsonError(w, "original payload not available (task created before replay support)", http.StatusUnprocessableEntity)
		return
	}

	if err := h.checkQueueDepth(ctx); err != nil {
		jsonError(w, "queue full", http.StatusServiceUnavailable)
		return
	}

	newTaskID := uuid.New().String()
	spanCtx := span.SpanContext()
	newTraceID := spanCtx.TraceID().String()
	traceparent := fmt.Sprintf("00-%s-%s-01", spanCtx.TraceID().String(), spanCtx.SpanID().String())

	if err := h.db.InsertTask(ctx, db.CreateTaskParams{
		ID:           newTaskID,
		TraceID:      newTraceID,
		InputPayload: payload,
		ReplayOf:     &taskID,
	}); err != nil {
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	body, _ := json.Marshal(map[string]string{
		"task_id":     newTaskID,
		"trace_id":    newTraceID,
		"traceparent": traceparent,
		"payload":     payload,
	})
	_, err = h.sqsClient.SendMessage(ctx, &sqssdk.SendMessageInput{
		QueueUrl:    aws.String(h.sqsURL),
		MessageBody: aws.String(string(body)),
		MessageAttributes: map[string]sqstypes.MessageAttributeValue{
			"traceparent": {DataType: aws.String("String"), StringValue: aws.String(traceparent)},
		},
	})
	if err != nil {
		_ = h.db.SetFailed(ctx, newTaskID, "sqs: publish failed on replay")
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	sseEvent, _ := json.Marshal(map[string]string{
		"event_id":   uuid.New().String(),
		"event_type": "task.created",
		"trace_id":   newTraceID,
		"task_id":    newTaskID,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
		"source":     "api/replay",
	})
	h.broker.Publish(sseEvent)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"task": map[string]any{
			"id":            newTaskID,
			"trace_id":      newTraceID,
			"status":        "pending",
			"replay_of":     taskID,
			"input_payload": payload,
			"created_at":    time.Now().UTC().Format(time.RFC3339),
		},
	})
}

func (h *ReplayHandler) checkQueueDepth(ctx context.Context) error {
	out, _ := h.sqsClient.GetQueueAttributes(ctx, &sqssdk.GetQueueAttributesInput{
		QueueUrl:       aws.String(h.sqsURL),
		AttributeNames: []sqstypes.QueueAttributeName{"ApproximateNumberOfMessages"},
	})
	if out == nil {
		return nil
	}
	var depth int
	_, _ = fmt.Sscanf(out.Attributes["ApproximateNumberOfMessages"], "%d", &depth)
	if depth >= 100 {
		return fmt.Errorf("queue full")
	}
	return nil
}
