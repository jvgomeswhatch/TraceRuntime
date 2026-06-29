package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestReplayTask(t *testing.T) {
	cleanDB(t)
	purgeQueue(t, queueURL)

	token := envOr("INTERNAL_TOKEN", "")

	taskID, _ := createTask(t, "test replay input")
	waitForTaskStatus(t, taskID, "completed", 60*time.Second)

	t.Run("ReplayCompleted", func(t *testing.T) {
		req, _ := http.NewRequest("POST", apiURL+"/api/tasks/"+taskID+"/replay", nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 201 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 201, got %d: %s", resp.StatusCode, string(b))
		}
		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		task := result["task"].(map[string]any)
		if task["id"] == taskID {
			t.Fatal("replay should create new task ID")
		}
		if task["replay_of"] != taskID {
			t.Fatalf("expected replay_of=%s, got %v", taskID, task["replay_of"])
		}
		if task["status"] != "pending" {
			t.Fatalf("expected pending, got %v", task["status"])
		}
	})

	t.Run("ReplayWithOverride", func(t *testing.T) {
		body := `{"override_payload": "new input"}`
		req, _ := http.NewRequest("POST", apiURL+"/api/tasks/"+taskID+"/replay", strings.NewReader(body))
		req.Header.Set("X-Internal-Token", token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 201 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 201, got %d: %s", resp.StatusCode, string(b))
		}
		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		task := result["task"].(map[string]any)
		if task["input_payload"] != "new input" {
			t.Fatalf("expected override payload, got %v", task["input_payload"])
		}
	})

	t.Run("ReplayNotFound", func(t *testing.T) {
		req, _ := http.NewRequest("POST", apiURL+"/api/tasks/00000000-0000-0000-0000-000000000000/replay", nil)
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

	t.Run("ReplayInProgress", func(t *testing.T) {
		newTaskID, _ := createTask(t, "in progress task")
		time.Sleep(500 * time.Millisecond)

		var status string
		ctx := context.Background()
		dbPool.QueryRow(ctx, "SELECT status FROM tasks WHERE id = $1", newTaskID).Scan(&status)
		if status == "completed" || status == "failed" {
			t.Skip("task completed too fast for in-progress test")
		}

		req, _ := http.NewRequest("POST", apiURL+"/api/tasks/"+newTaskID+"/replay", nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 409 {
			t.Fatalf("expected 409, got %d", resp.StatusCode)
		}
	})

	t.Run("ReplayNoPayload", func(t *testing.T) {
		ctx := context.Background()
		_, err := dbPool.Exec(ctx,
			`INSERT INTO tasks (id, trace_id, status, created_at, updated_at)
			 VALUES ('aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee', 'deadbeef', 'completed', NOW(), NOW())`)
		if err != nil {
			t.Fatalf("insert old task: %v", err)
		}

		req, _ := http.NewRequest("POST", apiURL+"/api/tasks/aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee/replay", nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 422 {
			t.Fatalf("expected 422, got %d", resp.StatusCode)
		}
	})
}
