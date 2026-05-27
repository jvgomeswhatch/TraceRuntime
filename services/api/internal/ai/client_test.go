package ai_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/runtime-platform/services/api/internal/ai"
)

func TestClient_CallsInferEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/infer" || r.Method != http.MethodPost {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("traceparent") == "" {
			t.Error("expected traceparent header")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"task_id":               "task-001",
			"execution_status":      "completed",
			"execution_profile":     map[string]any{"task_type": "general", "model": "qwen2.5:3b"},
			"output":                "hello world",
			"output_ref":            nil,
			"validation_status":     "ok",
			"inference_duration_ms": 500,
			"total_duration_ms":     510,
		})
	}))
	defer srv.Close()

	c := ai.NewClient(srv.URL, 10*time.Second)
	resp, err := c.Infer(context.Background(), ai.InferRequest{
		TaskID:         "task-001",
		Input:          "hello",
		DeadlineUnixMs: time.Now().Add(30 * time.Second).UnixMilli(),
		Traceparent:    "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.ExecutionStatus != "completed" {
		t.Errorf("expected completed, got %q", resp.ExecutionStatus)
	}
	if resp.Output != "hello world" {
		t.Errorf("expected 'hello world', got %q", resp.Output)
	}
}

func TestClient_ReturnsErrorOn5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := ai.NewClient(srv.URL, 5*time.Second)
	_, err := c.Infer(context.Background(), ai.InferRequest{
		TaskID:         "task-002",
		Input:          "hi",
		DeadlineUnixMs: time.Now().Add(30 * time.Second).UnixMilli(),
		Traceparent:    "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	})
	if err == nil {
		t.Fatal("expected error on 5xx, got nil")
	}
}

func TestClient_ReturnsErrorOnTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := ai.NewClient(srv.URL, 50*time.Millisecond)
	_, err := c.Infer(context.Background(), ai.InferRequest{
		TaskID:         "task-003",
		Input:          "hi",
		DeadlineUnixMs: time.Now().Add(30 * time.Second).UnixMilli(),
		Traceparent:    "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
	})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}
