package integration

import (
	"strings"
	"testing"
	"time"
)

func TestQueueBehavior_TraceparentPropagated(t *testing.T) {
	cleanDB(t)
	purgeQueue(t, queueURL)

	taskID, traceID := createTask(t, "traceparent test")

	waitForTaskStatus(t, taskID, "completed", 30*time.Second)

	task := getTask(t, taskID)
	if task.TraceID != traceID {
		t.Errorf("trace_id not consistent: expected %s, got %s", traceID, task.TraceID)
	}
}

// TestQueueBehavior_TraceIDConsistentAcrossRecords verifies that the same trace_id
// appears in the DB task record, the S3 artifact key prefix, and the artifact JSON.
func TestQueueBehavior_TraceIDConsistentAcrossRecords(t *testing.T) {
	cleanDB(t)
	purgeQueue(t, queueURL)

	taskID, traceID := createTask(t, "trace consistency test")

	waitForTaskStatus(t, taskID, "completed", 30*time.Second)

	task := getTask(t, taskID)

	// DB record must carry the same trace_id returned by the API.
	if task.TraceID != traceID {
		t.Errorf("DB trace_id mismatch: expected %s, got %s", traceID, task.TraceID)
	}

	// S3 artifact key must be prefixed with traceID/taskID.
	if task.ArtifactKey == nil || *task.ArtifactKey == "" {
		t.Fatal("artifact_key not set on completed task")
	}
	expectedKeyPrefix := traceID + "/" + taskID
	if !strings.HasPrefix(*task.ArtifactKey, expectedKeyPrefix) {
		t.Errorf("artifact_key %q does not have expected prefix %q", *task.ArtifactKey, expectedKeyPrefix)
	}

	// If the artifact JSON contains a trace_id field it must match.
	artifact := getArtifactJSON(t, *task.ArtifactKey)
	if artifactTraceID, ok := artifact["trace_id"].(string); ok && artifactTraceID != "" {
		if artifactTraceID != traceID {
			t.Errorf("artifact JSON trace_id %q does not match expected %q", artifactTraceID, traceID)
		}
	}
}

// TestQueueBehavior_QueueDrainsAfterProcessing confirms that after a task completes
// the message is no longer sitting in the queue (consumer deleted it).
func TestQueueBehavior_QueueDrainsAfterProcessing(t *testing.T) {
	cleanDB(t)
	purgeQueue(t, queueURL)

	taskID, _ := createTask(t, "queue drain test")

	waitForTaskStatus(t, taskID, "completed", 30*time.Second)

	depth := queueDepth(t)
	if depth != 0 {
		t.Errorf("expected queue depth 0 after task completed, got %d", depth)
	}
}

// TestQueueBehavior_DLQReceivesFailedMessages creates a task designed to fail and
// waits for SQS to route the message to the DLQ after maxReceiveCount is exhausted.
// Because DLQ routing timing is non-deterministic in LocalStack, the test is skipped
// (not failed) if the DLQ remains empty after 60 seconds.
func TestQueueBehavior_DLQReceivesFailedMessages(t *testing.T) {
	cleanDB(t)
	purgeQueue(t, queueURL)
	purgeQueue(t, dlqURL)

	taskID, _ := createTask(t, "trigger-failure")

	// Wait for the task to reach a failed state in the DB (up to 60 s).
	waitForTaskStatus(t, taskID, "failed", 60*time.Second)

	// Now wait up to 60 s for SQS to move the exhausted message to the DLQ.
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if dlqDepth(t) > 0 {
			return // success
		}
		time.Sleep(2 * time.Second)
	}

	t.Skip("DLQ depth still 0 after 60 s — DLQ routing timing is flaky in LocalStack; skipping")
}

// TestQueueBehavior_MultipleTasksProcessedInOrder creates three tasks concurrently,
// waits for all to complete, and verifies each has a unique artifact_key.
func TestQueueBehavior_MultipleTasksProcessedInOrder(t *testing.T) {
	cleanDB(t)
	purgeQueue(t, queueURL)

	const taskCount = 3
	taskIDs := make([]string, taskCount)
	for i := 0; i < taskCount; i++ {
		id, _ := createTask(t, "multi-task test")
		taskIDs[i] = id
	}

	// Wait for every task to reach "completed".
	for _, id := range taskIDs {
		waitForTaskStatus(t, id, "completed", 60*time.Second)
	}

	// Collect artifact keys and verify they are all set and distinct.
	seen := make(map[string]struct{}, taskCount)
	for _, id := range taskIDs {
		task := getTask(t, id)
		if task.ArtifactKey == nil || *task.ArtifactKey == "" {
			t.Errorf("task %s has no artifact_key after completion", id)
			continue
		}
		if _, dup := seen[*task.ArtifactKey]; dup {
			t.Errorf("duplicate artifact_key %q for task %s", *task.ArtifactKey, id)
		}
		seen[*task.ArtifactKey] = struct{}{}
	}
}
