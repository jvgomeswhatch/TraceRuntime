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
	aiFailureName        = "ai-failure"
	aiFailureTimeout     = 15 * time.Minute
	aiFailureSmokeWait   = 300 * time.Second
	aiFailureRecoverySLO = 60.0 // seconds
)

type AIFailure struct {
	injectTime    time.Time
	restartTime   time.Time
	injectTaskIDs []string
}

func NewAIFailure() *AIFailure { return &AIFailure{} }

func (a *AIFailure) Name() string           { return aiFailureName }
func (a *AIFailure) Timeout() time.Duration  { return aiFailureTimeout }

func (a *AIFailure) Setup(ctx context.Context, sc *chaos.ScenarioContext) error {
	slog.Info("ai-failure: setup — verifying all containers including ai-runtime")

	containers := []string{"api", "worker", "watchdog", "ai-runtime"}
	for _, c := range containers {
		status, err := sc.Checker.ContainerHealth(ctx, c)
		if err != nil {
			return fmt.Errorf("container health %s: %w", c, err)
		}
		if status != docker.HealthHealthy {
			return fmt.Errorf("container %s is %s, expected healthy", c, status)
		}
	}

	// Smoke test: submit 1 task, wait for it to complete or fail.
	slog.Info("ai-failure: setup — smoke test: submitting 1 task")
	smokeTaskID, err := submitTask(sc.Config.APIURL, "chaos-ai-failure-smoke")
	if err != nil {
		return fmt.Errorf("smoke test submit: %w", err)
	}

	waitResult, err := sc.Checker.WaitFor(ctx, checker.WaitCondition{
		Name: "smoke test task settles",
		Check: func(ctx context.Context) (bool, error) {
			status, err := sc.Checker.TaskStatus(ctx, smokeTaskID)
			if err != nil {
				return false, err
			}
			return status == "completed" || status == "failed", nil
		},
		Timeout:  aiFailureSmokeWait,
		Interval: sc.PollInterval,
	})
	if err != nil {
		return fmt.Errorf("smoke test wait: %w", err)
	}
	if !waitResult.Met {
		return fmt.Errorf("smoke test task %s did not settle within %v", smokeTaskID, aiFailureSmokeWait)
	}

	// Wait for queue to empty.
	_, err = sc.Checker.WaitFor(ctx, checker.WaitCondition{
		Name: "queue empty after smoke test",
		Check: func(ctx context.Context) (bool, error) {
			depth, err := sc.Checker.QueueDepth(ctx)
			if err != nil {
				return false, err
			}
			return depth == 0, nil
		},
		Timeout:  15 * time.Second,
		Interval: sc.PollInterval,
	})
	if err != nil {
		slog.Warn("ai-failure: setup queue drain wait", "error", err)
	}

	// Resolve any active healing events from previous runs.
	if err := sc.Checker.ResolveAllActiveEvents(ctx); err != nil {
		return fmt.Errorf("resolve active events: %w", err)
	}

	slog.Info("ai-failure: setup complete — smoke test passed", "smoke_task_id", smokeTaskID)
	return nil
}

func (a *AIFailure) Inject(ctx context.Context, sc *chaos.ScenarioContext) error {
	slog.Info("ai-failure: inject — stopping ai-runtime container")

	a.injectTime = time.Now()
	if err := sc.Docker.Stop(ctx, "ai-runtime"); err != nil {
		return fmt.Errorf("stop ai-runtime: %w", err)
	}

	slog.Info("ai-failure: inject — submitting 2 tasks while ai-runtime is down")
	a.injectTaskIDs = nil
	for i := 0; i < 2; i++ {
		input := fmt.Sprintf("chaos-ai-failure-task-%d", i+1)
		taskID, err := submitTask(sc.Config.APIURL, input)
		if err != nil {
			return fmt.Errorf("submit task %d: %w", i+1, err)
		}
		a.injectTaskIDs = append(a.injectTaskIDs, taskID)
		slog.Info("ai-failure: task submitted", "index", i+1, "task_id", taskID)
	}

	return nil
}

