package worker_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/runtime-platform/services/api/internal/ai"
	"github.com/runtime-platform/services/api/internal/event"
	"github.com/runtime-platform/services/api/internal/queue"
	"github.com/runtime-platform/services/api/internal/worker"
)

func TestWorker_PublishesCompletedWithOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task_id":               "task-001",
			"execution_status":      "completed",
			"execution_profile":     map[string]any{"task_type": "general", "model": "qwen2.5:3b"},
			"output":                "the answer",
			"output_ref":            nil,
			"validation_status":     "ok",
			"inference_duration_ms": 500,
			"total_duration_ms":     510,
		})
	}))
	defer srv.Close()

	broker := event.NewBroker()
	ch := broker.Subscribe("test")
	defer broker.Unsubscribe("test")

	q := queue.NewQueue(1)
	aiClient := ai.NewClient(srv.URL, 5*time.Second)
	w := worker.New(q, broker, aiClient)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go w.Run(ctx)

	q.Enqueue(queue.Task{
		ID:          "task-001",
		TraceID:     "abc123",
		Traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		Payload:     "hello",
	})

	var completed []byte
	for i := 0; i < 10; i++ {
		select {
		case msg := <-ch:
			if strings.Contains(string(msg), "task.completed") {
				completed = msg
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for task.completed")
		}
		if completed != nil {
			break
		}
	}

	if completed == nil {
		t.Fatal("never received task.completed event")
	}
	if !strings.Contains(string(completed), "the answer") {
		t.Errorf("expected output in task.completed, got: %s", completed)
	}
}

func TestWorker_PublishesFailedOnRuntimeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	broker := event.NewBroker()
	ch := broker.Subscribe("test")
	defer broker.Unsubscribe("test")

	q := queue.NewQueue(1)
	aiClient := ai.NewClient(srv.URL, 2*time.Second)
	w := worker.New(q, broker, aiClient)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go w.Run(ctx)

	q.Enqueue(queue.Task{
		ID:          "task-002",
		TraceID:     "abc123",
		Traceparent: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
		Payload:     "hello",
	})

	for i := 0; i < 10; i++ {
		select {
		case msg := <-ch:
			if strings.Contains(string(msg), "task.failed") {
				return // pass
			}
		case <-time.After(3 * time.Second):
			t.Fatal("timed out waiting for task.failed")
		}
	}
	t.Fatal("never received task.failed event")
}
