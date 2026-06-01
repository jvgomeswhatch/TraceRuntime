package http_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/runtime-platform/services/api/internal/event"
	apihttp "github.com/runtime-platform/services/api/internal/http"
	"github.com/runtime-platform/services/api/internal/queue"
)

func TestTask_CreatesTask(t *testing.T) {
	broker := event.NewBroker()
	h := apihttp.NewTaskHandler(broker, apihttp.NewQueuePublisher(queue.NewQueue(128)))

	req := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewBufferString(`{"input":"hello"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d — body: %s", rec.Code, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("could not decode response: %v", err)
	}
	if resp["task_id"] == "" {
		t.Error("expected task_id in response")
	}
	if resp["trace_id"] == "" {
		t.Error("expected trace_id in response")
	}
	// trace_id is the OTEL TraceID — 32 lowercase hex chars
	if len(resp["trace_id"]) != 32 {
		t.Errorf("expected trace_id length 32, got %d", len(resp["trace_id"]))
	}
	// traceparent must follow W3C format: 00-<traceID>-<spanID>-01
	if !strings.HasPrefix(resp["traceparent"], "00-") {
		t.Errorf("expected traceparent W3C format, got %q", resp["traceparent"])
	}
	if len(resp["traceparent"]) != 55 { // "00-" + 32 + "-" + 16 + "-01"
		t.Errorf("expected traceparent length 55, got %d", len(resp["traceparent"]))
	}
}

func TestTask_PublishesSSEEvent(t *testing.T) {
	broker := event.NewBroker()
	ch := broker.Subscribe("test")
	defer broker.Unsubscribe("test")

	h := apihttp.NewTaskHandler(broker, apihttp.NewQueuePublisher(queue.NewQueue(128)))

	req := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewBufferString(`{"input":"publish test"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	select {
	case msg := <-ch:
		if !strings.Contains(string(msg), "task.created") {
			t.Errorf("expected task.created in SSE message, got: %s", msg)
		}
		if !strings.Contains(string(msg), "trace_id") {
			t.Errorf("expected trace_id in SSE message, got: %s", msg)
		}
	default:
		t.Fatal("expected message published to broker, got none")
	}
}

func TestTask_RejectsEmptyInput(t *testing.T) {
	broker := event.NewBroker()
	h := apihttp.NewTaskHandler(broker, apihttp.NewQueuePublisher(queue.NewQueue(128)))

	for _, body := range []string{`{}`, `{"input":""}`} {
		req := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()

		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %q: expected 400, got %d", body, rec.Code)
		}
	}
}

func TestTask_ContentType(t *testing.T) {
	broker := event.NewBroker()
	h := apihttp.NewTaskHandler(broker, apihttp.NewQueuePublisher(queue.NewQueue(128)))

	req := httptest.NewRequest(http.MethodPost, "/tasks", bytes.NewBufferString(`{"input":"x"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}
}