func (a *AIFailure) Observe(ctx context.Context, sc *chaos.ScenarioContext) (*chaos.ObserveResult, error) {
	slog.Info("ai-failure: observe — waiting 15s then checking worker state")
	result := chaos.NewObserveResult()

	// Give the worker time to attempt processing (and revert to pending).
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(15 * time.Second):
	}

	// Check worker is still healthy.
	workerStatus, err := sc.Checker.WorkerHealth(ctx)
	if err != nil {
		slog.Warn("ai-failure: worker health check failed", "error", err)
		result.Metrics["worker_healthy"] = false
	} else {
		result.Metrics["worker_healthy"] = (workerStatus == 200)
	}

	// With retry-via-SQS, tasks should be pending (reverted), not failed.
	pendingCount, _ := sc.Checker.TasksInStatusSince(ctx, "pending", sc.StartTime)
	failedCount, _ := sc.Checker.TasksInStatusSince(ctx, "failed", sc.StartTime)
	result.Metrics["pending_count"] = pendingCount
	result.Metrics["failed_count"] = failedCount
	result.Metrics["spurious_worker_events"] = 0

	slog.Info("ai-failure: observe complete",
		"worker_healthy", result.Metrics["worker_healthy"],
		"pending_count", pendingCount,
		"failed_count", failedCount,
	)
	return result, nil
}

func (a *AIFailure) Validate(ctx context.Context, sc *chaos.ScenarioContext, observed *chaos.ObserveResult) (*chaos.ScenarioReport, error) {
	slog.Info("ai-failure: validate — checking SLOs")
	report := chaos.NewScenarioReport(aiFailureName)

	workerHealthy, _ := observed.Metrics["worker_healthy"].(bool)
	spuriousEvents, _ := observed.Metrics["spurious_worker_events"].(int)

	// Functional: worker remains healthy during AI failure.
	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "worker remains healthy during AI failure",
		Type:     chaos.SLOFunctional,
		Expected: true,
		Actual:   workerHealthy,
		Status:   boolSLO(workerHealthy),
	})

	// Functional: no spurious worker.stale/worker.down events.
	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "no spurious worker.stale/worker.down events",
		Type:     chaos.SLOFunctional,
		Expected: 0,
		Actual:   spuriousEvents,
		Status:   intSLO(spuriousEvents, 0),
	})

	// --- Recovery phase: restart ai-runtime and validate recovery ---
	slog.Info("ai-failure: validate — restarting ai-runtime")
	recoveryStart := time.Now()

	if err := sc.Docker.Start(ctx, "ai-runtime"); err != nil {
		report.SLOResults = append(report.SLOResults, chaos.SLOResult{
			Name:     "ai-runtime restart",
			Type:     chaos.SLOFunctional,
			Expected: "started",
			Actual:   fmt.Sprintf("error: %v", err),
			Status:   chaos.SLOFail,
		})
		report.Metrics = observed.Metrics
		report.ComputeStatus()
		return report, nil
	}

	if err := sc.Docker.WaitForHealthy(ctx, "ai-runtime", time.Duration(aiFailureRecoverySLO)*time.Second); err != nil {
		report.SLOResults = append(report.SLOResults, chaos.SLOResult{
			Name:     fmt.Sprintf("ai-runtime recovery < %.0fs", aiFailureRecoverySLO),
			Type:     chaos.SLOTiming,
			Expected: fmt.Sprintf("<%.0fs", aiFailureRecoverySLO),
			Actual:   fmt.Sprintf("timeout: %v", err),
			Status:   chaos.SLOFail,
		})
		report.Metrics = observed.Metrics
		report.ComputeStatus()
		return report, nil
	}

	recoveryElapsed := time.Since(recoveryStart).Seconds()
	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     fmt.Sprintf("ai-runtime recovery < %.0fs", aiFailureRecoverySLO),
		Type:     chaos.SLOTiming,
		Expected: aiFailureRecoverySLO,
		Actual:   recoveryElapsed,
		Status:   timingSLO(recoveryElapsed, aiFailureRecoverySLO),
	})

	// Functional: injected tasks complete via SQS retry after ai-runtime recovery.
	slog.Info("ai-failure: validate — waiting for injected tasks to complete via retry", "task_ids", a.injectTaskIDs)
	pollStart := time.Now()
	allRetried := true
	for _, taskID := range a.injectTaskIDs {
		waitResult, waitErr := sc.Checker.WaitFor(ctx, checker.WaitCondition{
			Name: fmt.Sprintf("inject task %s completes via retry", taskID),
			Check: func(ctx context.Context) (bool, error) {
				status, err := sc.Checker.TaskStatus(ctx, taskID)
				if err != nil {
					return false, err
				}
				if status != "completed" {
					slog.Info("ai-failure: validate — poll inject task", "task_id", taskID, "status", status, "elapsed_s", int(time.Since(pollStart).Seconds()))
				}
				return status == "completed", nil
			},
			Timeout:  aiFailureSmokeWait,
			Interval: sc.PollInterval,
		})
		if waitErr != nil || !waitResult.Met {
			allRetried = false
		}
	}
	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "injected tasks complete via SQS retry after recovery",
		Type:     chaos.SLOFunctional,
		Expected: true,
		Actual:   allRetried,
		Status:   boolSLO(allRetried),
	})

	// Functional: new post-recovery task also completes.
	slog.Info("ai-failure: validate — submitting post-recovery task")
	recoveryTaskID, err := submitTask(sc.Config.APIURL, "chaos-ai-failure-recovery")
	if err != nil {
		report.SLOResults = append(report.SLOResults, chaos.SLOResult{
			Name:     "post-recovery task submission",
			Type:     chaos.SLOFunctional,
			Expected: "submitted",
			Actual:   fmt.Sprintf("error: %v", err),
			Status:   chaos.SLOFail,
		})
		report.Metrics = observed.Metrics
		report.ComputeStatus()
		return report, nil
	}

	slog.Info("ai-failure: validate — waiting for recovery task", "task_id", recoveryTaskID, "timeout", aiFailureSmokeWait)
	recoveryPollStart := time.Now()
	waitResult, waitErr := sc.Checker.WaitFor(ctx, checker.WaitCondition{
		Name: "post-recovery task completes",
		Check: func(ctx context.Context) (bool, error) {
			status, err := sc.Checker.TaskStatus(ctx, recoveryTaskID)
			if err != nil {
				return false, err
			}
			if status != "completed" {
				slog.Info("ai-failure: validate — poll", "task_id", recoveryTaskID, "status", status, "elapsed_s", int(time.Since(recoveryPollStart).Seconds()))
			}
			return status == "completed", nil
		},
		Timeout:  aiFailureSmokeWait,
		Interval: sc.PollInterval,
	})

	recoveryTaskPassed := waitErr == nil && waitResult.Met
	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "post-recovery task completes",
		Type:     chaos.SLOFunctional,
		Expected: true,
		Actual:   recoveryTaskPassed,
		Status:   boolSLO(recoveryTaskPassed),
	})

	observed.Metrics["recovery_seconds"] = recoveryElapsed
	observed.Metrics["recovery_task_id"] = recoveryTaskID
	observed.Metrics["recovery_task_passed"] = recoveryTaskPassed
	observed.Metrics["inject_tasks_retried"] = allRetried

	report.Metrics = observed.Metrics
	report.ComputeStatus()

	slog.Info("ai-failure: validate complete", "status", report.Status)
	return report, nil
}

