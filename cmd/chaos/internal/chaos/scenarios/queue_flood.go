package scenarios

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/runtime-platform/cmd/chaos/internal/chaos"
	"github.com/runtime-platform/cmd/chaos/internal/chaos/checker"
	"github.com/runtime-platform/cmd/chaos/internal/chaos/docker"
)

const (
	queueFloodName        = "queue-flood"
	queueFloodTimeout     = 4 * time.Minute
	queueFloodTaskQty     = 60
	queueFloodDetectSLO   = 90.0 // seconds — SLO threshold for validation
	queueFloodObserveTmo  = 120 * time.Second // observe waits longer than SLO to capture the metric
)

type QueueFlood struct {
	restartBaseline *RestartBaseline
}

func NewQueueFlood() *QueueFlood { return &QueueFlood{} }

func (q *QueueFlood) Name() string           { return queueFloodName }
func (q *QueueFlood) Timeout() time.Duration  { return queueFloodTimeout }

func (q *QueueFlood) Setup(ctx context.Context, sc *chaos.ScenarioContext) error {
	slog.Info("queue-flood: setup — verifying system baseline")

	// All core containers must be healthy.
	coreContainers := []string{"api", "worker", "watchdog"}
	for _, c := range coreContainers {
		status, err := sc.Checker.ContainerHealth(ctx, c)
		if err != nil {
			return fmt.Errorf("container health %s: %w", c, err)
		}
		if status != docker.HealthHealthy {
			return fmt.Errorf("container %s is %s, expected healthy", c, status)
		}
	}

	// Queue must be empty.
	depth, err := sc.Checker.QueueDepth(ctx)
	if err != nil {
		return fmt.Errorf("queue depth: %w", err)
	}
	if depth > 0 {
		return fmt.Errorf("queue not empty (depth=%d), cannot start", depth)
	}

	// No active healing events.
	events, err := sc.Checker.ActiveHealingEvents(ctx)
	if err != nil {
		return fmt.Errorf("active healing events: %w", err)
	}
	if len(events) > 0 {
		return fmt.Errorf("%d active healing events — resolve before running", len(events))
	}

	baseline, err := CaptureRestartBaseline(ctx, sc, []string{"api", "worker", "watchdog"})
	if err != nil {
		return fmt.Errorf("restart baseline: %w", err)
	}
	q.restartBaseline = baseline

	slog.Info("queue-flood: setup complete — system baseline verified")
	return nil
}

func (q *QueueFlood) Inject(ctx context.Context, sc *chaos.ScenarioContext) error {
	slog.Info("queue-flood: inject — submitting burst of tasks", "count", queueFloodTaskQty)

	var wg sync.WaitGroup
	var submitted atomic.Int32
	var firstErr atomic.Value
	sem := make(chan struct{}, 10)

	for i := range queueFloodTaskQty {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			input := fmt.Sprintf("chaos-queue-flood-task-%d", idx+1)
			_, err := submitTaskWithRunID(sc.Config.APIURL, input, sc.ChaosRunDBID)
			if err != nil {
				firstErr.CompareAndSwap(nil, err)
				return
			}
			submitted.Add(1)
		}(i)
	}
	wg.Wait()

	if e, ok := firstErr.Load().(error); ok && e != nil {
		return fmt.Errorf("submit tasks: %w", e)
	}

	slog.Info("queue-flood: inject complete", "submitted", submitted.Load())
	return nil
}

