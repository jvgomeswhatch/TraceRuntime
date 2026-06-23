package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
	sqssdk "github.com/aws/aws-sdk-go-v2/service/sqs"
)

func TestIdempotency_DuplicateMessageNotReprocessed(t *testing.T) {
	cleanDB(t)
	purgeQueue(t, queueURL)
	purgeQueue(t, dlqURL)

	taskID, traceID := createTask(t, "idempotency test")
	waitForTaskStatus(t, taskID, "completed", 30*time.Second)

	taskBefore := getTask(t, taskID)

	duplicateBody, _ := json.Marshal(map[string]string{
		"task_id":  taskID,
		"trace_id": traceID,
		"input":    "idempotency test",
	})
	_, err := sqsClient.SendMessage(context.Background(), &sqssdk.SendMessageInput{
		QueueUrl:    &queueURL,
		MessageBody: aws.String(string(duplicateBody)),
	})
	if err != nil {
		t.Fatalf("failed to send duplicate message: %v", err)
	}

	time.Sleep(5 * time.Second)

	taskAfter := getTask(t, taskID)

	if taskAfter.Status != "completed" {
		t.Errorf("task status changed after duplicate: %s", taskAfter.Status)
	}

	if taskAfter.CompletionTokens != taskBefore.CompletionTokens {
		t.Errorf("completion_tokens changed: before=%d, after=%d",
			taskBefore.CompletionTokens, taskAfter.CompletionTokens)
	}

	if taskBefore.ArtifactKey != nil && taskAfter.ArtifactKey != nil {
		if *taskAfter.ArtifactKey != *taskBefore.ArtifactKey {
			t.Errorf("artifact_key changed: before=%s, after=%s",
				*taskBefore.ArtifactKey, *taskAfter.ArtifactKey)
		}
	}

	depth := dlqDepth(t)
	_ = depth

	fmt.Printf("  idempotency: task remained %s, tokens unchanged, artifact unchanged\n", taskAfter.Status)
}

// TestIdempotency_ArtifactNotDuplicated verifies that replaying a duplicate SQS
// message does not create additional S3 objects under the trace_id prefix.
func TestIdempotency_ArtifactNotDuplicated(t *testing.T) {
	cleanDB(t)
	purgeQueue(t, queueURL)
	purgeQueue(t, dlqURL)

	taskID, traceID := createTask(t, "artifact dedup test")
	waitForTaskStatus(t, taskID, "completed", 30*time.Second)

	// Count S3 objects before sending the duplicate.
	listBefore, err := s3Client.ListObjectsV2(context.Background(), &s3sdk.ListObjectsV2Input{
		Bucket: aws.String("traceruntime-outputs"),
		Prefix: aws.String(traceID + "/"),
	})
	if err != nil {
		t.Fatalf("failed to list S3 objects before duplicate: %v", err)
	}
	countBefore := 0
	if listBefore.KeyCount != nil {
		countBefore = int(*listBefore.KeyCount)
	}
	if countBefore == 0 {
		t.Fatalf("expected at least 1 S3 artifact for trace_id %s before duplicate, got 0", traceID)
	}

	// Send a duplicate message.
	duplicateBody, _ := json.Marshal(map[string]string{
		"task_id":  taskID,
		"trace_id": traceID,
		"input":    "artifact dedup test",
	})
	_, err = sqsClient.SendMessage(context.Background(), &sqssdk.SendMessageInput{
		QueueUrl:    &queueURL,
		MessageBody: aws.String(string(duplicateBody)),
	})
	if err != nil {
		t.Fatalf("failed to send duplicate message: %v", err)
	}

	// Give the worker time to (not) process the duplicate.
	time.Sleep(5 * time.Second)

	// Count S3 objects after the duplicate.
	listAfter, err := s3Client.ListObjectsV2(context.Background(), &s3sdk.ListObjectsV2Input{
		Bucket: aws.String("traceruntime-outputs"),
		Prefix: aws.String(traceID + "/"),
	})
	if err != nil {
		t.Fatalf("failed to list S3 objects after duplicate: %v", err)
	}
	countAfter := 0
	if listAfter.KeyCount != nil {
		countAfter = int(*listAfter.KeyCount)
	}

	if countAfter != countBefore {
		t.Errorf("S3 artifact count changed after duplicate: before=%d, after=%d (prefix=%s/)",
			countBefore, countAfter, traceID)
	} else {
		fmt.Printf("  artifact dedup: %d S3 object(s) under prefix %s/ — unchanged after duplicate\n",
			countAfter, traceID)
	}
}

