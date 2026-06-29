package integration

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

func TestDLQExplorer(t *testing.T) {
	cleanDB(t)
	purgeQueue(t, dlqURL)

	token := envOr("INTERNAL_TOKEN", "")
	ctx := context.Background()

	msgBody := `{"task_id":"dlq-test-task-1","trace_id":"dlq-trace-1","traceparent":"00-dlq-trace-01","payload":"test dlq input"}`
	_, err := sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
		QueueUrl:    &dlqURL,
		MessageBody: aws.String(msgBody),
		MessageAttributes: map[string]sqstypes.MessageAttributeValue{
			"traceparent": {DataType: aws.String("String"), StringValue: aws.String("00-dlq-trace-01")},
		},
	})
	if err != nil {
		t.Fatalf("send to DLQ: %v", err)
	}

	_, err = dbPool.Exec(ctx,
		`INSERT INTO tasks (id, trace_id, status, error_message, created_at, updated_at)
		 VALUES ('dlq-test-task-1', 'dlq-trace-1', 'failed', 'test failure', NOW(), NOW())`)
	if err != nil {
		t.Fatalf("insert task: %v", err)
	}

	time.Sleep(1 * time.Second)

	t.Run("ListMessages", func(t *testing.T) {
		req, _ := http.NewRequest("GET", apiURL+"/api/dlq/messages", nil)
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
		messages := result["messages"].([]any)
		if len(messages) == 0 {
			t.Fatal("expected at least 1 message in DLQ")
		}
	})

	t.Run("Stats", func(t *testing.T) {
		req, _ := http.NewRequest("GET", apiURL+"/api/dlq/stats", nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})

	t.Run("RetryMessage", func(t *testing.T) {
		req, _ := http.NewRequest("GET", apiURL+"/api/dlq/messages", nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		messages := result["messages"].([]any)
		if len(messages) == 0 {
			t.Skip("no messages in DLQ for retry test")
		}
		msg := messages[0].(map[string]any)

		retryBody, _ := json.Marshal(map[string]any{
			"receipt_handle":     msg["receipt_handle"],
			"message_body":       msg["message_body"],
			"message_attributes": msg["message_attributes"],
		})
		msgID := msg["message_id"].(string)
		retryReq, _ := http.NewRequest("POST", apiURL+"/api/dlq/messages/"+msgID+"/retry", strings.NewReader(string(retryBody)))
		retryReq.Header.Set("X-Internal-Token", token)
		retryReq.Header.Set("Content-Type", "application/json")
		retryResp, err := http.DefaultClient.Do(retryReq)
		if err != nil {
			t.Fatalf("retry request failed: %v", err)
		}
		defer retryResp.Body.Close()
		if retryResp.StatusCode != 200 {
			b, _ := io.ReadAll(retryResp.Body)
			t.Fatalf("expected 200, got %d: %s", retryResp.StatusCode, string(b))
		}
	})

	t.Run("Purge", func(t *testing.T) {
		sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
			QueueUrl:    &dlqURL,
			MessageBody: aws.String(`{"task_id":"purge-test","trace_id":"purge-trace","payload":"purge"}`),
		})
		time.Sleep(500 * time.Millisecond)

		req, _ := http.NewRequest("POST", apiURL+"/api/dlq/purge", nil)
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
	})
}
