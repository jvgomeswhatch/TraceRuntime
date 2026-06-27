package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// InferRequest is sent from the Go worker to the FastAPI AI runtime.
type InferRequest struct {
	TaskID         string `json:"task_id"`
	Input          string `json:"input"`
	DeadlineUnixMs int64  `json:"deadline_unix_ms"`
	Traceparent    string `json:"-"` // sent as header, not body
}

// ExecutionProfile mirrors the Python ExecutionProfile TypedDict.
type ExecutionProfile struct {
	TaskType       string `json:"task_type"`
	Model          string `json:"model"`
	TimeoutMs      int    `json:"timeout_ms"`
	MaxOutputChars int    `json:"max_output_chars"`
}

// InferResponse is the parsed response from POST /infer.
type InferResponse struct {
	TaskID              string           `json:"task_id"`
	ExecutionStatus     string           `json:"execution_status"`
	ExecutionProfile    ExecutionProfile `json:"execution_profile"`
	Output              string           `json:"output"`
	OutputRef           *string          `json:"output_ref"`
	ValidationStatus    string           `json:"validation_status"`
	InferenceDurationMs int              `json:"inference_duration_ms"`
	TotalDurationMs     int              `json:"total_duration_ms"`
}

// Client calls the FastAPI AI runtime.
type Client struct {
	baseURL string
	timeout time.Duration
	http    *http.Client
}

// NewClient creates an ai.Client. timeout is the per-call deadline.
func NewClient(baseURL string, timeout time.Duration) *Client {
	return &Client{
		baseURL: baseURL,
		timeout: timeout,
		http:    &http.Client{Timeout: timeout},
	}
}

// Infer calls POST /infer on the AI runtime.
// It propagates the W3C traceparent via request header.
func (c *Client) Infer(ctx context.Context, req InferRequest) (*InferResponse, error) {
	body, err := json.Marshal(map[string]any{
		"task_id":          req.TaskID,
		"input":            req.Input,
		"deadline_unix_ms": req.DeadlineUnixMs,
	})
	if err != nil {
		return nil, fmt.Errorf("ai client: marshal: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/infer", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("ai client: build request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if req.Traceparent != "" {
		httpReq.Header.Set("traceparent", req.Traceparent)
	}

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ai client: request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode >= 500 {
		return nil, fmt.Errorf("ai client: runtime error: HTTP %d", resp.StatusCode)
	}

	var result InferResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("ai client: decode response: %w", err)
	}
	return &result, nil
}
