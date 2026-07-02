package integration

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"
	"time"
)

func TestTraceDetail(t *testing.T) {
	cleanDB(t)
	purgeQueue(t, queueURL)
	purgeQueue(t, dlqURL)

	token := envOr("INTERNAL_TOKEN", "")

	t.Run("TraceDetailFound", func(t *testing.T) {
		taskID, traceID := createTask(t, "trace detail integration test")
		waitForTaskStatus(t, taskID, "completed", 30*time.Second)

		req, _ := http.NewRequest("GET", apiURL+"/api/traces/"+traceID, nil)
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
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}

		// trace_id in response must match the requested trace_id.
		if result["trace_id"] != traceID {
			t.Errorf("expected trace_id %q, got %v", traceID, result["trace_id"])
		}

		// task must be present and non-null.
		taskObj, ok := result["task"].(map[string]any)
		if !ok || taskObj == nil {
			t.Fatal("expected task to be a non-null object")
		}

		// task fields must be present.
		if taskObj["id"] == nil || taskObj["id"] == "" {
			t.Error("task.id is missing or empty")
		}
		if taskObj["trace_id"] != traceID {
			t.Errorf("task.trace_id %v does not match expected %q", taskObj["trace_id"], traceID)
		}
		if taskObj["status"] == nil || taskObj["status"] == "" {
			t.Error("task.status is missing or empty")
		}

		// task.id must match the task we created.
		if taskObj["id"] != taskID {
			t.Errorf("task.id %v does not match created taskID %q", taskObj["id"], taskID)
		}

		// Graceful degradation: no Tempo in CI, so tempo_available must be false
		// and spans must be null.
		tempoAvailable, ok := result["tempo_available"].(bool)
		if !ok {
			t.Fatal("tempo_available field missing or not a boolean")
		}
		if tempoAvailable {
			t.Log("tempo_available is true (Tempo is running); skipping null-spans assertion")
		} else {
			if result["spans"] != nil {
				t.Errorf("expected spans to be null when tempo_available is false, got %v", result["spans"])
			}
		}
	})

	t.Run("TraceDetailNotFound", func(t *testing.T) {
		req, _ := http.NewRequest("GET", apiURL+"/api/traces/00000000000000000000000000000000", nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 404 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 404, got %d: %s", resp.StatusCode, string(b))
		}
	})

	t.Run("TraceDetailInvalidID", func(t *testing.T) {
		req, _ := http.NewRequest("GET", apiURL+"/api/traces/not-a-valid-hex-id", nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != 400 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 400, got %d: %s", resp.StatusCode, string(b))
		}
	})
}
