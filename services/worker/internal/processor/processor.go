package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	sqssdk "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/runtime-platform/services/worker/internal/metrics"
)

var tracer = otel.Tracer("traceruntime-worker/processor")

type Config struct {
	SQSURL         string
	S3Bucket       string
	APIInternalURL string
	AIRuntimeURL   string
	AIEnabled      bool
}

type Processor struct {
	cfg      Config
	sqs      *sqssdk.Client
	s3       *s3.Client
	httpClient *http.Client
}

func New(cfg Config, sqsClient *sqssdk.Client, s3Client *s3.Client) *Processor {
	return &Processor{
		cfg:      cfg,
		sqs:      sqsClient,
		s3:       s3Client,
		httpClient: &http.Client{Timeout: 5 * time.Second},
	}
}

type sqsMessage struct {
	TaskID      string `json:"task_id"`
	TraceID     string `json:"trace_id"`
	Traceparent string `json:"traceparent"`
	Payload     string `json:"payload"`
}

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
	InferenceDurationMs int    `json:"inference_duration_ms,omitempty"`
	ErrorReason         string `json:"error_reason,omitempty"`
	S3Key               string `json:"s3_key,omitempty"`
}

// Process handles a single SQS message. Deletes from SQS only on success.
func (p *Processor) Process(ctx context.Context, body string, traceparentAttr string, receiptHandle string) {
	start := time.Now()

	var msg sqsMessage
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		slog.Error("failed to parse sqs message body", "error", err, "body", body)
		return // leave in queue for redelivery
	}

	// traceparent from message attribute is authoritative; fall back to body field
	traceparent := traceparentAttr
	if traceparent == "" {
		traceparent = msg.Traceparent
	}

	carrier := propagation.MapCarrier{"traceparent": traceparent}
	parentCtx := otel.GetTextMapPropagator().Extract(ctx, carrier)

	processCtx, span := tracer.Start(parentCtx, "task.process")
	defer span.End()

	spanCtx := span.SpanContext()
	childTraceparent := fmt.Sprintf("00-%s-%s-01",
		spanCtx.TraceID().String(),
		spanCtx.SpanID().String(),
	)
	traceID := spanCtx.TraceID().String()

	slog.Info("processing task",
		"task_id", msg.TaskID,
		"trace_id", traceID,
		"traceparent", childTraceparent,
	)

	p.publishSSE(sseEvent{
		EventType:   "task.processing",
		TraceID:     traceID,
		Traceparent: childTraceparent,
		TaskID:      msg.TaskID,
		Source:      "worker",
	})

	var output, model string
	var durationMs int

	if !p.cfg.AIEnabled {
		output = fmt.Sprintf("mock output for task %s", msg.TaskID)
		model = "mock"
		slog.Info("AI runtime disabled — using mock output", "task_id", msg.TaskID)
	} else {
		var err error
		output, model, durationMs, err = p.callAIRuntime(processCtx, msg, childTraceparent)
		if err != nil {
			slog.Error("ai runtime call failed", "task_id", msg.TaskID, "error", err)
			metrics.TasksFailed.Inc()
			p.publishSSE(sseEvent{
				EventType:   "task.failed",
				TraceID:     traceID,
				Traceparent: childTraceparent,
				TaskID:      msg.TaskID,
				Source:      "worker",
				ErrorReason: "ai_runtime_error",
			})
			return // do NOT delete from SQS — allow redelivery
		}
	}

	s3Key := fmt.Sprintf("%s/%s.json", traceID, msg.TaskID)
	s3Payload, _ := json.Marshal(map[string]any{
		"task_id":    msg.TaskID,
		"trace_id":   traceID,
		"output":     output,
		"model":      model,
		"created_at": time.Now().UTC().Format(time.RFC3339),
	})
	_, err := p.s3.PutObject(processCtx, &s3.PutObjectInput{
		Bucket:      aws.String(p.cfg.S3Bucket),
		Key:         aws.String(s3Key),
		Body:        bytes.NewReader(s3Payload),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		slog.Error("failed to write output to s3", "task_id", msg.TaskID, "error", err)
		metrics.TasksFailed.Inc()
		p.publishSSE(sseEvent{
			EventType:   "task.failed",
			TraceID:     traceID,
			Traceparent: childTraceparent,
			TaskID:      msg.TaskID,
			Source:      "worker",
			ErrorReason: "s3_write_error",
		})
		return // do NOT delete from SQS
	}

	_, err = p.sqs.DeleteMessage(processCtx, &sqssdk.DeleteMessageInput{
		QueueUrl:      aws.String(p.cfg.SQSURL),
		ReceiptHandle: aws.String(receiptHandle),
	})
	if err != nil {
		// task processed successfully; log only — don't fail
		slog.Error("failed to delete sqs message", "task_id", msg.TaskID, "error", err)
	}

	metrics.TasksProcessed.Inc()
	metrics.TaskDuration.Observe(time.Since(start).Seconds())

	slog.Info("task completed",
		"task_id", msg.TaskID,
		"trace_id", traceID,
		"model", model,
		"duration_ms", durationMs,
		"s3_key", s3Key,
	)

	p.publishSSE(sseEvent{
		EventType:           "task.completed",
		TraceID:             traceID,
		Traceparent:         childTraceparent,
		TaskID:              msg.TaskID,
		Source:              "worker",
		Output:              output,
		Model:               model,
		ExecutionStatus:     "completed",
		InferenceDurationMs: durationMs,
		S3Key:               s3Key,
	})
}

