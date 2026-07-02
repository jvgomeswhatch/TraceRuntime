package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChaosEndpoints(t *testing.T) {
	token := envOr("INTERNAL_TOKEN", "")
	resultsDir := envOr("CAPACITY_RESULTS_DIR", "../../results")

	fixture := map[string]any{
		"version":          "1.0",
		"run_id":           "chaos-suite-test-fixture",
		"git_commit":       "abc1234",
		"environment":      "test",
		"timestamp":        "2024-01-15T14:35:00Z",
		"duration_seconds": 61,
		"summary":          map[string]int{"total": 1, "passed": 1, "warned": 0, "failed": 0},
		"scenarios": []map[string]any{
			{
				"name":             "worker-crash",
				"status":           "PASS",
				"duration_seconds": 61,
				"stages": map[string]any{
					"setup":    map[string]any{"duration_ms": 5000, "status": "ok"},
					"inject":   map[string]any{"duration_ms": 1000, "status": "ok"},
					"observe":  map[string]any{"duration_ms": 45000, "status": "ok"},
					"validate": map[string]any{"duration_ms": 8000, "status": "ok"},
					"cleanup":  map[string]any{"duration_ms": 2000, "status": "ok"},
				},
				"metrics":     map[string]any{"detection_time_seconds": 3, "recovery_time_seconds": 58},
				"slo_results": []map[string]any{{"name": "detection_under_60s", "type": "timing", "expected": 60, "actual": 3, "status": "PASS"}},
				"warnings":    []string{},
			},
		},
	}

	fixtureData, _ := json.MarshalIndent(fixture, "", "  ")
	fixturePath := filepath.Join(resultsDir, "chaos-suite-test-fixture.json")
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatalf("create results dir: %v", err)
	}
	if err := os.WriteFile(fixturePath, fixtureData, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	t.Cleanup(func() { os.Remove(fixturePath) })

	t.Run("ListReports", func(t *testing.T) {
		req, _ := http.NewRequest("GET", apiURL+"/api/chaos/reports", nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
		}
		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		reports, ok := result["reports"].([]any)
		if !ok {
			t.Fatal("reports is not an array")
		}
		found := false
		for _, r := range reports {
			entry, ok := r.(map[string]any)
			if ok && entry["id"] == "chaos-suite-test-fixture" {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("fixture report not found in list")
		}
	})

	t.Run("GetReport", func(t *testing.T) {
		req, _ := http.NewRequest("GET", apiURL+"/api/chaos/reports/chaos-suite-test-fixture", nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
		}
		var report map[string]any
		json.NewDecoder(resp.Body).Decode(&report)
		if report["run_id"] != "chaos-suite-test-fixture" {
			t.Fatalf("expected run_id=chaos-suite-test-fixture, got %v", report["run_id"])
		}
		scenarios, ok := report["scenarios"].([]any)
		if !ok || len(scenarios) != 1 {
			t.Fatalf("expected 1 scenario, got %v", report["scenarios"])
		}
	})

	t.Run("GetReportNotFound", func(t *testing.T) {
		req, _ := http.NewRequest("GET", apiURL+"/api/chaos/reports/nonexistent-report", nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("PathTraversalBlocked", func(t *testing.T) {
		paths := []string{
			"/api/chaos/reports/..%2F..%2Fetc%2Fpasswd",
			"/api/chaos/reports/../../etc/passwd",
		}
		for _, p := range paths {
			req, _ := http.NewRequest("GET", apiURL+p, nil)
			req.Header.Set("X-Internal-Token", token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != 400 && resp.StatusCode != 404 {
				t.Fatalf("expected 400 or 404 for path traversal, got %d on %s", resp.StatusCode, p)
			}
		}
	})

	t.Run("TriggerScenario", func(t *testing.T) {
		requestDir := filepath.Join(resultsDir, "chaos-requests")
		os.MkdirAll(requestDir, 0o755)
		t.Cleanup(func() {
			files, _ := filepath.Glob(filepath.Join(requestDir, "chaos-worker-crash-*.json"))
			for _, f := range files {
				os.Remove(f)
			}
		})

		body := `{"scenario":"worker-crash","timeout_seconds":60}`
		req, _ := http.NewRequest("POST", apiURL+"/api/chaos/trigger", strings.NewReader(body))
		req.Header.Set("X-Internal-Token", token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 202 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 202, got %d: %s", resp.StatusCode, string(b))
		}
		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		if result["status"] != "queued" {
			t.Fatalf("expected status=queued, got %v", result["status"])
		}
		runID, ok := result["run_id"].(string)
		if !ok || !strings.HasPrefix(runID, "chaos-worker-crash-") {
			t.Fatalf("expected run_id starting with chaos-worker-crash-, got %v", result["run_id"])
		}

		reqFile := filepath.Join(requestDir, runID+".json")
		if _, err := os.Stat(reqFile); os.IsNotExist(err) {
			t.Fatal("request file not created")
		}
	})

	t.Run("TriggerInvalidScenario", func(t *testing.T) {
		body := `{"scenario":"drop-database"}`
		req, _ := http.NewRequest("POST", apiURL+"/api/chaos/trigger", strings.NewReader(body))
		req.Header.Set("X-Internal-Token", token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("StatusQueued", func(t *testing.T) {
		requestDir := filepath.Join(resultsDir, "chaos-requests")
		os.MkdirAll(requestDir, 0o755)
		testRunID := "chaos-status-test-queued"
		os.WriteFile(filepath.Join(requestDir, testRunID+".json"), []byte(`{}`), 0o644)
		t.Cleanup(func() { os.Remove(filepath.Join(requestDir, testRunID+".json")) })

		req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/chaos/status?run_id=%s", apiURL, testRunID), nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
		}
		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		if result["status"] != "queued" {
			t.Fatalf("expected status=queued, got %v", result["status"])
		}
	})

	t.Run("StatusCompleted", func(t *testing.T) {
		req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/chaos/status?run_id=chaos-suite-test-fixture", apiURL), nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
		}
		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		if result["status"] != "completed" {
			t.Fatalf("expected status=completed, got %v", result["status"])
		}
	})

	t.Run("StatusNotFound", func(t *testing.T) {
		req, _ := http.NewRequest("GET", apiURL+"/api/chaos/status?run_id=nonexistent-run", nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
	})
}
