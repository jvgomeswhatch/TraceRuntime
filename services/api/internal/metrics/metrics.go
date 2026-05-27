package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	QueueDepth = prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "traceruntime_queue_depth",
		Help: "Current number of tasks waiting in queue.",
	}, nil)

	QueueEnqueued = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "traceruntime_queue_enqueued_total",
		Help: "Total tasks successfully enqueued.",
	})

	QueueRejected = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "traceruntime_queue_rejected_total",
		Help: "Total tasks rejected due to queue full.",
	})

	QueueProcessed = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "traceruntime_queue_processed_total",
		Help: "Total tasks dequeued and sent to worker.",
	})

	WorkerActiveTasks = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "traceruntime_worker_active_tasks",
		Help: "Number of tasks currently being processed by the worker.",
	})

	SSEDropped = prometheus.NewCounter(prometheus.CounterOpts{
		Name: "traceruntime_sse_events_dropped_total",
		Help: "Total SSE events dropped due to slow consumers.",
	})
)

func MustRegisterAll(queueDepthFn func() float64) {
	QueueDepth = prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "traceruntime_queue_depth",
		Help: "Current number of tasks waiting in queue.",
	}, queueDepthFn)

	prometheus.MustRegister(
		QueueDepth,
		QueueEnqueued,
		QueueRejected,
		QueueProcessed,
		WorkerActiveTasks,
		SSEDropped,
	)
}
