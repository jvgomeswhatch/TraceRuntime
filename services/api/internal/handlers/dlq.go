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

	"github.com/runtime-platform/services/api/internal/db"
	"github.com/runtime-platform/services/api/internal/event"
)

type DLQHandler struct {
	db        *db.DB
	broker    *event.Broker
	sqsClient *sqssdk.Client
	dlqURL    string
	mainURL   string
}

func NewDLQHandler(database *db.DB, broker *event.Broker, sqsClient *sqssdk.Client, dlqURL, mainURL string) *DLQHandler {
	return &DLQHandler{db: database, broker: broker, sqsClient: sqsClient, dlqURL: dlqURL, mainURL: mainURL}
}

type dlqMessage struct {
	MessageID     string            `json:"message_id"`
	ReceiptHandle string            `json:"receipt_handle"`
	TaskID        string            `json:"task_id"`
	TraceID       string            `json:"trace_id"`
	Payload       string            `json:"payload"`
	ReceiveCount  string            `json:"receive_count"`
	FirstReceived string            `json:"first_received_at"`
	SentAt        string            `json:"sent_at"`
	TaskStatus    string            `json:"task_status"`
	ErrorMessage  string            `json:"error_message"`
	MessageBody   string            `json:"message_body"`
	MessageAttrs  map[string]string `json:"message_attributes"`
}

func (h *DLQHandler) ListMessages(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	output, err := h.sqsClient.ReceiveMessage(ctx, &sqssdk.ReceiveMessageInput{
		QueueUrl:              aws.String(h.dlqURL),
		MaxNumberOfMessages:   10,
		VisibilityTimeout:     30,
		WaitTimeSeconds:       0,
		MessageAttributeNames: []string{"All"},
		AttributeNames:        []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameAll},
	})
	if err != nil {
		jsonError(w, "failed to read DLQ", http.StatusInternalServerError)
		return
	}

	for _, msg := range output.Messages {
		_, _ = h.sqsClient.ChangeMessageVisibility(ctx, &sqssdk.ChangeMessageVisibilityInput{
			QueueUrl:          aws.String(h.dlqURL),
			ReceiptHandle:     msg.ReceiptHandle,
			VisibilityTimeout: 0,
		})
	}

	messages := make([]dlqMessage, 0, len(output.Messages))
	for _, msg := range output.Messages {
		var body map[string]string
		_ = json.Unmarshal([]byte(*msg.Body), &body)

		taskStatus, errorMsg, _ := h.db.GetTaskStatus(ctx, body["task_id"])

		attrs := make(map[string]string)
		for k, v := range msg.MessageAttributes {
			if v.StringValue != nil {
				attrs[k] = *v.StringValue
			}
		}

		messages = append(messages, dlqMessage{
			MessageID:     *msg.MessageId,
			ReceiptHandle: *msg.ReceiptHandle,
			TaskID:        body["task_id"],
			TraceID:       body["trace_id"],
			Payload:       body["payload"],
			ReceiveCount:  msg.Attributes["ApproximateReceiveCount"],
			FirstReceived: msg.Attributes["ApproximateFirstReceiveTimestamp"],
			SentAt:        msg.Attributes["SentTimestamp"],
			TaskStatus:    taskStatus,
			ErrorMessage:  errorMsg,
			MessageBody:   *msg.Body,
			MessageAttrs:  attrs,
		})
	}

	attrOut, _ := h.sqsClient.GetQueueAttributes(ctx, &sqssdk.GetQueueAttributesInput{
		QueueUrl:       aws.String(h.dlqURL),
		AttributeNames: []sqstypes.QueueAttributeName{"ApproximateNumberOfMessages"},
	})
	approxCount := 0
	if attrOut != nil {
		_, _ = fmt.Sscanf(attrOut.Attributes["ApproximateNumberOfMessages"], "%d", &approxCount)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"messages":          messages,
		"approximate_count": approxCount,
	})
}

type retryRequest struct {
	ReceiptHandle     string            `json:"receipt_handle"`
	MessageBody       string            `json:"message_body"`
	MessageAttributes map[string]string `json:"message_attributes"`
}

