package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	QueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "traceruntime_worker_queue_depth",
		Help: "Approximate number of messages in the main SQS queue.",
	})

	QueueInflight = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "traceruntime_worker_queue_inflight",
		Help: "Approximate number of messages currently being processed (not visible).",
	})

	DLQDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "traceruntime_worker_dlq_depth",
		Help: "Approximate number of messages in the DLQ.",
	})

	TaskDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "traceruntime_worker_task_duration_seconds",
		Help:    "End-to-end task processing time in seconds.",
		Buckets: []float64{1, 5, 10, 30, 60, 90, 120, 150},
	})

	SSEPublishErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "traceruntime_worker_sse_publish_errors_total",
		Help: "Total failed internal SSE publish calls to the API.",
	})

	TasksProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "traceruntime_worker_tasks_processed_total",
		Help: "Total tasks successfully processed.",
	})

	TasksFailed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "traceruntime_worker_tasks_failed_total",
		Help: "Total tasks that failed processing.",
	})

	SQSReceiveDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "traceruntime_worker_sqs_receive_duration_seconds",
		Help:    "Time spent waiting for SQS messages (long poll duration).",
		Buckets: []float64{0.1, 1, 5, 10, 20},
	})
)
