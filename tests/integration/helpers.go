package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Global clients initialized in TestMain.
var (
	apiURL    string
	dbPool    *pgxpool.Pool
	sqsClient *sqs.Client
	s3Client  *s3.Client
	queueURL  string
	dlqURL    string
)

// createTask sends POST /tasks and returns the task_id and trace_id from the response.
func createTask(t *testing.T, input string) (taskID, traceID string) {
	t.Helper()
	body := fmt.Sprintf(`{"input":%q}`, input)
	resp, err := http.Post(apiURL+"/tasks", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status %d: %s", resp.StatusCode, string(b))
	}
	var result struct {
		TaskID  string `json:"task_id"`
		TraceID string `json:"trace_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("failed to decode create-task response: %v", err)
	}
	return result.TaskID, result.TraceID
}

// waitForTaskStatus polls the database until the task reaches the expected status
// or the timeout expires. Uses polling (no time.Sleep loops without a deadline).
func waitForTaskStatus(t *testing.T, taskID, expected string, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	var lastStatus string
	for {
		err := dbPool.QueryRow(ctx, "SELECT status FROM tasks WHERE id = $1", taskID).Scan(&lastStatus)
		if err == nil && lastStatus == expected {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("task %s did not reach status %q within %v (last: %q)", taskID, expected, timeout, lastStatus)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// taskRecord mirrors the tasks table schema including token columns added in migration 5.
type taskRecord struct {
	ID                  string
	TraceID             string
	Status              string
	ArtifactKey         *string
	ErrorMessage        string
	PromptTokens        int
	CompletionTokens    int
	TokensPerSecond     float64
	CreatedAt           time.Time
	ProcessingStartedAt *time.Time
	CompletedAt         *time.Time
}

// getTask reads a full task record from the database.
func getTask(t *testing.T, taskID string) taskRecord {
	t.Helper()
	var task taskRecord
	err := dbPool.QueryRow(context.Background(),
		`SELECT id, trace_id, status, artifact_key,
		        COALESCE(error_message, ''),
		        prompt_tokens, completion_tokens, tokens_per_second,
		        created_at, processing_started_at, completed_at
		 FROM tasks WHERE id = $1`, taskID).Scan(
		&task.ID, &task.TraceID, &task.Status, &task.ArtifactKey,
		&task.ErrorMessage,
		&task.PromptTokens, &task.CompletionTokens, &task.TokensPerSecond,
		&task.CreatedAt, &task.ProcessingStartedAt, &task.CompletedAt,
	)
	if err != nil {
		t.Fatalf("failed to get task %s: %v", taskID, err)
	}
	return task
}

// cleanDB truncates all test-related tables. Called in test cleanup.
func cleanDB(t *testing.T) {
	t.Helper()
	_, err := dbPool.Exec(context.Background(),
		"TRUNCATE TABLE healing_events, worker_heartbeats, tasks, chaos_runs RESTART IDENTITY CASCADE")
	if err != nil {
		t.Fatalf("failed to clean database: %v", err)
	}
}

// queueDepth returns the approximate number of messages in the main task queue.
func queueDepth(t *testing.T) int {
	t.Helper()
	out, err := sqsClient.GetQueueAttributes(context.Background(), &sqs.GetQueueAttributesInput{
		QueueUrl:       &queueURL,
		AttributeNames: []sqstypes.QueueAttributeName{"ApproximateNumberOfMessages"},
	})
	if err != nil {
		t.Fatalf("failed to get queue depth: %v", err)
	}
	var n int
	fmt.Sscanf(out.Attributes["ApproximateNumberOfMessages"], "%d", &n)
	return n
}

// dlqDepth returns the approximate number of messages in the dead-letter queue.
func dlqDepth(t *testing.T) int {
	t.Helper()
	out, err := sqsClient.GetQueueAttributes(context.Background(), &sqs.GetQueueAttributesInput{
		QueueUrl:       &dlqURL,
		AttributeNames: []sqstypes.QueueAttributeName{"ApproximateNumberOfMessages"},
	})
	if err != nil {
		t.Fatalf("failed to get DLQ depth: %v", err)
	}
	var n int
	fmt.Sscanf(out.Attributes["ApproximateNumberOfMessages"], "%d", &n)
	return n
}

// purgeQueue removes all messages from the given SQS queue URL.
func purgeQueue(t *testing.T, url string) {
	t.Helper()
	_, err := sqsClient.PurgeQueue(context.Background(), &sqs.PurgeQueueInput{QueueUrl: &url})
	if err != nil {
		t.Logf("purge queue warning: %v", err)
	}
}

// getArtifact downloads a raw artifact from S3.
func getArtifact(t *testing.T, key string) []byte {
	t.Helper()
	out, err := s3Client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String("traceruntime-outputs"),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Fatalf("failed to get S3 artifact %s: %v", key, err)
	}
	defer out.Body.Close()
	data, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatalf("failed to read S3 artifact: %v", err)
	}
	return data
}

// getArtifactJSON downloads an artifact from S3 and parses it as JSON.
func getArtifactJSON(t *testing.T, key string) map[string]any {
	t.Helper()
	data := getArtifact(t, key)
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to parse artifact JSON: %v", err)
	}
	return result
}
