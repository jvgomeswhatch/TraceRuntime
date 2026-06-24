package scenarios

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/runtime-platform/cmd/chaos/internal/chaos"
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
