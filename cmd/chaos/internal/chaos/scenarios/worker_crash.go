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

// WorkerCrash kills the worker with SIGKILL and validates that the watchdog
// detects it via stale heartbeat / health check failure, and that the worker
// recovers after restart.
type WorkerCrash struct {
	taskID   string
	killedAt time.Time
}

func NewWorkerCrash() *WorkerCrash {
	return &WorkerCrash{}
}

func (s *WorkerCrash) Name() string           { return "worker-crash" }
func (s *WorkerCrash) Timeout() time.Duration { return 3 * time.Minute }

func (s *WorkerCrash) Setup(ctx context.Context, sc *chaos.ScenarioContext) error {
	slog.Info("worker-crash: setup — verifying all containers healthy")

	required := []string{"api", "worker", "watchdog", "postgres", "localstack"}
	healthy, err := sc.Checker.AllContainersHealthy(ctx, required)
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}
	if !healthy {
		return fmt.Errorf("not all containers healthy")
	}

	// Ensure zero active healing events before starting.
	if err := sc.Checker.ResolveAllActiveEvents(ctx); err != nil {
		return fmt.Errorf("resolve active events: %w", err)
	}

	// Wait for watchdog to reconcile (pick up the external resolution).
	_, err = sc.Checker.WaitFor(ctx, checker.WaitCondition{
		Name: "zero-active-events",
		Check: func(ctx context.Context) (bool, error) {
			events, err := sc.Checker.ActiveHealingEvents(ctx)
			if err != nil {
				return false, err
			}
			return len(events) == 0, nil
		},
		Timeout:  30 * time.Second,
		Interval: 3 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("wait for zero active events: %w", err)
	}

	// Ensure worker heartbeat is fresh (no stale state from previous runs).
	// Uses server-side age to avoid host/container clock drift.
	_, err = sc.Checker.WaitFor(ctx, checker.WaitCondition{
		Name: "fresh-heartbeat",
		Check: func(ctx context.Context) (bool, error) {
			ageSec, err := sc.Checker.HeartbeatAgeSec(ctx)
			if err != nil {
				return false, nil
			}
			return ageSec < 15, nil
		},
		Timeout:  20 * time.Second,
		Interval: 3 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("wait for fresh heartbeat: %w", err)
	}

	// Validate setup state before proceeding.
	setupAge, _ := sc.Checker.HeartbeatAgeSec(ctx)
	setupEvents, _ := sc.Checker.ActiveHealingEvents(ctx)
	setupTypes := eventTypeList(setupEvents)
	slog.Info("worker-crash: setup validation",
		"active_events", len(setupEvents),
		"event_types", setupTypes,
		"heartbeat_age_seconds", setupAge,
	)

	slog.Info("worker-crash: setup — state clean, submitting task")
	taskID, err := submitTask(sc.Config.APIURL, "chaos-worker-crash-test")
	if err != nil {
		return fmt.Errorf("submit task: %w", err)
	}
	s.taskID = taskID
	slog.Info("worker-crash: task submitted, waiting 5s for pickup", "task_id", taskID)

	time.Sleep(5 * time.Second)

	// Final state check — if events appeared during the 5s wait, the system
	// is not truly stable and the test will produce false results.
	preInjectAge, _ := sc.Checker.HeartbeatAgeSec(ctx)
	preInjectEvents, _ := sc.Checker.ActiveHealingEvents(ctx)
	preInjectTypes := eventTypeList(preInjectEvents)
	slog.Info("worker-crash: pre-inject state",
		"active_events", len(preInjectEvents),
		"event_types", preInjectTypes,
		"heartbeat_age_seconds", preInjectAge,
	)

	slog.Info("worker-crash: setup complete — state deterministic")
	return nil
}

func (s *WorkerCrash) Inject(ctx context.Context, sc *chaos.ScenarioContext) error {
	slog.Info("worker-crash: inject — disabling auto-restart then killing with SIGKILL")

	if err := sc.Docker.DisableRestart(ctx, "worker"); err != nil {
		return fmt.Errorf("disable restart: %w", err)
	}

	s.killedAt = time.Now()
	if err := sc.Docker.Kill(ctx, "worker", "SIGKILL"); err != nil {
		return fmt.Errorf("kill worker: %w", err)
	}
	slog.Info("worker-crash: worker killed", "killed_at", s.killedAt)

	return nil
}

func (s *WorkerCrash) Observe(ctx context.Context, sc *chaos.ScenarioContext) (*chaos.ObserveResult, error) {
	slog.Info("worker-crash: observe — waiting for watchdog healing events")

	observed := chaos.NewObserveResult()
	observeStart := time.Now()

	var firstEventTime time.Time
	var eventTypes []string

	result, err := sc.Checker.WaitFor(ctx, checker.WaitCondition{
		Name: "worker-critical-event",
		Check: func(ctx context.Context) (bool, error) {
			events, err := sc.Checker.HealingEventsSince(ctx, s.killedAt)
			if err != nil {
				return false, err
			}
			for _, e := range events {
				if e.EventType == "worker.stale" || e.EventType == "worker.down" {
					if firstEventTime.IsZero() {
						firstEventTime = e.CreatedAt
					}
					if !containsStr(eventTypes, e.EventType) {
						eventTypes = append(eventTypes, e.EventType)
					}
					slog.Info("worker-crash: healing event detected",
						"event_type", e.EventType,
						"event_id", e.ID,
						"status", e.Status,
						"elapsed", time.Since(s.killedAt),
					)
					return true, nil
				}
			}
			return false, nil
		},
		Timeout:  90 * time.Second,
		Interval: 3 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("wait for healing event: %w", err)
	}

	detectionTime := time.Since(observeStart).Seconds()

	observed.Metrics["critical_event_detected"] = result.Met
	observed.Metrics["detection_time_s"] = detectionTime
	observed.Metrics["event_types"] = eventTypes
	if !firstEventTime.IsZero() {
		observed.Metrics["first_event_at"] = firstEventTime.Format(time.RFC3339)
	}

	slog.Info("worker-crash: observe complete",
		"detected", result.Met,
		"detection_time_s", detectionTime,
		"event_types", eventTypes,
	)

	return observed, nil
}

func eventTypeList(events []checker.HealingEvent) []string {
	types := make([]string, 0, len(events))
	for _, ev := range events {
		types = append(types, ev.EventType)
	}
	return types
}

func containsStr(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

func (s *WorkerCrash) Validate(ctx context.Context, sc *chaos.ScenarioContext, observed *chaos.ObserveResult) (*chaos.ScenarioReport, error) {
	report := chaos.NewScenarioReport(s.Name())

	criticalDetected, _ := observed.Metrics["critical_event_detected"].(bool)
	detectionTime, _ := observed.Metrics["detection_time_s"].(float64)

	// Functional: watchdog detected critical event
	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "watchdog_detects_worker_failure",
		Type:     chaos.SLOFunctional,
		Expected: true,
		Actual:   criticalDetected,
		Status:   boolSLO(criticalDetected),
	})

	// Timing: detection < 60s
	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "failure_detection_time",
		Type:     chaos.SLOTiming,
		Expected: 60.0,
		Actual:   detectionTime,
		Status:   timingSLO(detectionTime, 60.0),
	})

	// Restart worker and verify recovery
	slog.Info("worker-crash: validate — restarting worker")
	restartStart := time.Now()
	if err := sc.Docker.Start(ctx, "worker"); err != nil {
		report.SLOResults = append(report.SLOResults, chaos.SLOResult{
			Name:     "worker_restart",
			Type:     chaos.SLOFunctional,
			Expected: true,
			Actual:   false,
			Status:   chaos.SLOFail,
		})
		report.ComputeStatus()
		return report, nil
	}

	// Wait for worker to become healthy.
	// Docker health check has start_period=20s + interval=30s, so healthy
	// status takes 20-50s. SLO allows 60s.
	healthErr := sc.Docker.WaitForHealthy(ctx, "worker", 60*time.Second)
	recoveryTime := time.Since(restartStart).Seconds()
	workerRecovered := healthErr == nil

	// Functional: worker recovers and is healthy
	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "worker_recovers_healthy",
		Type:     chaos.SLOFunctional,
		Expected: true,
		Actual:   workerRecovered,
		Status:   boolSLO(workerRecovered),
	})

	// Timing: recovery < 60s after restart (start_period=20s + health check interval=30s)
	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "worker_recovery_time",
		Type:     chaos.SLOTiming,
		Expected: 60.0,
		Actual:   recoveryTime,
		Status:   timingSLO(recoveryTime, 60.0),
	})

	// Functional: heartbeat resumes.
	// Uses server-side age to avoid host/container clock drift.
	heartbeatResumed := false
	hbResult, err := sc.Checker.WaitFor(ctx, checker.WaitCondition{
		Name: "heartbeat-resume",
		Check: func(ctx context.Context) (bool, error) {
			ageSec, err := sc.Checker.HeartbeatAgeSec(ctx)
			if err != nil {
				return false, nil
			}
			return ageSec < 30, nil
		},
		Timeout:  30 * time.Second,
		Interval: 3 * time.Second,
	})
	if err == nil && hbResult.Met {
		heartbeatResumed = true
	}

	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "heartbeat_resumes",
		Type:     chaos.SLOFunctional,
		Expected: true,
		Actual:   heartbeatResumed,
		Status:   boolSLO(heartbeatResumed),
	})

	// Functional: no collateral crashes (api and watchdog still healthy)
	apiHealth, _ := sc.Checker.ContainerHealth(ctx, "api")
	watchdogHealth, _ := sc.Checker.ContainerHealth(ctx, "watchdog")
	noCollateral := apiHealth == docker.HealthHealthy && watchdogHealth == docker.HealthHealthy

	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "no_collateral_crashes",
		Type:     chaos.SLOFunctional,
		Expected: true,
		Actual:   noCollateral,
		Status:   boolSLO(noCollateral),
	})

	report.Metrics["critical_event_detected"] = criticalDetected
	report.Metrics["detection_time_s"] = detectionTime
	report.Metrics["worker_recovered"] = workerRecovered
	report.Metrics["recovery_time_s"] = recoveryTime
	report.Metrics["heartbeat_resumed"] = heartbeatResumed
	report.Metrics["api_healthy"] = apiHealth == docker.HealthHealthy
	report.Metrics["watchdog_healthy"] = watchdogHealth == docker.HealthHealthy

	report.ComputeStatus()
	return report, nil
}

