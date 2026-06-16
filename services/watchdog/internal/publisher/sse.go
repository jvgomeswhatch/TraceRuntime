package publisher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type SSEEvent struct {
	EventID        string          `json:"event_id"`
	EventType      string          `json:"event_type"`
	TraceID        string          `json:"trace_id"`
	TaskID         string          `json:"task_id"`
	Timestamp      string          `json:"timestamp"`
	Source         string          `json:"source"`
	Severity       string          `json:"severity,omitempty"`
	Status         string          `json:"status,omitempty"`
	HealingEventID string          `json:"healing_event_id,omitempty"`
	Details        json.RawMessage `json:"details,omitempty"`
}

type SSEPublisher struct {
	url    string
	token  string
	client *http.Client
}

func NewSSEPublisher(apiEventsURL, internalToken string) *SSEPublisher {
	return &SSEPublisher{
		url:    apiEventsURL,
		token:  internalToken,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (p *SSEPublisher) Publish(ctx context.Context, ev SSEEvent) error {
	ev.EventID = uuid.New().String()
	ev.Timestamp = time.Now().UTC().Format(time.RFC3339)
	ev.Source = "watchdog"

	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal sse event: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build sse request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.token != "" {
		req.Header.Set("X-Internal-Token", p.token)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("sse publish: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("sse publish returned %d", resp.StatusCode)
	}
	return nil
}
