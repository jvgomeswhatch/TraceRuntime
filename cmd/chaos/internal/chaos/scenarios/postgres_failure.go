package scenarios

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/runtime-platform/cmd/chaos/internal/chaos"
	"github.com/runtime-platform/cmd/chaos/internal/chaos/checker"
	"github.com/runtime-platform/cmd/chaos/internal/chaos/docker"
)

// PostgresFailure stops PostgreSQL and validates that API and worker degrade
// gracefully without crashing, then verifies automatic pool reconnection
// after restart.
type PostgresFailure struct{}

func NewPostgresFailure() *PostgresFailure {
	return &PostgresFailure{}
}

func (s *PostgresFailure) Name() string           { return "postgres-failure" }
func (s *PostgresFailure) Timeout() time.Duration { return 3 * time.Minute }

func (s *PostgresFailure) Setup(ctx context.Context, sc *chaos.ScenarioContext) error {
	slog.Info("postgres-failure: setup — verifying all containers healthy")

	required := []string{"api", "worker", "watchdog", "postgres", "localstack"}
	healthy, err := sc.Checker.AllContainersHealthy(ctx, required)
	if err != nil {
		return fmt.Errorf("health check: %w", err)
	}
	if !healthy {
		return fmt.Errorf("not all containers healthy")
	}

	slog.Info("postgres-failure: setup complete")
	return nil
}

func (s *PostgresFailure) Inject(ctx context.Context, sc *chaos.ScenarioContext) error {
	slog.Info("postgres-failure: inject — stopping postgres")

	if err := sc.Docker.Stop(ctx, "postgres"); err != nil {
		return fmt.Errorf("stop postgres: %w", err)
	}

	slog.Info("postgres-failure: postgres stopped")
	return nil
}

func (s *PostgresFailure) Observe(ctx context.Context, sc *chaos.ScenarioContext) (*chaos.ObserveResult, error) {
	slog.Info("postgres-failure: observe — waiting 10s then checking degradation behavior")

	observed := chaos.NewObserveResult()

	// Wait for services to notice postgres is gone
	time.Sleep(10 * time.Second)

	// Check API responds (GET /health) — should not crash/hang
	apiResponds := false
	apiStructuredStatus := false
	httpClient := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sc.Config.APIURL+"/health", nil)
	if err == nil {
		resp, err := httpClient.Do(req)
		if err == nil {
			defer resp.Body.Close()
			apiResponds = true
			// Structured status: 200 or 503, not a 500 panic
			apiStructuredStatus = resp.StatusCode == 200 || resp.StatusCode == 503
			observed.Metrics["api_health_status_code"] = resp.StatusCode
			slog.Info("postgres-failure: API health response",
				"status_code", resp.StatusCode,
				"responds", true,
				"structured", apiStructuredStatus,
			)
		} else {
			slog.Warn("postgres-failure: API health request failed", "error", err)
		}
	}
	observed.Metrics["api_responds"] = apiResponds
	observed.Metrics["api_structured_status"] = apiStructuredStatus

	// Check container health status and restart counts for api, worker, watchdog
	services := []string{"api", "worker", "watchdog"}
	restartCounts := make(map[string]int)
	containerAlive := make(map[string]bool)

	for _, svc := range services {
		health, err := sc.Checker.ContainerHealth(ctx, svc)
		if err != nil {
			slog.Warn("postgres-failure: container health error", "service", svc, "error", err)
			containerAlive[svc] = false
		} else {
			containerAlive[svc] = health != docker.HealthExited
		}

		count, err := sc.Checker.ContainerRestartCount(ctx, svc)
		if err != nil {
			slog.Warn("postgres-failure: restart count error", "service", svc, "error", err)
			restartCounts[svc] = -1
		} else {
			restartCounts[svc] = count
		}

		slog.Info("postgres-failure: service status",
			"service", svc,
			"alive", containerAlive[svc],
			"restart_count", restartCounts[svc],
		)
	}

	observed.Metrics["restart_counts"] = restartCounts
	observed.Metrics["containers_alive"] = containerAlive

	return observed, nil
}