func (q *QueueFlood) Observe(ctx context.Context, sc *chaos.ScenarioContext) (*chaos.ObserveResult, error) {
	slog.Info("queue-flood: observe — watching for queue.lag healing event")
	result := chaos.NewObserveResult()

	waitResult, err := sc.Checker.WaitFor(ctx, checker.WaitCondition{
		Name: "queue.lag healing event",
		Check: func(ctx context.Context) (bool, error) {
			events, err := sc.Checker.ActiveHealingEvents(ctx)
			if err != nil {
				return false, err
			}
			for _, e := range events {
				if e.EventType == "queue.lag" {
					return true, nil
				}
			}
			recent, err := sc.Checker.HealingEventsSince(ctx, sc.StartTime.Add(-30*time.Second))
			if err != nil {
				return false, err
			}
			for _, e := range recent {
				if e.EventType == "queue.lag" {
					return true, nil
				}
			}
			return false, nil
		},
		Timeout:  queueFloodObserveTmo,
		Interval: sc.PollInterval,
	})
	if err != nil {
		return nil, fmt.Errorf("waiting for queue.lag event: %w", err)
	}

	result.Metrics["queue_lag_detected"] = waitResult.Met
	result.Metrics["detection_seconds"] = waitResult.Elapsed.Seconds()

	// Track peak queue depth (sample once now, it may have already drained).
	peakDepth, err := sc.Checker.QueueDepth(ctx)
	if err != nil {
		slog.Warn("queue-flood: could not read peak queue depth", "error", err)
		peakDepth = -1
	}
	result.Metrics["peak_queue_depth"] = peakDepth

	// Record restart count deltas (compared to baseline captured in Setup).
	for _, svc := range []string{"api", "worker", "watchdog"} {
		delta, err := q.restartBaseline.Delta(ctx, sc, svc)
		if err != nil {
			slog.Warn("queue-flood: restart count error", "service", svc, "error", err)
			delta = -1
		}
		result.Metrics[svc+"_restart_count"] = delta
	}

	// Check worker health.
	workerStatus, err := sc.Checker.WorkerHealth(ctx)
	if err != nil {
		slog.Warn("queue-flood: worker health check failed", "error", err)
		result.Metrics["worker_healthy"] = false
	} else {
		result.Metrics["worker_healthy"] = (workerStatus == 200)
	}

	slog.Info("queue-flood: observe complete",
		"detected", waitResult.Met,
		"detection_s", fmt.Sprintf("%.1f", waitResult.Elapsed.Seconds()),
		"peak_depth", peakDepth,
	)
	return result, nil
}

func (q *QueueFlood) Validate(ctx context.Context, sc *chaos.ScenarioContext, observed *chaos.ObserveResult) (*chaos.ScenarioReport, error) {
	slog.Info("queue-flood: validate — checking SLOs")
	report := chaos.NewScenarioReport(queueFloodName)

	detected, _ := observed.Metrics["queue_lag_detected"].(bool)
	detectionSec, _ := observed.Metrics["detection_seconds"].(float64)
	workerHealthy, _ := observed.Metrics["worker_healthy"].(bool)

	// Functional: watchdog emits queue.lag healing event
	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "watchdog emits queue.lag event",
		Type:     chaos.SLOFunctional,
		Expected: true,
		Actual:   detected,
		Status:   boolSLO(detected),
	})

	// Functional: no container crashes
	for _, svc := range []string{"api", "worker", "watchdog"} {
		key := svc + "_restart_count"
		rc, _ := observed.Metrics[key].(int)
		report.SLOResults = append(report.SLOResults, chaos.SLOResult{
			Name:     fmt.Sprintf("no crashes (%s)", svc),
			Type:     chaos.SLOFunctional,
			Expected: 0,
			Actual:   rc,
			Status:   intSLO(rc, 0),
		})
	}

	// Functional: worker remains healthy
	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "worker remains healthy",
		Type:     chaos.SLOFunctional,
		Expected: true,
		Actual:   workerHealthy,
		Status:   boolSLO(workerHealthy),
	})

	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "queue.lag detection < 90s",
		Type:     chaos.SLOTiming,
		Expected: queueFloodDetectSLO,
		Actual:   detectionSec,
		Status:   timingSLO(detectionSec, queueFloodDetectSLO),
	})

	report.Metrics = observed.Metrics
	report.ComputeStatus()

	slog.Info("queue-flood: validate complete", "status", report.Status)
	return report, nil
}

func (q *QueueFlood) Cleanup(ctx context.Context, sc *chaos.ScenarioContext) error {
	slog.Info("queue-flood: cleanup — draining queue and resolving events")

	if err := sc.Checker.PurgeQueue(ctx); err != nil {
		slog.Warn("queue-flood: purge queue error", "error", err)
	}

	stabilizeSystem(ctx, "queue-flood", sc)

	slog.Info("queue-flood: cleanup complete")
	return nil
}
