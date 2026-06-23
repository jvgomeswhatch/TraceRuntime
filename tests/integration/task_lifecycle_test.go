package integration

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestTaskLifecycle_HappyPath(t *testing.T) {
	cleanDB(t)
	purgeQueue(t, queueURL)
	purgeQueue(t, dlqURL)

	taskID, traceID := createTask(t, "integration test prompt")

	waitForTaskStatus(t, taskID, "completed", 30*time.Second)

	task := getTask(t, taskID)

	if task.TraceID != traceID {
		t.Errorf("trace_id mismatch: API returned %q, DB has %q", traceID, task.TraceID)
	}

	if task.ArtifactKey == nil || *task.ArtifactKey == "" {
		t.Fatal("artifact_key not set on completed task")
	}

	expectedKeyPrefix := traceID + "/" + taskID
	if !strings.HasPrefix(*task.ArtifactKey, expectedKeyPrefix) {
		t.Errorf("artifact_key %q does not match expected prefix %q", *task.ArtifactKey, expectedKeyPrefix)
	}

	artifact := getArtifactJSON(t, *task.ArtifactKey)
	if output, ok := artifact["output"].(string); !ok || output == "" {
		t.Error("artifact output is empty")
	}

	if task.PromptTokens <= 0 {
		t.Error("prompt_tokens not populated")
	}
	if task.CompletionTokens <= 0 {
		t.Error("completion_tokens not populated")
	}
	if task.TokensPerSecond <= 0 {
		t.Error("tokens_per_second not populated")
	}

	if task.ProcessingStartedAt == nil {
		t.Fatal("processing_started_at not set")
	}
	if task.CompletedAt == nil {
		t.Fatal("completed_at not set")
	}

	if !task.CreatedAt.Before(*task.ProcessingStartedAt) {
		t.Error("created_at should be before processing_started_at")
	}
	if !task.ProcessingStartedAt.Before(*task.CompletedAt) {
		t.Error("processing_started_at should be before completed_at")
	}

	t.Run("artifact_content_matches_db", func(t *testing.T) {
		artifactTaskID, ok := artifact["task_id"].(string)
		if !ok || artifactTaskID == "" {
			t.Fatal("artifact missing task_id field")
		}
		if artifactTaskID != task.ID {
			t.Errorf("artifact task_id %q does not match DB task id %q", artifactTaskID, task.ID)
		}

		artifactTraceID, ok := artifact["trace_id"].(string)
		if !ok || artifactTraceID == "" {
			t.Fatal("artifact missing trace_id field")
		}
		if artifactTraceID != task.TraceID {
			t.Errorf("artifact trace_id %q does not match DB trace_id %q", artifactTraceID, task.TraceID)
		}
	})

	t.Run("s3_object_exists", func(t *testing.T) {
		raw := getArtifact(t, *task.ArtifactKey)
		if len(raw) == 0 {
			t.Error("S3 artifact raw bytes are empty")
		}
	})

	t.Run("multiple_tasks_independent", func(t *testing.T) {
		const n = 3
		type result struct {
			taskID      string
			traceID     string
			artifactKey string
		}

		results := make([]result, n)
		for i := 0; i < n; i++ {
			tid, trid := createTask(t, fmt.Sprintf("independent task %d", i+1))
			results[i] = result{taskID: tid, traceID: trid}
		}

		for i := range results {
			waitForTaskStatus(t, results[i].taskID, "completed", 30*time.Second)
			rec := getTask(t, results[i].taskID)
			if rec.ArtifactKey == nil || *rec.ArtifactKey == "" {
				t.Errorf("task %d (%s): artifact_key not set", i+1, results[i].taskID)
				continue
			}
			results[i].artifactKey = *rec.ArtifactKey
		}

		// All task IDs must be unique (sanity check).
		taskIDs := make(map[string]bool, n)
		traceIDs := make(map[string]bool, n)
		artifactKeys := make(map[string]bool, n)

		for i, r := range results {
			if taskIDs[r.taskID] {
				t.Errorf("task %d: duplicate task_id %q", i+1, r.taskID)
			}
			taskIDs[r.taskID] = true

			if traceIDs[r.traceID] {
				t.Errorf("task %d: duplicate trace_id %q", i+1, r.traceID)
			}
			traceIDs[r.traceID] = true

			if r.artifactKey != "" {
				if artifactKeys[r.artifactKey] {
					t.Errorf("task %d: duplicate artifact_key %q", i+1, r.artifactKey)
				}
				artifactKeys[r.artifactKey] = true
			}
		}
	})
}

func TestTaskLifecycle_InitialStatus(t *testing.T) {
	cleanDB(t)
	purgeQueue(t, queueURL)
	purgeQueue(t, dlqURL)

	taskID, _ := createTask(t, "initial status check")

	// Read the DB immediately, before the worker has had time to complete the task.
	task := getTask(t, taskID)

	if task.Status != "pending" && task.Status != "processing" {
		t.Errorf("expected initial status to be %q or %q, got %q", "pending", "processing", task.Status)
	}
}
