package scenarios

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/runtime-platform/cmd/chaos/internal/chaos"
	"github.com/runtime-platform/cmd/chaos/internal/chaos/checker"
)

// submitTask sends a POST /tasks request and returns the created task_id.
func submitTask(apiURL, input string) (string, error) {
	body, err := json.Marshal(map[string]string{"input": input})
	if err != nil {
		return "", fmt.Errorf("marshal task input: %w", err)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Post(apiURL+"/tasks", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("post task: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("unexpected status %d from POST /tasks", resp.StatusCode)
	}

	var result struct {
		TaskID string `json:"task_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("decode task response: %w", err)
	}
	if result.TaskID == "" {
		return "", fmt.Errorf("empty task_id in response")
	}
	return result.TaskID, nil
}

// boolSLO returns SLOPass if actual is true, SLOFail otherwise.
func boolSLO(actual bool) chaos.SLOStatus {
	if actual {
		return chaos.SLOPass
	}
	return chaos.SLOFail
}

// intSLO returns SLOPass if actual equals expected, SLOFail otherwise.
func intSLO(actual, expected int) chaos.SLOStatus {
	if actual == expected {
		return chaos.SLOPass
	}
	return chaos.SLOFail
}

// strSLO returns SLOPass if actual equals expected, SLOFail otherwise.
func strSLO(actual, expected string) chaos.SLOStatus {
	if actual == expected {
		return chaos.SLOPass
	}
	return chaos.SLOFail
}

// timingSLO returns SLOPass if actual <= maxExpected, SLOFail otherwise.
func timingSLO(actual, maxExpected float64) chaos.SLOStatus {
	if actual <= maxExpected {
		return chaos.SLOPass
	}
	return chaos.SLOFail
}

// stabilizeSystem brings the system to a clean state after a chaos scenario.
// Every Cleanup() should call this as the final step to ensure the next
// scenario starts from a deterministic baseline.
func stabilizeSystem(ctx context.Context, scenario string, sc *chaos.ScenarioContext) {
	// 1. Fail any tasks stuck in processing.
	if failed, err := sc.Checker.FailStuckTasks(ctx, scenario+"-cleanup"); err != nil {
		slog.Warn(scenario+": cleanup fail stuck tasks", "error", err)
	} else if failed > 0 {
		slog.Info(scenario+": cleanup failed stuck tasks", "count", failed)
	}

	// 2. Purge DLQ (messages from failed/retried tasks).
	dlqDepth, _ := sc.Checker.DLQDepth(ctx)
	if dlqDepth > 0 {
		if err := sc.Checker.PurgeDLQ(ctx); err != nil {
			slog.Warn(scenario+": cleanup purge DLQ", "error", err)
		} else {
			slog.Info(scenario+": cleanup purged DLQ", "messages", dlqDepth)
		}
	}

	// 3. Delete stale heartbeats.
	if deleted, err := sc.Checker.DeleteStaleHeartbeats(ctx, 30*time.Second); err != nil {
		slog.Warn(scenario+": cleanup delete stale heartbeats", "error", err)
	} else if deleted > 0 {
		slog.Info(scenario+": cleanup deleted stale heartbeats", "count", deleted)
	}

	// 4. Resolve all active healing events.
	if err := sc.Checker.ResolveAllActiveEvents(ctx); err != nil {
		slog.Warn(scenario+": cleanup resolve events", "error", err)
	}

	// 5. Wait for watchdog to reconcile — zero active events in DB.
	_, err := sc.Checker.WaitFor(ctx, checker.WaitCondition{
		Name: "stabilize-zero-events",
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
		slog.Warn(scenario+": cleanup events not fully resolved", "error", err)
	}
}

// setChaosConfig sends a POST to /internal/chaos/config with the given delay.
func setChaosConfig(aiRuntimeURL, token string, delaySeconds int) error {
	body, err := json.Marshal(map[string]int{"delay_seconds": delaySeconds})
	if err != nil {
		return fmt.Errorf("marshal chaos config: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, aiRuntimeURL+"/internal/chaos/config", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create chaos config request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("X-Internal-Token", token)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post chaos config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("chaos config returned status %d", resp.StatusCode)
	}
	return nil
}

// resetChaosConfig sends a POST to /internal/chaos/reset to clear injected faults.
func resetChaosConfig(aiRuntimeURL, token string) error {
	req, err := http.NewRequest(http.MethodPost, aiRuntimeURL+"/internal/chaos/reset", nil)
	if err != nil {
		return fmt.Errorf("create chaos reset request: %w", err)
	}
	if token != "" {
		req.Header.Set("X-Internal-Token", token)
	}

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("post chaos reset: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("chaos reset returned status %d", resp.StatusCode)
	}
	return nil
}
