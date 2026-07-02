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

const (
	slowInferenceName      = "slow-inference"
	slowInferenceTimeout   = 10 * time.Minute
	slowInferenceDelay     = 30 // seconds
	slowInferenceTaskQty   = 2
	slowInferenceSettleTmo = 8 * time.Minute
)

type SlowInference struct {
	taskIDs []string
}

func NewSlowInference() *SlowInference { return &SlowInference{} }

func (s *SlowInference) Name() string           { return slowInferenceName }
func (s *SlowInference) Timeout() time.Duration  { return slowInferenceTimeout }

func (s *SlowInference) Setup(ctx context.Context, sc *chaos.ScenarioContext) error {
	slog.Info("slow-inference: setup — verifying system baseline")

	containers := []string{"api", "worker", "watchdog", "ai-runtime"}
	for _, c := range containers {
		status, err := sc.Checker.ContainerHealth(ctx, c)
		if err != nil {
			return fmt.Errorf("container health %s: %w", c, err)
		}
		if status != docker.HealthHealthy {
			if c == "ai-runtime" {
				slog.Info("slow-inference: setup — attempting ai-runtime recovery", "current_status", status)
				if startErr := sc.Docker.Start(ctx, "ai-runtime"); startErr != nil {
					slog.Warn("slow-inference: setup start ai-runtime", "error", startErr)
				}
				if waitErr := sc.Docker.WaitForHealthy(ctx, "ai-runtime", 120*time.Second); waitErr != nil {
					return fmt.Errorf("ai-runtime recovery failed: %w", waitErr)
				}
				slog.Info("slow-inference: setup — ai-runtime recovered")
				continue
			}
			return fmt.Errorf("container %s is %s, expected healthy", c, status)
		}
	}

	// Reset any prior chaos config.
	if err := resetChaosConfig(sc.Config.AIRuntimeURL, sc.Config.InternalToken); err != nil {
		slog.Warn("slow-inference: reset chaos config during setup", "error", err)
		// Non-fatal: may not have been set before.
	}

	slog.Info("slow-inference: setup complete")
	return nil
}

func (s *SlowInference) Inject(ctx context.Context, sc *chaos.ScenarioContext) error {
	slog.Info("slow-inference: inject — setting artificial delay", "delay_seconds", slowInferenceDelay)

	if err := setChaosConfig(sc.Config.AIRuntimeURL, sc.Config.InternalToken, slowInferenceDelay); err != nil {
		return fmt.Errorf("set chaos delay: %w", err)
	}

	slog.Info("slow-inference: inject — submitting tasks", "count", slowInferenceTaskQty)
	s.taskIDs = nil
	for i := 0; i < slowInferenceTaskQty; i++ {
		input := fmt.Sprintf("chaos-slow-inference-task-%d", i+1)
		taskID, err := submitTaskWithRunID(sc.Config.APIURL, input, sc.ChaosRunDBID)
		if err != nil {
			return fmt.Errorf("submit task %d: %w", i+1, err)
		}
		s.taskIDs = append(s.taskIDs, taskID)
		slog.Info("slow-inference: task submitted", "index", i+1, "task_id", taskID)
	}

	return nil
}