func (s *PostgresFailure) Validate(ctx context.Context, sc *chaos.ScenarioContext, observed *chaos.ObserveResult) (*chaos.ScenarioReport, error) {
	report := chaos.NewScenarioReport(s.Name())

	apiResponds, _ := observed.Metrics["api_responds"].(bool)
	apiStructured, _ := observed.Metrics["api_structured_status"].(bool)
	restartCounts, _ := observed.Metrics["restart_counts"].(map[string]int)
	containersAlive, _ := observed.Metrics["containers_alive"].(map[string]bool)

	// Functional: API responds during postgres failure
	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "api_responds_during_failure",
		Type:     chaos.SLOFunctional,
		Expected: true,
		Actual:   apiResponds,
		Status:   boolSLO(apiResponds),
	})

	// Functional: API returns structured status (200 or 503, not 500 panic)
	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "api_structured_status",
		Type:     chaos.SLOFunctional,
		Expected: true,
		Actual:   apiStructured,
		Status:   boolSLO(apiStructured),
	})

	// Functional: no crash loops (restart_count == 0 for api, worker, watchdog)
	services := []string{"api", "worker", "watchdog"}
	for _, svc := range services {
		count := restartCounts[svc]
		report.SLOResults = append(report.SLOResults, chaos.SLOResult{
			Name:     fmt.Sprintf("%s_no_crash_loop", svc),
			Type:     chaos.SLOFunctional,
			Expected: 0,
			Actual:   count,
			Status:   intSLO(count, 0),
		})
	}

	// Functional: containers still alive (not exited) for worker, watchdog, api
	for _, svc := range services {
		alive := containersAlive[svc]
		report.SLOResults = append(report.SLOResults, chaos.SLOResult{
			Name:     fmt.Sprintf("%s_still_alive", svc),
			Type:     chaos.SLOFunctional,
			Expected: true,
			Actual:   alive,
			Status:   boolSLO(alive),
		})
	}

	// Restart postgres and verify reconnection
	slog.Info("postgres-failure: validate — restarting postgres")
	if err := sc.Docker.Start(ctx, "postgres"); err != nil {
		report.SLOResults = append(report.SLOResults, chaos.SLOResult{
			Name:     "postgres_restart",
			Type:     chaos.SLOFunctional,
			Expected: true,
			Actual:   false,
			Status:   chaos.SLOFail,
		})
		report.ComputeStatus()
		return report, nil
	}

	if err := sc.Docker.WaitForHealthy(ctx, "postgres", 30*time.Second); err != nil {
		slog.Warn("postgres-failure: postgres did not become healthy in 30s", "error", err)
	}

	// Functional: pgx pool reconnects (submit task succeeds within 30s)
	reconnectStart := time.Now()
	poolReconnected := false

	result, err := sc.Checker.WaitFor(ctx, checker.WaitCondition{
		Name: "pgx-reconnect",
		Check: func(ctx context.Context) (bool, error) {
			_, err := submitTask(sc.Config.APIURL, "chaos-postgres-reconnect-test")
			if err != nil {
				slog.Debug("postgres-failure: reconnect attempt failed", "error", err)
				return false, nil
			}
			return true, nil
		},
		Timeout:  30 * time.Second,
		Interval: 3 * time.Second,
	})
	if err == nil && result.Met {
		poolReconnected = true
	}
	reconnectTime := time.Since(reconnectStart).Seconds()

	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "pgx_pool_reconnects",
		Type:     chaos.SLOFunctional,
		Expected: true,
		Actual:   poolReconnected,
		Status:   boolSLO(poolReconnected),
	})

	// Timing: reconnection < 30s
	report.SLOResults = append(report.SLOResults, chaos.SLOResult{
		Name:     "reconnection_time",
		Type:     chaos.SLOTiming,
		Expected: 30.0,
		Actual:   reconnectTime,
		Status:   timingSLO(reconnectTime, 30.0),
	})

	report.Metrics["api_responds"] = apiResponds
	report.Metrics["api_structured_status"] = apiStructured
	report.Metrics["restart_counts"] = restartCounts
	report.Metrics["pool_reconnected"] = poolReconnected
	report.Metrics["reconnect_time_s"] = reconnectTime

	report.ComputeStatus()
	return report, nil
}

func (s *PostgresFailure) Cleanup(ctx context.Context, sc *chaos.ScenarioContext) error {
	slog.Info("postgres-failure: cleanup — ensuring postgres is running")

	// Make sure postgres is running and healthy
	pgStatus, err := sc.Checker.ContainerHealth(ctx, "postgres")
	if err != nil || pgStatus != docker.HealthHealthy {
		slog.Info("postgres-failure: cleanup — starting postgres")
		if startErr := sc.Docker.Start(ctx, "postgres"); startErr != nil {
			slog.Warn("postgres-failure: cleanup start failed", "error", startErr)
		}
		if err := sc.Docker.WaitForHealthy(ctx, "postgres", 30*time.Second); err != nil {
			slog.Warn("postgres-failure: cleanup — postgres not healthy", "error", err)
		}
	}

	// Wait for all services to be healthy
	allHealthy, err := sc.Checker.WaitFor(ctx, checker.WaitCondition{
		Name: "all-services-healthy",
		Check: func(ctx context.Context) (bool, error) {
			return sc.Checker.AllContainersHealthy(ctx, []string{"api", "worker", "watchdog", "postgres"})
		},
		Timeout:  60 * time.Second,
		Interval: 5 * time.Second,
	})
	if err != nil {
		slog.Warn("postgres-failure: cleanup — error waiting for healthy", "error", err)
	} else if !allHealthy.Met {
		slog.Warn("postgres-failure: cleanup — not all services healthy within 60s")
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
		slog.Warn("postgres-failure: queue drain error", "error", err)
	} else if !drainResult.Met {
		slog.Warn("postgres-failure: queue did not drain within 60s")
	}

	slog.Info("postgres-failure: cleanup complete")
	return nil
}
