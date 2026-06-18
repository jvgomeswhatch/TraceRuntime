package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type TaskResult struct {
	TaskID   string
	TraceID  string
	SubmitAt time.Time
	Latency  time.Duration
	Error    error
}

type taskRequest struct {
	Input string `json:"input"`
}

type taskResponse struct {
	TaskID      string `json:"task_id"`
	TraceID     string `json:"trace_id"`
	Traceparent string `json:"traceparent"`
}

var sampleInputs = []string{
	"Analyze the performance characteristics of this distributed system",
	"Classify the following event: worker heartbeat timeout after 60 seconds",
	"Summarize the operational status of the runtime platform",
	"Evaluate the queue backlog and recommend scaling actions",
	"Generate a report on inference latency trends",
}

func sendTasks(ctx context.Context, cfg Config) ([]TaskResult, error) {
	results := make([]TaskResult, 0, cfg.Tasks)
	var mu sync.Mutex

	interval := time.Duration(float64(time.Second) / cfg.Rate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	taskCh := make(chan int, cfg.Tasks)
	var wg sync.WaitGroup
	var sent atomic.Int64

	client := &http.Client{Timeout: 30 * time.Second}

	for i := 0; i < cfg.Concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range taskCh {
				if ctx.Err() != nil {
					return
				}
				input := sampleInputs[idx%len(sampleInputs)]
				result := submitTask(ctx, client, cfg.APIURL, input)
				mu.Lock()
				results = append(results, result)
				mu.Unlock()
				n := sent.Add(1)
				if n%10 == 0 || n == int64(cfg.Tasks) {
					slog.Info("progress", "sent", n, "total", cfg.Tasks)
				}
			}
		}()
	}

	for i := 0; i < cfg.Tasks; i++ {
		select {
		case <-ctx.Done():
			close(taskCh)
			wg.Wait()
			return results, ctx.Err()
		case <-ticker.C:
			taskCh <- i
		}
	}
	close(taskCh)
	wg.Wait()

	return results, nil
}

func submitTask(ctx context.Context, client *http.Client, apiURL, input string) TaskResult {
	body, _ := json.Marshal(taskRequest{Input: input})
	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, apiURL+"/tasks", bytes.NewReader(body))
	if err != nil {
		return TaskResult{SubmitAt: start, Latency: time.Since(start), Error: err}
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	latency := time.Since(start)
	if err != nil {
		return TaskResult{SubmitAt: start, Latency: latency, Error: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		return TaskResult{SubmitAt: start, Latency: latency, Error: fmt.Errorf("unexpected status: %d", resp.StatusCode)}
	}

	var tr taskResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return TaskResult{SubmitAt: start, Latency: latency, Error: err}
	}

	return TaskResult{
		TaskID:   tr.TaskID,
		TraceID:  tr.TraceID,
		SubmitAt: start,
		Latency:  latency,
	}
}