func (h *DLQHandler) Retry(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req retryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.ReceiptHandle == "" || req.MessageBody == "" {
		jsonError(w, "receipt_handle and message_body required", http.StatusBadRequest)
		return
	}

	msgAttrs := make(map[string]sqstypes.MessageAttributeValue)
	for k, v := range req.MessageAttributes {
		msgAttrs[k] = sqstypes.MessageAttributeValue{
			DataType:    aws.String("String"),
			StringValue: aws.String(v),
		}
	}

	_, err := h.sqsClient.SendMessage(ctx, &sqssdk.SendMessageInput{
		QueueUrl:          aws.String(h.mainURL),
		MessageBody:       aws.String(req.MessageBody),
		MessageAttributes: msgAttrs,
	})
	if err != nil {
		jsonError(w, "failed to send to main queue", http.StatusInternalServerError)
		return
	}

	warning := false
	_, err = h.sqsClient.DeleteMessage(ctx, &sqssdk.DeleteMessageInput{
		QueueUrl:      aws.String(h.dlqURL),
		ReceiptHandle: aws.String(req.ReceiptHandle),
	})
	if err != nil {
		warning = true
	}

	var body map[string]string
	_ = json.Unmarshal([]byte(req.MessageBody), &body)
	taskID := body["task_id"]
	_ = h.db.RevertToPendingForRetry(ctx, taskID)

	details, _ := json.Marshal(map[string]string{"task_id": taskID})
	_, _ = h.db.InsertAuditEvent(ctx, "dlq.message.retried", "info", "operator", details)

	sseEvent, _ := json.Marshal(map[string]string{
		"event_type": "task.retrying",
		"task_id":    taskID,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	})
	h.broker.Publish(sseEvent)

	resp := map[string]any{
		"status":  "retried",
		"task_id": taskID,
		"message": "Message moved to main queue",
	}
	if warning {
		resp["message"] = "Message sent to main queue but DLQ delete failed (receipt handle expired). Message may appear in both queues temporarily."
		resp["warning"] = true
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

type deleteRequest struct {
	ReceiptHandle  string `json:"receipt_handle"`
	TaskID         string `json:"task_id"`
	TraceID        string `json:"trace_id"`
	PayloadPreview string `json:"payload_preview"`
}

func (h *DLQHandler) Delete(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req deleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request", http.StatusBadRequest)
		return
	}
	if req.ReceiptHandle == "" {
		jsonError(w, "receipt_handle required", http.StatusBadRequest)
		return
	}

	details, _ := json.Marshal(map[string]string{
		"task_id": req.TaskID, "trace_id": req.TraceID, "payload_preview": req.PayloadPreview,
	})
	auditID, _ := h.db.InsertAuditEvent(ctx, "dlq.message.deleted", "info", "operator", details)

	_, err := h.sqsClient.DeleteMessage(ctx, &sqssdk.DeleteMessageInput{
		QueueUrl:      aws.String(h.dlqURL),
		ReceiptHandle: aws.String(req.ReceiptHandle),
	})
	if err != nil {
		jsonError(w, "receipt handle expired, message may have been redelivered", http.StatusGone)
		return
	}

	sseEvent, _ := json.Marshal(map[string]string{
		"event_type": "healing.dlq.message.deleted",
		"task_id":    req.TaskID,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	})
	h.broker.Publish(sseEvent)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":         "deleted",
		"message_id":     chi.URLParam(r, "messageID"),
		"audit_event_id": auditID,
	})
}

func (h *DLQHandler) Purge(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	attrOut, _ := h.sqsClient.GetQueueAttributes(ctx, &sqssdk.GetQueueAttributesInput{
		QueueUrl:       aws.String(h.dlqURL),
		AttributeNames: []sqstypes.QueueAttributeName{"ApproximateNumberOfMessages"},
	})
	approxCount := 0
	if attrOut != nil {
		_, _ = fmt.Sscanf(attrOut.Attributes["ApproximateNumberOfMessages"], "%d", &approxCount)
	}

	details, _ := json.Marshal(map[string]string{"approximate_count": fmt.Sprintf("%d", approxCount)})
	auditID, _ := h.db.InsertAuditEvent(ctx, "dlq.purged", "warning", "operator", details)

	_, err := h.sqsClient.PurgeQueue(ctx, &sqssdk.PurgeQueueInput{
		QueueUrl: aws.String(h.dlqURL),
	})
	if err != nil {
		jsonError(w, "purge already in progress, wait 60s before retrying", http.StatusTooManyRequests)
		return
	}

	sseEvent, _ := json.Marshal(map[string]string{
		"event_type": "healing.dlq.purged",
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	})
	h.broker.Publish(sseEvent)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":              "purged",
		"approximate_deleted": approxCount,
		"audit_event_id":      auditID,
	})
}

func (h *DLQHandler) Stats(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	attrOut, err := h.sqsClient.GetQueueAttributes(ctx, &sqssdk.GetQueueAttributesInput{
		QueueUrl: aws.String(h.dlqURL),
		AttributeNames: []sqstypes.QueueAttributeName{
			"ApproximateNumberOfMessages",
			"ApproximateNumberOfMessagesNotVisible",
		},
	})
	if err != nil {
		jsonError(w, "failed to get DLQ stats", http.StatusInternalServerError)
		return
	}

	var approxMsgs, approxNotVis int
	_, _ = fmt.Sscanf(attrOut.Attributes["ApproximateNumberOfMessages"], "%d", &approxMsgs)
	_, _ = fmt.Sscanf(attrOut.Attributes["ApproximateNumberOfMessagesNotVisible"], "%d", &approxNotVis)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"approximate_messages":    approxMsgs,
		"approximate_not_visible": approxNotVis,
	})
}
