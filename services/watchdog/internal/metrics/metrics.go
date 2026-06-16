package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	WorkersTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "traceruntime_watchdog_workers_total",
		Help: "Total registered workers.",
	})

	WorkersHealthy = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "traceruntime_watchdog_workers_healthy",
		Help: "Workers with status healthy.",
	})

	WorkersStale = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "traceruntime_watchdog_workers_stale",
		Help: "Workers with status stale.",
	})

	WorkersDown = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "traceruntime_watchdog_workers_down",
		Help: "Workers with status down.",
	})

	HealingEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "traceruntime_watchdog_healing_events_total",
		Help: "Total healing events created.",
	}, []string{"event_type", "severity"})

	ActiveIncidents = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "traceruntime_watchdog_active_incidents",
		Help: "Currently active incidents.",
	})

	QueueLag = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "traceruntime_watchdog_queue_lag",
		Help: "Current main queue depth.",
	})

	DLQDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "traceruntime_watchdog_dlq_depth",
		Help: "Current DLQ depth.",
	})

	StuckTasks = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "traceruntime_watchdog_stuck_tasks",
		Help: "Currently stuck tasks.",
	})

	HealingEventsResolved = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "traceruntime_watchdog_healing_events_resolved_total",
		Help: "Total healing events resolved.",
	}, []string{"event_type"})

	PollDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "traceruntime_watchdog_poll_duration_seconds",
		Help:    "Time taken per watchdog poll cycle.",
		Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5},
	})
)
