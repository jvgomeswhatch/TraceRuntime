package scenarios

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/runtime-platform/cmd/chaos/internal/chaos"
	"github.com/runtime-platform/cmd/chaos/internal/chaos/checker"
	"github.com/runtime-platform/cmd/chaos/internal/chaos/docker"
)

// RuntimeHang freezes the ai-runtime process (docker pause) and validates that
// the watchdog detects the stuck task within the configured threshold.
type RuntimeHang struct {
	taskID   string
	pausedAt time.Time
}

func NewRuntimeHang() *RuntimeHang {
	return &RuntimeHang{}
}

func (s *RuntimeHang) Name() string           { return "runtime-hang" }
func (s *RuntimeHang) Timeout() time.Duration { return 8 * time.Minute }

func (s *RuntimeHang) Setup(ctx context.Context, sc *chaos.ScenarioContext) error {
	slog.Info("runtime-hang: setup — verifying all containers healthy")

	required := []string{"api", "worker", "watchdog", "postgres", "localstack", "ai-runtime"}
	healthy, err := sc.Checker.AllContainersHealthy(ctx, required)
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}
	if !healthy {
		return fmt.Errorf("not all containers healthy")
	}

	slog.Info("runtime-hang: setup complete — all containers healthy")
	return nil
}

func (s *RuntimeHang) Inject(ctx context.Context, sc *chaos.ScenarioContext) error {
	slog.Info("runtime-hang: inject — submitting task")

	taskID, err := submitTask(sc.Config.APIURL, "chaos-runtime-hang-test")
	if err != nil {
		return fmt.Errorf("submit task: %w", err)
	}
	s.taskID = taskID
	slog.Info("runtime-hang: task submitted", "task_id", taskID)

	// Wait for task to reach "processing" status
	result, err := sc.Checker.WaitFor(ctx, checker.WaitCondition{
		Name: "task-processing",
		Check: func(ctx context.Context) (bool, error) {
			status, err := sc.Checker.TaskStatus(ctx, s.taskID)
			if err != nil {
				return false, err
			}
			return status == "processing", nil
		},
		Timeout:  30 * time.Second,
		Interval: 2 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("wait for processing: %w", err)
	}
	if !result.Met {
		return fmt.Errorf("task %s did not reach processing within 30s", s.taskID)
	}
	slog.Info("runtime-hang: task is processing", "task_id", s.taskID, "elapsed", result.Elapsed)

	// Pause ai-runtime to simulate a frozen process
	slog.Info("runtime-hang: pausing ai-runtime")
	if err := sc.Docker.Pause(ctx, "ai-runtime"); err != nil {
		return fmt.Errorf("pause ai-runtime: %w", err)
	}
	s.pausedAt = time.Now()
	slog.Info("runtime-hang: ai-runtime paused", "paused_at", s.pausedAt)

	return nil
}

func (s *RuntimeHang) Observe(ctx context.Context, sc *chaos.ScenarioContext) (*chaos.ObserveResult, error) {
	slog.Info("runtime-hang: observe — waiting for task.stuck healing event")

	observed := chaos.NewObserveResult()
	observeStart := time.Now()

	// Wait up to 360s (300s default threshold + 60s margin)
	result, err := sc.Checker.WaitFor(ctx, checker.WaitCondition{
		Name: "task-stuck-event",
		Check: func(ctx context.Context) (bool, error) {
			events, err := sc.Checker.HealingEventsSince(ctx, s.pausedAt)
			if err != nil {
				return false, err
			}
			for _, e := range events {
				if e.EventType == "task.stuck" {
					slog.Info("runtime-hang: task.stuck event detected", "event_id", e.ID, "elapsed", time.Since(s.pausedAt))
					return true, nil
				}
			}
			return false, nil
		},
		Timeout:  360 * time.Second,
		Interval: 5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("wait for task.stuck: %w", err)
	}

	stuckDetected := result.Met
	stuckDetectionTime := time.Since(observeStart).Seconds()
	observed.Metrics["stuck_detected"] = stuckDetected
	observed.Metrics["stuck_detection_time_s"] = stuckDetectionTime

	// Check worker health — it should still be running even with ai-runtime paused
	workerHealth, err := sc.Checker.ContainerHealth(ctx, "worker")
	if err != nil {
		slog.Warn("runtime-hang: could not check worker health", "error", err)
		observed.Metrics["worker_healthy"] = false
	} else {
		observed.Metrics["worker_healthy"] = workerHealth == docker.HealthHealthy
	}

	slog.Info("runtime-hang: observe complete",
		"stuck_detected", stuckDetected,
		"detection_time_s", stuckDetectionTime,
		"worker_healthy", observed.Metrics["worker_healthy"],
	)

	return observed, nil
}

func (s *RuntimeHang) Validate(ctx context.Context, sc *chaos.ScenarioContext, observed *chaos.ObserveResult) (*chaos.ScenarioReport, error) {
	report := chaos.NewScenarioReport(s.Name())

	stuckDetected, _ := observed.Metrics["stuck_detected"].(bool)
	workerHealthy, _ := observed.Metrics["worker_healthy"].(bool)
	detectionTime, _ := observed.Metrics["stuck_detection_time_s"].(float64)

	// Functional: watchdog emits task.stuck
	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "watchdog_emits_task_stuck",
		Type:     chaos.SLOFunctional,
		Expected: true,
		Actual:   stuckDetected,
		Status:   boolSLO(stuckDetected),
	})

	// Functional: worker does not crash during hang
	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "worker_survives_hang",
		Type:     chaos.SLOFunctional,
		Expected: true,
		Actual:   workerHealthy,
		Status:   boolSLO(workerHealthy),
	})

	// Timing: stuck detection within 360s (300s threshold + 60s margin)
	// Uses default WATCHDOG_TASK_STUCK_SECONDS=300
	maxDetectionTime := 360.0
	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "stuck_detection_time",
		Type:     chaos.SLOTiming,
		Expected: maxDetectionTime,
		Actual:   detectionTime,
		Status:   timingSLO(detectionTime, maxDetectionTime),
	})

	report.Metrics["stuck_detected"] = stuckDetected
	report.Metrics["worker_healthy"] = workerHealthy
	report.Metrics["stuck_detection_time_s"] = detectionTime

	report.ComputeStatus()
	return report, nil
}

