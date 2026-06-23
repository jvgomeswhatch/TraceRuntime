package integration

import (
	"net/http"
	"testing"
	"time"
)

func TestTaskFailure_AIRuntimeError(t *testing.T) {
	cleanDB(t)
	purgeQueue(t, queueURL)
	purgeQueue(t, dlqURL)

	taskID, _ := createTask(t, "trigger-failure")

	waitForTaskStatus(t, taskID, "failed", 60*time.Second)

	task := getTask(t, taskID)

	t.Run("error_message_is_descriptive", func(t *testing.T) {
		if task.ErrorMessage == "" {
			t.Fatal("error_message should be non-empty for failed task")
		}
		if len(task.ErrorMessage) < 5 {
			t.Errorf("error_message too short to be useful (got %q)", task.ErrorMessage)
		}
	})

	t.Run("no_artifact_created", func(t *testing.T) {
		if task.ArtifactKey != nil {
			t.Errorf("artifact_key should be nil for failed task, got %q", *task.ArtifactKey)
		}
	})

	t.Run("tokens_are_zero", func(t *testing.T) {
		if task.PromptTokens != 0 {
			t.Errorf("prompt_tokens should be 0 for failed task, got %d", task.PromptTokens)
		}
		if task.CompletionTokens != 0 {
			t.Errorf("completion_tokens should be 0 for failed task, got %d", task.CompletionTokens)
		}
		if task.TokensPerSecond != 0 {
			t.Errorf("tokens_per_second should be 0 for failed task, got %f", task.TokensPerSecond)
		}
	})

	t.Run("worker_still_healthy", func(t *testing.T) {
		resp, err := http.Get(apiURL + "/health")
		if err != nil {
			t.Fatalf("API health check failed: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("API should still be healthy after task failure, got %d", resp.StatusCode)
		}

		workerResp, err := http.Get("http://localhost:9091/health")
		if err != nil {
			t.Fatalf("worker health check failed: %v", err)
		}
		workerResp.Body.Close()
		if workerResp.StatusCode != 200 {
			t.Errorf("worker should still be healthy after task failure, got %d", workerResp.StatusCode)
		}
	})

	t.Run("multiple_failures_dont_crash", func(t *testing.T) {
		cleanDB(t)
		purgeQueue(t, queueURL)
		purgeQueue(t, dlqURL)

		const count = 3
		taskIDs := make([]string, count)
		for i := range taskIDs {
			id, _ := createTask(t, "trigger-failure")
			taskIDs[i] = id
		}

		for _, id := range taskIDs {
			waitForTaskStatus(t, id, "failed", 90*time.Second)
		}

		resp, err := http.Get(apiURL + "/health")
		if err != nil {
			t.Fatalf("API health check failed after multiple failures: %v", err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Errorf("API should still be healthy after multiple failures, got %d", resp.StatusCode)
		}

		workerResp, err := http.Get("http://localhost:9091/health")
		if err != nil {
			t.Fatalf("worker health check failed after multiple failures: %v", err)
		}
		workerResp.Body.Close()
		if workerResp.StatusCode != 200 {
			t.Errorf("worker should still be healthy after multiple failures, got %d", workerResp.StatusCode)
		}
	})
}