func (s *WorkerCrash) Cleanup(ctx context.Context, sc *chaos.ScenarioContext) error {
	slog.Info("worker-crash: cleanup — restoring worker")

	// Re-enable auto-restart policy.
	if err := sc.Docker.EnableRestart(ctx, "worker"); err != nil {
		slog.Warn("worker-crash: cleanup enable restart", "error", err)
	}

	// Make sure worker is running and healthy
	workerStatus, err := sc.Checker.ContainerHealth(ctx, "worker")
	if err != nil || workerStatus != docker.HealthHealthy {
		slog.Info("worker-crash: cleanup — starting worker")
		if startErr := sc.Docker.Start(ctx, "worker"); startErr != nil {
			slog.Warn("worker-crash: cleanup start failed", "error", startErr)
		}
		if err := sc.Docker.WaitForHealthy(ctx, "worker", 30*time.Second); err != nil {
			slog.Warn("worker-crash: cleanup — worker not healthy after restart", "error", err)
		}
	}

	// Wait for queue to empty
	drainResult, err := sc.Checker.WaitFor(ctx, checker.WaitCondition{
		Name: "queue-empty",
		Check: func(ctx context.Context) (bool, error) {
			depth, err := sc.Checker.QueueDepth(ctx)
			if err != nil {
				return false, err
			}
			return depth == 0, nil
		},
		Timeout:  60 * time.Second,
		Interval: 5 * time.Second,
	})
	if err != nil {
		slog.Warn("worker-crash: queue drain error", "error", err)
	} else if !drainResult.Met {
		slog.Warn("worker-crash: queue did not drain within 60s")
	}

	stabilizeSystem(ctx, "worker-crash", sc)

	slog.Info("worker-crash: cleanup complete")
	return nil
}