func (s *SlowInference) Observe(ctx context.Context, sc *chaos.ScenarioContext) (*chaos.ObserveResult, error) {
	slog.Info("slow-inference: observe — waiting for submitted tasks to complete", "timeout", slowInferenceSettleTmo, "task_ids", s.taskIDs)
	result := chaos.NewObserveResult()
	observeStart := time.Now()

	waitResult, err := sc.Checker.WaitFor(ctx, checker.WaitCondition{
		Name: "submitted tasks settled",
		Check: func(ctx context.Context) (bool, error) {
			for _, id := range s.taskIDs {
				status, err := sc.Checker.TaskStatus(ctx, id)
				if err != nil {
					return false, err
				}
				if status == "pending" || status == "processing" {
					return false, nil
				}
			}
			return true, nil
		},
		Timeout:  slowInferenceSettleTmo,
		Interval: 5 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("waiting for tasks to settle: %w", err)
	}

	result.Metrics["tasks_settled"] = waitResult.Met
	result.Metrics["settle_seconds"] = time.Since(observeStart).Seconds()

	completed := 0
	failed := 0
	for _, id := range s.taskIDs {
		status, err := sc.Checker.TaskStatus(ctx, id)
		if err != nil {
			slog.Warn("slow-inference: task status error", "task_id", id, "error", err)
			continue
		}
		switch status {
		case "completed":
			completed++
		case "failed":
			failed++
		}
	}
	result.Metrics["completed_count"] = completed
	result.Metrics["failed_count"] = failed

	dlqDepth, err := sc.Checker.DLQDepth(ctx)
	if err != nil {
		slog.Warn("slow-inference: dlq depth", "error", err)
		dlqDepth = -1
	}
	result.Metrics["dlq_depth"] = dlqDepth

	workerStatus, err := sc.Checker.WorkerHealth(ctx)
	if err != nil {
		slog.Warn("slow-inference: worker health", "error", err)
		result.Metrics["worker_healthy"] = false
	} else {
		result.Metrics["worker_healthy"] = (workerStatus == 200)
	}

	slog.Info("slow-inference: observe complete",
		"settled", waitResult.Met,
		"completed", completed,
		"failed", failed,
		"dlq_depth", dlqDepth,
		"settle_s", fmt.Sprintf("%.1f", time.Since(observeStart).Seconds()),
	)
	return result, nil
}

func (s *SlowInference) Validate(ctx context.Context, sc *chaos.ScenarioContext, observed *chaos.ObserveResult) (*chaos.ScenarioReport, error) {
	slog.Info("slow-inference: validate — checking SLOs")
	report := chaos.NewScenarioReport(slowInferenceName)

	completed, _ := observed.Metrics["completed_count"].(int)
	dlqDepth, _ := observed.Metrics["dlq_depth"].(int)
	settled, _ := observed.Metrics["tasks_settled"].(bool)
	workerHealthy, _ := observed.Metrics["worker_healthy"].(bool)
	settleSec, _ := observed.Metrics["settle_seconds"].(float64)

	// Functional: tasks completed successfully (count == 2)
	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "tasks completed successfully",
		Type:     chaos.SLOFunctional,
		Expected: slowInferenceTaskQty,
		Actual:   completed,
		Status:   intSLO(completed, slowInferenceTaskQty),
	})

	// Functional: zero DLQ messages
	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "zero DLQ messages",
		Type:     chaos.SLOFunctional,
		Expected: 0,
		Actual:   dlqDepth,
		Status:   intSLO(dlqDepth, 0),
	})

	// Functional: tasks settled within timeout
	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "tasks settled within timeout",
		Type:     chaos.SLOFunctional,
		Expected: true,
		Actual:   settled,
		Status:   boolSLO(settled),
	})

	// Functional: worker healthy throughout
	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "worker healthy throughout",
		Type:     chaos.SLOFunctional,
		Expected: true,
		Actual:   workerHealthy,
		Status:   boolSLO(workerHealthy),
	})

	// Timing: processing duration > 120s (delay was applied)
	// If settle time < delay, the delay was not actually applied.
	delayApplied := settleSec > float64(slowInferenceDelay)
	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     fmt.Sprintf("processing duration > %ds (delay applied)", slowInferenceDelay),
		Type:     chaos.SLOTiming,
		Expected: fmt.Sprintf(">%ds", slowInferenceDelay),
		Actual:   fmt.Sprintf("%.1fs", settleSec),
		Status:   boolSLO(delayApplied),
	})

	report.Metrics = observed.Metrics
	report.ComputeStatus()

	slog.Info("slow-inference: validate complete", "status", report.Status)
	return report, nil
}

func (s *SlowInference) Cleanup(ctx context.Context, sc *chaos.ScenarioContext) error {
	slog.Info("slow-inference: cleanup — resetting chaos config")

	if err := resetChaosConfig(sc.Config.AIRuntimeURL, sc.Config.InternalToken); err != nil {
		slog.Warn("slow-inference: reset chaos config", "error", err)
	}

	// Wait for queue to drain.
	_, err := sc.Checker.WaitFor(ctx, checker.WaitCondition{
		Name: "queue drained after slow-inference",
		Check: func(ctx context.Context) (bool, error) {
			depth, err := sc.Checker.QueueDepth(ctx)
			if err != nil {
				return false, err
			}
			return depth == 0, nil
		},
		Timeout:  30 * time.Second,
		Interval: sc.PollInterval,
	})
	if err != nil {
		slog.Warn("slow-inference: queue drain wait", "error", err)
	}

	stabilizeSystem(ctx, "slow-inference", sc)

	slog.Info("slow-inference: cleanup complete")
	return nil
}
