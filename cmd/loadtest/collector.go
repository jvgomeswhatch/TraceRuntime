package main

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type TaskTimestamps struct {
	TaskID              string
	Status              string
	CreatedAt           time.Time
	ProcessingStartedAt *time.Time
	CompletedAt         *time.Time
	PromptTokens        int
	CompletionTokens    int
	TokensPerSecond     float64
	Model               string
}

type LatencyBucket struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

type TokenMetricsSummary struct {
	TotalPromptTokens     int           `json:"total_prompt_tokens"`
	TotalCompletionTokens int           `json:"total_completion_tokens"`
	TotalTokens           int           `json:"total_tokens"`
	AvgTokensPerSecond    float64       `json:"avg_tokens_per_second"`
	TokensPerSecond       LatencyBucket `json:"tokens_per_second_distribution"`
	OutputTokens          LatencyBucket `json:"output_tokens_distribution"`
	Model                 string        `json:"model"`
}

type CollectedResults struct {
	TasksCompleted    int
	TasksFailed       int
	NegativeDurations int
	APIRequest        LatencyBucket
	SubmitToProc      LatencyBucket
	Processing        LatencyBucket
	EndToEnd          LatencyBucket
	TokenMetrics      TokenMetricsSummary
}

func collectResults(ctx context.Context, dbURL string, submissions []TaskResult, timeout time.Duration) (*CollectedResults, error) {
	pool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	defer pool.Close()

	taskIDs := make([]string, 0, len(submissions))
	submitTimes := make(map[string]time.Time)
	apiLatencies := make(map[string]time.Duration)
	for _, s := range submissions {
		if s.Error == nil && s.TaskID != "" {
			taskIDs = append(taskIDs, s.TaskID)
			submitTimes[s.TaskID] = s.SubmitAt
			apiLatencies[s.TaskID] = s.Latency
		}
	}

	if len(taskIDs) == 0 {
		return nil, fmt.Errorf("no successfully submitted tasks to collect")
	}

	slog.Info("waiting for tasks to complete", "task_count", len(taskIDs))

	deadline := time.Now().Add(timeout)
	var timestamps []TaskTimestamps

	for time.Now().Before(deadline) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		timestamps, err = queryTaskTimestamps(ctx, pool, taskIDs)
		if err != nil {
			return nil, err
		}

		allDone := true
		for _, t := range timestamps {
			if t.Status == "pending" || t.Status == "processing" {
				allDone = false
				break
			}
		}

		if allDone {
			break
		}

		pending := 0
		processing := 0
		for _, t := range timestamps {
			switch t.Status {
			case "pending":
				pending++
			case "processing":
				processing++
			}
		}
		slog.Info("waiting for completion", "pending", pending, "processing", processing, "completed+failed", len(timestamps)-pending-processing)

		time.Sleep(3 * time.Second)
	}

	var apiReqMs, submitToProcMs, processingMs, endToEndMs []float64
	completed := 0
	failed := 0
	negativeDurations := 0

	for _, t := range timestamps {
		switch t.Status {
		case "completed":
			completed++
		case "failed":
			failed++
		}

		if lat, ok := apiLatencies[t.TaskID]; ok {
			apiReqMs = append(apiReqMs, float64(lat.Milliseconds()))
		}

		// Use DB timestamps for queue wait and end-to-end to avoid
		// host/container clock skew. created_at and processing_started_at
		// are both written by services inside Docker (same clock).
		if t.ProcessingStartedAt != nil {
			submitToProc := t.ProcessingStartedAt.Sub(t.CreatedAt)
			if submitToProc < 0 {
				slog.Warn("negative queue wait clamped to 0", "task_id", t.TaskID)
				negativeDurations++
				submitToProc = 0
			}
			submitToProcMs = append(submitToProcMs, float64(submitToProc.Milliseconds()))
		}

		if t.ProcessingStartedAt != nil && t.CompletedAt != nil {
			procDur := t.CompletedAt.Sub(*t.ProcessingStartedAt)
			if procDur < 0 {
				slog.Warn("negative processing duration clamped to 0", "task_id", t.TaskID)
				negativeDurations++
				procDur = 0
			}
			processingMs = append(processingMs, float64(procDur.Milliseconds()))
		}

		if t.CompletedAt != nil {
			e2e := t.CompletedAt.Sub(t.CreatedAt)
			if e2e < 0 {
				slog.Warn("negative end-to-end duration clamped to 0", "task_id", t.TaskID)
				negativeDurations++
				e2e = 0
			}
			endToEndMs = append(endToEndMs, float64(e2e.Milliseconds()))
		}
	}

	// Aggregate token metrics from completed tasks.
	var totalPrompt, totalCompletion int
	var tpsValues, outputTokenValues []float64
	modelCounts := make(map[string]int)

	for _, t := range timestamps {
		if t.Status != "completed" {
			continue
		}
		totalPrompt += t.PromptTokens
		totalCompletion += t.CompletionTokens
		if t.TokensPerSecond > 0 {
			tpsValues = append(tpsValues, t.TokensPerSecond)
		}
		if t.CompletionTokens > 0 {
			outputTokenValues = append(outputTokenValues, float64(t.CompletionTokens))
		}
		if t.Model != "" {
			modelCounts[t.Model]++
		}
	}

	avgTPS := 0.0
	if len(tpsValues) > 0 {
		sum := 0.0
		for _, v := range tpsValues {
			sum += v
		}
		avgTPS = sum / float64(len(tpsValues))
	}

	bestModel := ""
	bestCount := 0
	for m, c := range modelCounts {
		if c > bestCount {
			bestModel = m
			bestCount = c
		}
	}

	tokenMetrics := TokenMetricsSummary{
		TotalPromptTokens:     totalPrompt,
		TotalCompletionTokens: totalCompletion,
		TotalTokens:           totalPrompt + totalCompletion,
		AvgTokensPerSecond:    avgTPS,
		TokensPerSecond:       computeBucket(tpsValues),
		OutputTokens:          computeBucket(outputTokenValues),
		Model:                 bestModel,
	}

	return &CollectedResults{
		TasksCompleted:    completed,
		TasksFailed:       failed,
		NegativeDurations: negativeDurations,
		APIRequest:        computeBucket(apiReqMs),
		SubmitToProc:      computeBucket(submitToProcMs),
		Processing:        computeBucket(processingMs),
		EndToEnd:          computeBucket(endToEndMs),
		TokenMetrics:      tokenMetrics,
	}, nil
}