// TestIdempotency_TimestampsUnchanged verifies that CompletedAt is not updated
// when the worker receives and ignores a duplicate SQS message.
func TestIdempotency_TimestampsUnchanged(t *testing.T) {
	cleanDB(t)
	purgeQueue(t, queueURL)
	purgeQueue(t, dlqURL)

	taskID, traceID := createTask(t, "timestamp dedup test")
	waitForTaskStatus(t, taskID, "completed", 30*time.Second)

	taskBefore := getTask(t, taskID)
	if taskBefore.CompletedAt == nil {
		t.Fatal("completed_at is nil before duplicate — task not fully completed")
	}
	completedAtBefore := *taskBefore.CompletedAt

	// Send duplicate.
	duplicateBody, _ := json.Marshal(map[string]string{
		"task_id":  taskID,
		"trace_id": traceID,
		"input":    "timestamp dedup test",
	})
	_, err := sqsClient.SendMessage(context.Background(), &sqssdk.SendMessageInput{
		QueueUrl:    &queueURL,
		MessageBody: aws.String(string(duplicateBody)),
	})
	if err != nil {
		t.Fatalf("failed to send duplicate message: %v", err)
	}

	time.Sleep(5 * time.Second)

	taskAfter := getTask(t, taskID)
	if taskAfter.CompletedAt == nil {
		t.Fatal("completed_at is nil after duplicate")
	}
	completedAtAfter := *taskAfter.CompletedAt

	if !completedAtAfter.Equal(completedAtBefore) {
		t.Errorf("completed_at changed after duplicate: before=%v, after=%v",
			completedAtBefore, completedAtAfter)
	} else {
		fmt.Printf("  timestamp dedup: completed_at unchanged (%v)\n", completedAtAfter)
	}
}

// TestIdempotency_WorkerStableAfterDuplicate verifies that the worker process
// remains healthy (HTTP 200 on /health) after processing a duplicate message.
func TestIdempotency_WorkerStableAfterDuplicate(t *testing.T) {
	cleanDB(t)
	purgeQueue(t, queueURL)
	purgeQueue(t, dlqURL)

	taskID, traceID := createTask(t, "worker stability test")
	waitForTaskStatus(t, taskID, "completed", 30*time.Second)

	// Send duplicate.
	duplicateBody, _ := json.Marshal(map[string]string{
		"task_id":  taskID,
		"trace_id": traceID,
		"input":    "worker stability test",
	})
	_, err := sqsClient.SendMessage(context.Background(), &sqssdk.SendMessageInput{
		QueueUrl:    &queueURL,
		MessageBody: aws.String(string(duplicateBody)),
	})
	if err != nil {
		t.Fatalf("failed to send duplicate message: %v", err)
	}

	// Allow the worker time to consume (and ignore) the duplicate.
	time.Sleep(5 * time.Second)

	workerResp, err := http.Get("http://localhost:9091/health")
	if err != nil {
		t.Fatalf("worker health check failed after duplicate: %v", err)
	}
	workerResp.Body.Close()
	if workerResp.StatusCode != http.StatusOK {
		t.Errorf("worker unhealthy after duplicate: expected 200, got %d", workerResp.StatusCode)
	} else {
		fmt.Printf("  worker stable: /health returned 200 after duplicate message\n")
	}
}