func (s *RuntimeHang) Cleanup(ctx context.Context, sc *chaos.ScenarioContext) error {
	slog.Info("runtime-hang: cleanup — unpausing ai-runtime")

	// Unpause ai-runtime; if that fails, try Start
	if err := sc.Docker.Unpause(ctx, "ai-runtime"); err != nil {
		slog.Warn("runtime-hang: unpause failed, attempting start", "error", err)
		if startErr := sc.Docker.Start(ctx, "ai-runtime"); startErr != nil {
			slog.Error("runtime-hang: start also failed", "error", startErr)
			return fmt.Errorf("unpause: %w, start: %w", err, startErr)
		}
	}

	// Wait for ai-runtime to become healthy
	if err := sc.Docker.WaitForHealthy(ctx, "ai-runtime", 60*time.Second); err != nil {
		slog.Warn("runtime-hang: ai-runtime did not become healthy", "error", err)
	}

	// Wait for queue to drain
	drainResult, err := sc.Checker.WaitFor(ctx, checker.WaitCondition{
		Name: "queue-drain",
		Check: func(ctx context.Context) (bool, error) {
			depth, err := sc.Checker.QueueDepth(ctx)
			if err != nil {
				return false, err
			}
			return depth == 0, nil
		},
		Timeout:  120 * time.Second,
		Interval: 5 * time.Second,
	})
	if err != nil {
		slog.Warn("runtime-hang: queue drain wait error", "error", err)
	} else if !drainResult.Met {
		slog.Warn("runtime-hang: queue did not drain within 120s")
	}

	stabilizeSystem(ctx, "runtime-hang", sc)

	slog.Info("runtime-hang: cleanup complete")
	return nil
}