func queryTaskTimestamps(ctx context.Context, pool *pgxpool.Pool, taskIDs []string) ([]TaskTimestamps, error) {
	rows, err := pool.Query(ctx,
		`SELECT id::text, status, created_at, processing_started_at, completed_at,
		        prompt_tokens, completion_tokens, tokens_per_second, model
		 FROM tasks WHERE id = ANY($1::uuid[])`, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("query tasks: %w", err)
	}
	defer rows.Close()

	var results []TaskTimestamps
	for rows.Next() {
		var t TaskTimestamps
		if err := rows.Scan(&t.TaskID, &t.Status, &t.CreatedAt, &t.ProcessingStartedAt, &t.CompletedAt,
			&t.PromptTokens, &t.CompletionTokens, &t.TokensPerSecond, &t.Model); err != nil {
			return nil, fmt.Errorf("scan task: %w", err)
		}
		results = append(results, t)
	}
	return results, rows.Err()
}

func computeBucket(values []float64) LatencyBucket {
	if len(values) == 0 {
		return LatencyBucket{}
	}
	sort.Float64s(values)
	return LatencyBucket{
		P50: percentile(values, 50),
		P95: percentile(values, 95),
		P99: percentile(values, 99),
		Min: values[0],
		Max: values[len(values)-1],
	}
}

func percentile(sorted []float64, pct float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	rank := (pct / 100) * float64(len(sorted)-1)
	lower := int(math.Floor(rank))
	upper := int(math.Ceil(rank))
	if lower == upper {
		return sorted[lower]
	}
	frac := rank - float64(lower)
	return sorted[lower]*(1-frac) + sorted[upper]*frac
}