func (p *Processor) callAIRuntime(ctx context.Context, msg sqsMessage, traceparent string) (output, model string, durationMs int, err error) {
	deadline := time.Now().Add(120 * time.Second)
	inferCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	body, _ := json.Marshal(map[string]any{
		"task_id":          msg.TaskID,
		"input":            msg.Payload,
		"deadline_unix_ms": deadline.UnixMilli(),
	})

	req, err := http.NewRequestWithContext(inferCtx, http.MethodPost, p.cfg.AIRuntimeURL+"/infer", bytes.NewReader(body))
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("traceparent", traceparent)

	client := &http.Client{Timeout: 125 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return "", "", 0, fmt.Errorf("ai runtime HTTP %d", resp.StatusCode)
	}

	var result struct {
		Output          string `json:"output"`
		ExecutionStatus string `json:"execution_status"`
		InferenceDurationMs int `json:"inference_duration_ms"`
		ExecutionProfile struct {
			Model string `json:"model"`
		} `json:"execution_profile"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", 0, err
	}
	if result.ExecutionStatus == "failed" {
		return "", "", 0, fmt.Errorf("ai runtime returned failed status")
	}
	return result.Output, result.ExecutionProfile.Model, result.InferenceDurationMs, nil
}

func (p *Processor) publishSSE(ev sseEvent) {
	ev.EventID = uuid.New().String()
	ev.Timestamp = time.Now().UTC().Format(time.RFC3339)

	body, err := json.Marshal(ev)
	if err != nil {
		slog.Error("failed to marshal sse event", "error", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.cfg.APIInternalURL+"/internal/events",
		bytes.NewReader(body),
	)
	if err != nil {
		slog.Error("failed to build sse publish request", "error", err)
		metrics.SSEPublishErrors.Inc()
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		slog.Warn("sse publish failed — api unreachable", "error", err)
		metrics.SSEPublishErrors.Inc()
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		slog.Warn("sse publish returned error status", "status", resp.StatusCode)
		metrics.SSEPublishErrors.Inc()
	}
}

func EnvBool(key string, def bool) bool {
	v := os.Getenv(key)
	if v == "false" || v == "0" {
		return false
	}
	if v == "true" || v == "1" {
		return true
	}
	return def
}
