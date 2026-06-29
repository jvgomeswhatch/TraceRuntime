package http

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/aws/aws-sdk-go-v2/aws"
	sqssdk "github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"

	"github.com/runtime-platform/services/api/internal/db"
	"github.com/runtime-platform/services/api/internal/queue"
	"github.com/runtime-platform/services/api/internal/telemetry"
)

type runtimeMetricsResponse struct {
	WindowSeconds  int     `json:"window_seconds"`
	P95LatencyMs   float64 `json:"p95_latency_ms"`
	AvgLatencyMs   float64 `json:"avg_latency_ms"`
	SuccessRate    float64 `json:"success_rate"`
	ErrorRate      float64 `json:"error_rate"`
	Completed      int     `json:"completed"`
	Failed         int     `json:"failed"`
	TotalCompleted int     `json:"total_completed"`
	TotalFailed    int     `json:"total_failed"`
	QueueDepth     int     `json:"queue_depth"`
}

type RuntimeMetricsHandler struct {
	db        *db.DB
	sqsClient *sqssdk.Client
	queueURL  string
	queue     *queue.Queue
}

func NewRuntimeMetricsHandler(database *db.DB, sqsClient *sqssdk.Client, queueURL string, q *queue.Queue) *RuntimeMetricsHandler {
	return &RuntimeMetricsHandler{
		db:        database,
		sqsClient: sqsClient,
		queueURL:  queueURL,
		queue:     q,
	}
}

func (h *RuntimeMetricsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	const defaultWindow = 300

	if h.db == nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(runtimeMetricsResponse{WindowSeconds: defaultWindow})
		return
	}

	m, err := h.db.RuntimeMetrics(ctx, defaultWindow)
	if err != nil {
		telemetry.Error(ctx, "runtime_metrics: query failed", "error", err)
		jsonError(w, "internal error", http.StatusInternalServerError)
		return
	}

	depth := h.queueDepth(ctx)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(runtimeMetricsResponse{
		WindowSeconds:  m.WindowSeconds,
		P95LatencyMs:   m.P95LatencyMs,
		AvgLatencyMs:   m.AvgLatencyMs,
		SuccessRate:    m.SuccessRate,
		ErrorRate:      m.ErrorRate,
		Completed:      m.Completed,
		Failed:         m.Failed,
		TotalCompleted: m.TotalCompleted,
		TotalFailed:    m.TotalFailed,
		QueueDepth:     depth,
	})
}

func (h *RuntimeMetricsHandler) queueDepth(ctx context.Context) int {
	if h.sqsClient != nil && h.queueURL != "" {
		out, err := h.sqsClient.GetQueueAttributes(ctx, &sqssdk.GetQueueAttributesInput{
			QueueUrl:       aws.String(h.queueURL),
			AttributeNames: []sqstypes.QueueAttributeName{sqstypes.QueueAttributeNameApproximateNumberOfMessages},
		})
		if err != nil {
			slog.Warn("runtime_metrics: sqs get queue attributes failed", "error", err)
			return 0
		}
		if v, ok := out.Attributes[string(sqstypes.QueueAttributeNameApproximateNumberOfMessages)]; ok {
			if n, err := strconv.Atoi(v); err == nil {
				return n
			}
		}
		return 0
	}
	if h.queue != nil {
		return h.queue.Depth()
	}
	return 0
}