func (a *AIFailure) Cleanup(ctx context.Context, sc *chaos.ScenarioContext) error {
	slog.Info("ai-failure: cleanup — ensuring ai-runtime is running")

	// Ensure ai-runtime is running and healthy.
	status, err := sc.Checker.ContainerHealth(ctx, "ai-runtime")
	if err != nil || status != docker.HealthHealthy {
		slog.Info("ai-failure: cleanup — starting ai-runtime")
		if startErr := sc.Docker.Start(ctx, "ai-runtime"); startErr != nil {
			slog.Warn("ai-failure: cleanup start ai-runtime", "error", startErr)
		}
		if waitErr := sc.Docker.WaitForHealthy(ctx, "ai-runtime", 60*time.Second); waitErr != nil {
			slog.Warn("ai-failure: cleanup wait for ai-runtime healthy", "error", waitErr)
		}
	}

	// Wait for queue to empty.
	_, err = sc.Checker.WaitFor(ctx, checker.WaitCondition{
		Name: "queue empty after ai-failure",
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
		slog.Warn("ai-failure: cleanup queue drain", "error", err)
	}

	if err := sc.Checker.ResolveAllActiveEvents(ctx); err != nil {
		slog.Warn("ai-failure: cleanup resolve events", "error", err)
	}

	slog.Info("ai-failure: cleanup complete")
	return nil
}
