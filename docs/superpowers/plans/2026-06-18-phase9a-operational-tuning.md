# Phase 9A — Operational Tuning & Capacity: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a load test tool that measures real inference latency across the full pipeline, produces JSON baselines with calibration recommendations, and surfaces the latest result on the frontend dashboard.

**Architecture:** Go binary (`cmd/loadtest/`) runs on the host, submits tasks to the API at a controlled rate, captures SQS queue high-water marks in a background goroutine, then queries PostgreSQL directly for segmented latency breakdown. The API serves the latest result JSON via `GET /api/capacity/latest`. A frontend `CapacityReport` component renders it.

**Tech Stack:** Go (net/http, pgx/v5, aws-sdk-go-v2), Next.js/TypeScript (shadcn/ui Card), PostgreSQL, SQS/LocalStack

---

## File Map

| Action | File | Responsibility |
|---|---|---|
| Create | `cmd/loadtest/main.go` | CLI entry point: flags, orchestration, summary output |
| Create | `cmd/loadtest/sender.go` | Rate-limited task submission goroutines |
| Create | `cmd/loadtest/collector.go` | PostgreSQL query for task timestamps + percentile calculation |
| Create | `cmd/loadtest/queue.go` | SQS attribute polling for high-water marks |
| Create | `cmd/loadtest/report.go` | JSON result assembly, recommendations, status logic, file write |
| Create | `cmd/loadtest/go.mod` | Standalone module (pgx, aws-sdk-go-v2, no shared deps) |
| Create | `services/api/internal/http/capacity.go` | Handler for `GET /api/capacity/latest` |
| Modify | `services/api/internal/http/router.go` | Register `/api/capacity/latest` route |
| Modify | `services/api/cmd/server/main.go` | Pass results dir to router |
| Create | `frontend/components/capacity-report.tsx` | Capacity Report panel component |
| Modify | `frontend/lib/types.ts` | Add `CapacityReport` TypeScript interface |
| Modify | `frontend/components/dashboard-client.tsx` | Include CapacityReport in layout |
| Modify | `Makefile` | Add `loadtest` target |
| Modify | `.gitignore` | Add `results/*.json` |
| Create | `results/.gitkeep` | Ensure directory exists in repo |

---

## Task 1: Project scaffolding — loadtest module and results directory

**Files:**
- Create: `cmd/loadtest/go.mod`
- Create: `cmd/loadtest/main.go` (skeleton only)
- Create: `results/.gitkeep`
- Modify: `.gitignore`
- Modify: `Makefile`

- [ ] **Step 1: Create the loadtest Go module**

```bash
mkdir -p cmd/loadtest
```

Create `cmd/loadtest/go.mod`:
```go
module github.com/runtime-platform/cmd/loadtest

go 1.22.0

require (
	github.com/aws/aws-sdk-go-v2 v1.41.9
	github.com/aws/aws-sdk-go-v2/config v1.32.20
	github.com/aws/aws-sdk-go-v2/service/sqs v1.42.29
	github.com/jackc/pgx/v5 v5.10.0
)
```

Then run: `cd cmd/loadtest && go mod tidy`

- [ ] **Step 2: Create main.go skeleton with flag parsing**

Create `cmd/loadtest/main.go`:
```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	tasks := flag.Int("tasks", 50, "total tasks to submit")
	rate := flag.Float64("rate", 2, "requests per second")
	concurrency := flag.Int("concurrency", 4, "parallel sender goroutines")
	apiURL := flag.String("api-url", "http://localhost:8082", "API endpoint")
	dbURL := flag.String("db-url", envOrDefault("DATABASE_URL", "postgres://traceruntime:traceruntime@localhost:5432/traceruntime?sslmode=disable"), "PostgreSQL connection string")
	sqsURL := flag.String("sqs-url", envOrDefault("SQS_QUEUE_URL", "http://localhost:4566/000000000000/traceruntime-tasks"), "SQS queue URL")
	outputDir := flag.String("output-dir", "results", "directory for JSON output")
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg := Config{
		Tasks:       *tasks,
		Rate:        *rate,
		Concurrency: *concurrency,
		APIURL:      *apiURL,
		DBURL:       *dbURL,
		SQSURL:      *sqsURL,
		OutputDir:   *outputDir,
	}

	slog.Info("loadtest starting",
		"tasks", cfg.Tasks,
		"rate", cfg.Rate,
		"concurrency", cfg.Concurrency,
		"api_url", cfg.APIURL,
	)

	if err := run(ctx, cfg); err != nil {
		slog.Error("loadtest failed", "error", err)
		os.Exit(1)
	}
}

type Config struct {
	Tasks       int
	Rate        float64
	Concurrency int
	APIURL      string
	DBURL       string
	SQSURL      string
	OutputDir   string
}

func run(ctx context.Context, cfg Config) error {
	start := time.Now()
	_ = start
	fmt.Println("loadtest: not yet implemented")
	return nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
```

- [ ] **Step 3: Create results directory and update .gitignore**

Create `results/.gitkeep` (empty file).

Add to `.gitignore`:
```
# Loadtest results
results/*.json
```

- [ ] **Step 4: Add Makefile target**

Add to `Makefile` at the end:
```makefile
# ── Load Testing ──────────────────────────────────────────────────────────────
.PHONY: loadtest

loadtest:
	cd cmd/loadtest && go run . \
		--tasks=$(or $(TASKS),50) \
		--rate=$(or $(RATE),2) \
		--concurrency=$(or $(CONCURRENCY),4)
```

- [ ] **Step 5: Verify module builds**

Run: `cd cmd/loadtest && go mod tidy && go build .`
Expected: no errors, binary produced.

Run: `make loadtest TASKS=1`
Expected: prints "loadtest: not yet implemented" and exits cleanly.

---

## Task 2: Rate-limited task sender

**Files:**
- Create: `cmd/loadtest/sender.go`

- [ ] **Step 1: Implement the sender**

Create `cmd/loadtest/sender.go`:
```go
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
	TaskID    string
	TraceID   string
	SubmitAt  time.Time
	Latency   time.Duration
	Error     error
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
```

- [ ] **Step 2: Verify sender compiles**

Run: `cd cmd/loadtest && go build .`
Expected: no errors.

---

## Task 3: SQS queue high-water mark tracker

**Files:**
- Create: `cmd/loadtest/queue.go`

- [ ] **Step 1: Implement queue metrics tracker**

Create `cmd/loadtest/queue.go`:
```go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

type QueueMetrics struct {
	MaxVisible  int64
	MaxInflight int64
	mu          sync.Mutex
}

func (qm *QueueMetrics) update(visible, inflight int64) {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	if visible > qm.MaxVisible {
		qm.MaxVisible = visible
	}
	if inflight > qm.MaxInflight {
		qm.MaxInflight = inflight
	}
}

func (qm *QueueMetrics) snapshot() (maxVisible, maxInflight int64) {
	qm.mu.Lock()
	defer qm.mu.Unlock()
	return qm.MaxVisible, qm.MaxInflight
}

func trackQueueMetrics(ctx context.Context, sqsURL string, qm *QueueMetrics) {
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		slog.Warn("queue tracker: failed to load AWS config", "error", err)
		return
	}
	client := sqs.NewFromConfig(awsCfg)

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			out, err := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
				QueueUrl: aws.String(sqsURL),
				AttributeNames: []sqstypes.QueueAttributeName{
					"ApproximateNumberOfMessages",
					"ApproximateNumberOfMessagesNotVisible",
				},
			})
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				slog.Warn("queue tracker: get attributes failed", "error", err)
				continue
			}

			var visible, inflight int64
			if v, ok := out.Attributes["ApproximateNumberOfMessages"]; ok {
				fmt.Sscanf(string(v), "%d", &visible)
			}
			if v, ok := out.Attributes["ApproximateNumberOfMessagesNotVisible"]; ok {
				fmt.Sscanf(string(v), "%d", &inflight)
			}
			qm.update(visible, inflight)
		}
	}
}

func checkBacklogConverged(ctx context.Context, sqsURL string) bool {
	awsCfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return false
	}
	client := sqs.NewFromConfig(awsCfg)

	out, err := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl: aws.String(sqsURL),
		AttributeNames: []sqstypes.QueueAttributeName{
			"ApproximateNumberOfMessages",
		},
	})
	if err != nil {
		return false
	}

	var visible int64
	if v, ok := out.Attributes["ApproximateNumberOfMessages"]; ok {
		fmt.Sscanf(string(v), "%d", &visible)
	}
	return visible == 0
}
```

- [ ] **Step 2: Verify compiles**

Run: `cd cmd/loadtest && go build .`
Expected: no errors.

---

## Task 4: PostgreSQL result collector with segmented latency

**Files:**
- Create: `cmd/loadtest/collector.go`

- [ ] **Step 1: Implement the collector**

The tasks table has: `created_at`, `processing_started_at`, `completed_at`, `status`. The collector queries these timestamps for all submitted task IDs and computes segmented latency percentiles.

Create `cmd/loadtest/collector.go`:
```go
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
}

type LatencyBucket struct {
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	P99 float64 `json:"p99"`
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

type CollectedResults struct {
	TasksCompleted int
	TasksFailed    int
	APIRequest     LatencyBucket
	SubmitToProc   LatencyBucket
	Processing     LatencyBucket
	EndToEnd       LatencyBucket
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

	for _, t := range timestamps {
		switch t.Status {
		case "completed":
			completed++
		case "failed":
			failed++
		}

		taskSubmitTime := submitTimes[t.TaskID]

		if lat, ok := apiLatencies[t.TaskID]; ok {
			apiReqMs = append(apiReqMs, float64(lat.Milliseconds()))
		}

		if t.ProcessingStartedAt != nil {
			submitToProc := t.ProcessingStartedAt.Sub(taskSubmitTime)
			submitToProcMs = append(submitToProcMs, float64(submitToProc.Milliseconds()))
		}

		if t.ProcessingStartedAt != nil && t.CompletedAt != nil {
			procDur := t.CompletedAt.Sub(*t.ProcessingStartedAt)
			processingMs = append(processingMs, float64(procDur.Milliseconds()))
		}

		if t.CompletedAt != nil {
			e2e := t.CompletedAt.Sub(taskSubmitTime)
			endToEndMs = append(endToEndMs, float64(e2e.Milliseconds()))
		}
	}

	return &CollectedResults{
		TasksCompleted: completed,
		TasksFailed:    failed,
		APIRequest:     computeBucket(apiReqMs),
		SubmitToProc:   computeBucket(submitToProcMs),
		Processing:     computeBucket(processingMs),
		EndToEnd:       computeBucket(endToEndMs),
	}, nil
}

func queryTaskTimestamps(ctx context.Context, pool *pgxpool.Pool, taskIDs []string) ([]TaskTimestamps, error) {
	rows, err := pool.Query(ctx,
		`SELECT id::text, status, created_at, processing_started_at, completed_at
		 FROM tasks WHERE id = ANY($1::uuid[])`, taskIDs)
	if err != nil {
		return nil, fmt.Errorf("query tasks: %w", err)
	}
	defer rows.Close()

	var results []TaskTimestamps
	for rows.Next() {
		var t TaskTimestamps
		if err := rows.Scan(&t.TaskID, &t.Status, &t.CreatedAt, &t.ProcessingStartedAt, &t.CompletedAt); err != nil {
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
```

- [ ] **Step 2: Verify compiles**

Run: `cd cmd/loadtest && go build .`
Expected: no errors.

---

## Task 5: Report generation — JSON output, recommendations, terminal summary

**Files:**
- Create: `cmd/loadtest/report.go`

- [ ] **Step 1: Implement report generation**

Create `cmd/loadtest/report.go`:
```go
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Report struct {
	Timestamp string       `json:"timestamp"`
	Config    ReportConfig `json:"config"`
	Results   ReportResults `json:"results"`
	QueueMets ReportQueue  `json:"queue_metrics"`
	Recs      Recommendations `json:"recommendations"`
	Status    string       `json:"status"`
	Criteria  StatusCriteria `json:"status_criteria"`
}

type ReportConfig struct {
	Tasks       int     `json:"tasks"`
	Rate        float64 `json:"rate"`
	Concurrency int     `json:"concurrency"`
	APIURL      string  `json:"api_url"`
}

type ReportResults struct {
	DurationSeconds float64        `json:"duration_seconds"`
	TasksSubmitted  int            `json:"tasks_submitted"`
	TasksCompleted  int            `json:"tasks_completed"`
	TasksFailed     int            `json:"tasks_failed"`
	ThroughputRPS   float64        `json:"throughput_rps"`
	LatencyMs       ReportLatency  `json:"latency_ms"`
}

type ReportLatency struct {
	APIRequest      LatencyBucket `json:"api_request"`
	SubmitToProc    LatencyBucket `json:"submit_to_processing"`
	Processing      LatencyBucket `json:"processing_duration"`
	EndToEnd        LatencyBucket `json:"end_to_end"`
}

type ReportQueue struct {
	MaxVisible  int64 `json:"max_visible_messages"`
	MaxInflight int64 `json:"max_inflight_messages"`
	Converged   bool  `json:"backlog_converged"`
}

type VisibilityTimeoutRec struct {
	Current     int    `json:"current"`
	Recommended int    `json:"recommended"`
	Formula     string `json:"formula"`
}

type QueueDepthRec struct {
	Current        int    `json:"current"`
	Recommendation string `json:"recommendation"`
	Reason         string `json:"reason"`
}

type ConcurrencyRec struct {
	Current     int    `json:"current"`
	Observation string `json:"observation"`
}

type Recommendations struct {
	VisibilityTimeout VisibilityTimeoutRec `json:"visibility_timeout"`
	QueueMaxDepth     QueueDepthRec        `json:"queue_max_depth"`
	Concurrency       ConcurrencyRec       `json:"worker_concurrency"`
}

type StatusCriteria struct {
	ErrorRatePct float64 `json:"error_rate_pct"`
	Timeouts     int     `json:"timeouts"`
	Converged    bool    `json:"backlog_converged"`
	DLQTriggered bool    `json:"dlq_triggered"`
}

func buildReport(cfg Config, duration time.Duration, submissions []TaskResult, collected *CollectedResults, qm *QueueMetrics, converged bool) *Report {
	maxVis, maxInf := qm.snapshot()

	submitted := 0
	submitErrors := 0
	for _, s := range submissions {
		if s.Error == nil {
			submitted++
		} else {
			submitErrors++
		}
	}

	totalTasks := collected.TasksCompleted + collected.TasksFailed
	errorRate := 0.0
	if totalTasks > 0 {
		errorRate = float64(collected.TasksFailed) / float64(totalTasks) * 100
	}

	throughput := 0.0
	if duration.Seconds() > 0 {
		throughput = float64(collected.TasksCompleted) / duration.Seconds()
	}

	p95Processing := collected.Processing.P95
	currentVT := 150
	recommendedVT := int(p95Processing/1000) + 30
	if recommendedVT < currentVT {
		recommendedVT = currentVT
	}

	queueDepthReason := "no saturation observed — current threshold adequate"
	queueDepthRec := "adequate"
	if maxVis > 40 {
		queueDepthReason = fmt.Sprintf("peak backlog reached %d messages — approaching threshold", maxVis)
		queueDepthRec = "review"
	}

	concurrencyObs := "no resource contention observed, consider testing concurrency=2"
	if collected.TasksFailed > 0 {
		concurrencyObs = fmt.Sprintf("%d failures observed — investigate before scaling concurrency", collected.TasksFailed)
	}

	status := determineStatus(errorRate, submitErrors, converged)

	return &Report{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Config: ReportConfig{
			Tasks:       cfg.Tasks,
			Rate:        cfg.Rate,
			Concurrency: cfg.Concurrency,
			APIURL:      cfg.APIURL,
		},
		Results: ReportResults{
			DurationSeconds: duration.Seconds(),
			TasksSubmitted:  submitted,
			TasksCompleted:  collected.TasksCompleted,
			TasksFailed:     collected.TasksFailed,
			ThroughputRPS:   throughput,
			LatencyMs: ReportLatency{
				APIRequest:   collected.APIRequest,
				SubmitToProc: collected.SubmitToProc,
				Processing:   collected.Processing,
				EndToEnd:     collected.EndToEnd,
			},
		},
		QueueMets: ReportQueue{
			MaxVisible:  maxVis,
			MaxInflight: maxInf,
			Converged:   converged,
		},
		Recs: Recommendations{
			VisibilityTimeout: VisibilityTimeoutRec{
				Current:     currentVT,
				Recommended: recommendedVT,
				Formula:     "p95_processing_duration + 30s margin",
			},
			QueueMaxDepth: QueueDepthRec{
				Current:        50,
				Recommendation: queueDepthRec,
				Reason:         queueDepthReason,
			},
			Concurrency: ConcurrencyRec{
				Current:     1,
				Observation: concurrencyObs,
			},
		},
		Status: status,
		Criteria: StatusCriteria{
			ErrorRatePct: errorRate,
			Timeouts:     submitErrors,
			Converged:    converged,
			DLQTriggered: false,
		},
	}
}

func determineStatus(errorRate float64, timeouts int, converged bool) string {
	if errorRate > 15 || !converged {
		return "FAIL"
	}
	if errorRate > 5 || timeouts > 0 || !converged {
		return "WARNING"
	}
	return "PASS"
}

func writeReport(report *Report, outputDir string) (string, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", outputDir, err)
	}

	filename := fmt.Sprintf("loadtest-%s.json", time.Now().Format("2006-01-02-1504"))
	path := filepath.Join(outputDir, filename)

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal report: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", path, err)
	}

	return path, nil
}

func printSummary(report *Report) {
	fmt.Println()
	fmt.Println("╔══════════════════════════════════════════════════════════════╗")
	fmt.Println("║                    LOADTEST RESULTS                         ║")
	fmt.Println("╚══════════════════════════════════════════════════════════════╝")
	fmt.Println()
	fmt.Printf("  Tasks:       %d submitted, %d completed, %d failed\n",
		report.Results.TasksSubmitted, report.Results.TasksCompleted, report.Results.TasksFailed)
	fmt.Printf("  Duration:    %.1fs\n", report.Results.DurationSeconds)
	fmt.Printf("  Throughput:  %.2f req/s\n", report.Results.ThroughputRPS)
	fmt.Printf("  Error Rate:  %.1f%%\n", report.Criteria.ErrorRatePct)
	fmt.Println()
	fmt.Println("  Latency (ms)          p50       p95       p99       min       max")
	fmt.Println("  ─────────────────────────────────────────────────────────────────")
	printLatencyRow("  API Request     ", report.Results.LatencyMs.APIRequest)
	printLatencyRow("  Queue Wait      ", report.Results.LatencyMs.SubmitToProc)
	printLatencyRow("  Processing      ", report.Results.LatencyMs.Processing)
	printLatencyRow("  End-to-End      ", report.Results.LatencyMs.EndToEnd)
	fmt.Println()
	fmt.Println("  Queue Behavior")
	fmt.Printf("    Peak Backlog:    %d messages\n", report.QueueMets.MaxVisible)
	fmt.Printf("    Peak In-Flight:  %d messages\n", report.QueueMets.MaxInflight)
	fmt.Printf("    Converged:       %v\n", report.QueueMets.Converged)
	fmt.Println()
	fmt.Println("  Recommendations")
	fmt.Printf("    VisibilityTimeout:   %ds → %ds (%s)\n",
		report.Recs.VisibilityTimeout.Current,
		report.Recs.VisibilityTimeout.Recommended,
		report.Recs.VisibilityTimeout.Formula)
	fmt.Printf("    Queue Max Depth:     %s (%s)\n",
		report.Recs.QueueMaxDepth.Recommendation,
		report.Recs.QueueMaxDepth.Reason)
	fmt.Printf("    Worker Concurrency:  %s\n",
		report.Recs.Concurrency.Observation)
	fmt.Println()

	statusColor := "\033[32m"
	if report.Status == "WARNING" {
		statusColor = "\033[33m"
	} else if report.Status == "FAIL" {
		statusColor = "\033[31m"
	}
	fmt.Printf("  Status: %s● %s\033[0m\n", statusColor, report.Status)
	fmt.Println()
}

func printLatencyRow(label string, b LatencyBucket) {
	fmt.Printf("%s %8.0f  %8.0f  %8.0f  %8.0f  %8.0f\n",
		label, b.P50, b.P95, b.P99, b.Min, b.Max)
}
```

- [ ] **Step 2: Verify compiles**

Run: `cd cmd/loadtest && go build .`
Expected: no errors.

---

## Task 6: Wire everything together in main.go — complete `run()` function

**Files:**
- Modify: `cmd/loadtest/main.go`

- [ ] **Step 1: Replace the placeholder `run()` function**

Replace the `run` function in `cmd/loadtest/main.go` with:

```go
func run(ctx context.Context, cfg Config) error {
	start := time.Now()

	qm := &QueueMetrics{}
	queueCtx, queueCancel := context.WithCancel(ctx)
	defer queueCancel()
	go trackQueueMetrics(queueCtx, cfg.SQSURL, qm)

	submissions, err := sendTasks(ctx, cfg)
	if err != nil && ctx.Err() == nil {
		return fmt.Errorf("send tasks: %w", err)
	}

	successCount := 0
	for _, s := range submissions {
		if s.Error == nil {
			successCount++
		}
	}
	slog.Info("submission phase complete", "success", successCount, "errors", len(submissions)-successCount)

	if successCount == 0 {
		return fmt.Errorf("no tasks were successfully submitted")
	}

	waitTimeout := 10 * time.Minute
	collected, err := collectResults(ctx, cfg.DBURL, submissions, waitTimeout)
	if err != nil {
		return fmt.Errorf("collect results: %w", err)
	}

	queueCancel()
	time.Sleep(1 * time.Second)

	converged := checkBacklogConverged(ctx, cfg.SQSURL)

	duration := time.Since(start)
	report := buildReport(cfg, duration, submissions, collected, qm, converged)

	printSummary(report)

	path, err := writeReport(report, cfg.OutputDir)
	if err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	slog.Info("report written", "path", path)

	return nil
}
```

Also add the missing import for `"time"` if not already present (it should be from the skeleton).

- [ ] **Step 2: Verify builds**

Run: `cd cmd/loadtest && go build .`
Expected: no errors.

- [ ] **Step 3: Manual smoke test with 1 task (AI disabled)**

With the platform running (`make up`), run:
```bash
make loadtest TASKS=1 RATE=1 CONCURRENCY=1
```

Expected:
- 1 task submitted
- Waits for completion
- Prints latency summary
- Writes `results/loadtest-*.json`
- Status: PASS (assuming task completes)

Verify the JSON file was written: `ls results/loadtest-*.json`

---

## Task 7: API endpoint — `GET /api/capacity/latest`

**Files:**
- Create: `services/api/internal/http/capacity.go`
- Modify: `services/api/internal/http/router.go`
- Modify: `services/api/cmd/server/main.go`

- [ ] **Step 1: Create the capacity handler**

Create `services/api/internal/http/capacity.go`:
```go
package http

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type CapacityHandler struct {
	resultsDir string
}

func NewCapacityHandler(resultsDir string) *CapacityHandler {
	return &CapacityHandler{resultsDir: resultsDir}
}

func (h *CapacityHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	entries, err := os.ReadDir(h.resultsDir)
	if err != nil {
		jsonError(w, "no capacity results available", http.StatusNotFound)
		return
	}

	var jsonFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "loadtest-") && strings.HasSuffix(e.Name(), ".json") {
			jsonFiles = append(jsonFiles, e.Name())
		}
	}

	if len(jsonFiles) == 0 {
		jsonError(w, "no capacity results available", http.StatusNotFound)
		return
	}

	sort.Strings(jsonFiles)
	latest := jsonFiles[len(jsonFiles)-1]

	data, err := os.ReadFile(filepath.Join(h.resultsDir, latest))
	if err != nil {
		jsonError(w, "failed to read results", http.StatusInternalServerError)
		return
	}

	var report json.RawMessage
	if err := json.Unmarshal(data, &report); err != nil {
		jsonError(w, "invalid results file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(data)
}
```

- [ ] **Step 2: Register the route in router.go**

In `services/api/internal/http/router.go`, update the `NewRouter` function signature to accept `resultsDir string` and add the route:

Change the function signature from:
```go
func NewRouter(broker *event.Broker, publisher Publisher, q *queue.Queue, database *db.DB) http.Handler {
```
to:
```go
func NewRouter(broker *event.Broker, publisher Publisher, q *queue.Queue, database *db.DB, resultsDir string) http.Handler {
```

Add after the `/api/operations/summary` route:
```go
	r.Get("/api/capacity/latest", NewCapacityHandler(resultsDir).ServeHTTP)
```

- [ ] **Step 3: Update main.go to pass resultsDir**

In `services/api/cmd/server/main.go`, change the `NewRouter` call from:
```go
	router := apihttp.NewRouter(broker, publisher, q, database)
```
to:
```go
	resultsDir := envString("CAPACITY_RESULTS_DIR", "./results")
	router := apihttp.NewRouter(broker, publisher, q, database, resultsDir)
```

- [ ] **Step 4: Mount results directory in docker-compose**

In `docker-compose.yml`, add a volume to the `api` service to mount the host `results/` directory:

Under the `api:` service, add:
```yaml
    volumes:
      - ./results:/app/results:ro
```

- [ ] **Step 5: Verify API builds and runs**

Run: `cd services/api && go build ./...`
Expected: no errors.

- [ ] **Step 6: Manual test**

With platform running and a loadtest result existing in `results/`:

```bash
curl -s http://localhost:8082/api/capacity/latest | python3 -m json.tool
```

Expected: returns the latest loadtest JSON. If no results exist, returns 404 with `{"error": "no capacity results available"}`.

---

## Task 8: Frontend TypeScript types

**Files:**
- Modify: `frontend/lib/types.ts`

- [ ] **Step 1: Add CapacityReport interface**

Add to the end of `frontend/lib/types.ts`:

```typescript
export interface CapacityLatencyBucket {
  p50: number;
  p95: number;
  p99: number;
  min: number;
  max: number;
}

export interface CapacityReport {
  timestamp: string;
  config: {
    tasks: number;
    rate: number;
    concurrency: number;
    api_url: string;
  };
  results: {
    duration_seconds: number;
    tasks_submitted: number;
    tasks_completed: number;
    tasks_failed: number;
    throughput_rps: number;
    latency_ms: {
      api_request: CapacityLatencyBucket;
      submit_to_processing: CapacityLatencyBucket;
      processing_duration: CapacityLatencyBucket;
      end_to_end: CapacityLatencyBucket;
    };
  };
  queue_metrics: {
    max_visible_messages: number;
    max_inflight_messages: number;
    backlog_converged: boolean;
  };
  recommendations: {
    visibility_timeout: {
      current: number;
      recommended: number;
      formula: string;
    };
    queue_max_depth: {
      current: number;
      recommendation: string;
      reason: string;
    };
    worker_concurrency: {
      current: number;
      observation: string;
    };
  };
  status: "PASS" | "WARNING" | "FAIL";
  status_criteria: {
    error_rate_pct: number;
    timeouts: number;
    backlog_converged: boolean;
    dlq_triggered: boolean;
  };
}
```

- [ ] **Step 2: Verify types compile**

Run: `cd frontend && npx tsc --noEmit`
Expected: no errors.

---

## Task 9: Frontend Capacity Report component

**Files:**
- Create: `frontend/components/capacity-report.tsx`
- Modify: `frontend/components/dashboard-client.tsx`

- [ ] **Step 1: Create the component**

Create `frontend/components/capacity-report.tsx`:
```tsx
"use client";

import React, { useEffect, useState } from "react";
import {
  Activity,
  BarChart3,
  Clock,
  AlertTriangle,
  CheckCircle2,
  XCircle,
  Loader2,
  Gauge,
  Inbox,
} from "lucide-react";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import type { CapacityReport as CapacityReportType, CapacityLatencyBucket } from "@/lib/types";

const CAPACITY_URL = "http://localhost:8082/api/capacity/latest";

function formatMs(ms: number): string {
  if (ms < 1000) return `${Math.round(ms)}ms`;
  return `${(ms / 1000).toFixed(2)}s`;
}

function formatDuration(seconds: number): string {
  const m = Math.floor(seconds / 60);
  const s = Math.round(seconds % 60);
  return m > 0 ? `${m}m ${s}s` : `${s}s`;
}

function formatTimestamp(ts: string): string {
  try {
    return new Date(ts).toLocaleString();
  } catch {
    return ts;
  }
}

function StatusBadge({ status }: { status: CapacityReportType["status"] }) {
  const config = {
    PASS: { icon: CheckCircle2, color: "text-emerald-400 border-emerald-500/40 bg-emerald-500/10" },
    WARNING: { icon: AlertTriangle, color: "text-amber-400 border-amber-500/40 bg-amber-500/10" },
    FAIL: { icon: XCircle, color: "text-red-400 border-red-500/40 bg-red-500/10" },
  }[status];
  const Icon = config.icon;
  return (
    <Badge variant="outline" className={`${config.color} text-sm font-semibold gap-1.5 px-3 py-1`}>
      <Icon className="size-4" />
      {status}
    </Badge>
  );
}

function LatencyTable({ latency }: { latency: CapacityReportType["results"]["latency_ms"] }) {
  const rows: { label: string; data: CapacityLatencyBucket }[] = [
    { label: "API Request", data: latency.api_request },
    { label: "Queue Wait", data: latency.submit_to_processing },
    { label: "Processing", data: latency.processing_duration },
    { label: "End-to-End", data: latency.end_to_end },
  ];

  return (
    <div className="overflow-x-auto">
      <table className="w-full text-sm">
        <thead>
          <tr className="text-zinc-400 text-xs uppercase tracking-wider">
            <th className="text-left py-2 pr-4 font-medium">Segment</th>
            <th className="text-right py-2 px-3 font-medium">p50</th>
            <th className="text-right py-2 px-3 font-medium">p95</th>
            <th className="text-right py-2 px-3 font-medium">p99</th>
            <th className="text-right py-2 px-3 font-medium">min</th>
            <th className="text-right py-2 pl-3 font-medium">max</th>
          </tr>
        </thead>
        <tbody className="text-zinc-200">
          {rows.map((row) => (
            <tr key={row.label} className="border-t border-zinc-800/50">
              <td className="py-2 pr-4 text-zinc-300 font-medium">{row.label}</td>
              <td className="text-right py-2 px-3 tabular-nums">{formatMs(row.data.p50)}</td>
              <td className="text-right py-2 px-3 tabular-nums text-amber-400">{formatMs(row.data.p95)}</td>
              <td className="text-right py-2 px-3 tabular-nums text-red-400">{formatMs(row.data.p99)}</td>
              <td className="text-right py-2 px-3 tabular-nums text-zinc-500">{formatMs(row.data.min)}</td>
              <td className="text-right py-2 pl-3 tabular-nums text-zinc-500">{formatMs(row.data.max)}</td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function CapacityReport() {
  const [report, setReport] = useState<CapacityReportType | null>(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;
    async function fetchReport() {
      try {
        const res = await fetch(CAPACITY_URL);
        if (!res.ok) return;
        const data: unknown = await res.json();
        if (!cancelled && data && typeof data === "object" && "status" in data) {
          setReport(data as CapacityReportType);
        }
      } catch {
        // no capacity report available
      } finally {
        if (!cancelled) setLoading(false);
      }
    }
    fetchReport();
    return () => { cancelled = true; };
  }, []);

  if (loading) {
    return (
      <Card className="bg-zinc-900/60 border-zinc-800/80 text-zinc-100 shadow-lg shadow-black/20 border-t-blue-500/30 border-t-2">
        <CardHeader>
          <CardTitle className="flex items-center gap-2.5 text-lg font-semibold text-zinc-100">
            <Gauge className="size-5 text-blue-400" />
            Capacity Report
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-center gap-3 py-10 text-zinc-500 text-base">
            <Loader2 className="size-5 animate-spin" />
            Loading capacity data...
          </div>
        </CardContent>
      </Card>
    );
  }

  if (!report) {
    return (
      <Card className="bg-zinc-900/60 border-zinc-800/80 text-zinc-100 shadow-lg shadow-black/20 border-t-blue-500/30 border-t-2">
        <CardHeader>
          <CardTitle className="flex items-center gap-2.5 text-lg font-semibold text-zinc-100">
            <Gauge className="size-5 text-blue-400" />
            Capacity Report
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="text-center py-10 text-zinc-500 text-sm">
            No capacity results available. Run{" "}
            <code className="bg-zinc-800 px-2 py-0.5 rounded text-zinc-300">make loadtest</code>{" "}
            to generate baselines.
          </div>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card className="bg-zinc-900/60 border-zinc-800/80 text-zinc-100 shadow-lg shadow-black/20 border-t-blue-500/30 border-t-2">
      <CardHeader>
        <div className="flex items-center justify-between">
          <CardTitle className="flex items-center gap-2.5 text-lg font-semibold text-zinc-100">
            <Gauge className="size-5 text-blue-400" />
            Capacity Report
          </CardTitle>
          <StatusBadge status={report.status} />
        </div>
        <p className="text-xs text-zinc-500 mt-1">
          {formatTimestamp(report.timestamp)}
        </p>
      </CardHeader>
      <CardContent className="space-y-6">
        {/* Config + Throughput */}
        <div className="grid grid-cols-2 lg:grid-cols-4 gap-4">
          <div className="rounded-xl border border-zinc-800/80 bg-zinc-800/30 p-4">
            <div className="flex items-center gap-2 text-zinc-400 mb-2">
              <Activity className="size-4" />
              <span className="text-xs font-medium uppercase tracking-wide">Tasks</span>
            </div>
            <span className="text-2xl font-bold text-zinc-100 tabular-nums">{report.config.tasks}</span>
            <span className="text-sm text-zinc-500 ml-2">@ {report.config.rate}/s</span>
          </div>

          <div className="rounded-xl border border-zinc-800/80 bg-zinc-800/30 p-4">
            <div className="flex items-center gap-2 text-zinc-400 mb-2">
              <BarChart3 className="size-4" />
              <span className="text-xs font-medium uppercase tracking-wide">Throughput</span>
            </div>
            <span className="text-2xl font-bold text-emerald-400 tabular-nums">
              {report.results.throughput_rps.toFixed(2)}
            </span>
            <span className="text-sm text-zinc-500 ml-1">req/s</span>
          </div>

          <div className="rounded-xl border border-zinc-800/80 bg-zinc-800/30 p-4">
            <div className="flex items-center gap-2 text-zinc-400 mb-2">
              <Clock className="size-4" />
              <span className="text-xs font-medium uppercase tracking-wide">Duration</span>
            </div>
            <span className="text-2xl font-bold text-zinc-100 tabular-nums">
              {formatDuration(report.results.duration_seconds)}
            </span>
          </div>

          <div className="rounded-xl border border-zinc-800/80 bg-zinc-800/30 p-4">
            <div className="flex items-center gap-2 text-zinc-400 mb-2">
              <AlertTriangle className="size-4" />
              <span className="text-xs font-medium uppercase tracking-wide">Error Rate</span>
            </div>
            <span className={`text-2xl font-bold tabular-nums ${
              report.status_criteria.error_rate_pct > 5 ? "text-red-400" : "text-emerald-400"
            }`}>
              {report.status_criteria.error_rate_pct.toFixed(1)}%
            </span>
          </div>
        </div>

        {/* Latency Table */}
        <div className="border-t border-zinc-800/80 pt-4">
          <h3 className="text-sm font-medium text-zinc-400 mb-3 uppercase tracking-wide">
            Latency Breakdown
          </h3>
          <LatencyTable latency={report.results.latency_ms} />
        </div>

        {/* Queue Behavior */}
        <div className="border-t border-zinc-800/80 pt-4">
          <h3 className="text-sm font-medium text-zinc-400 mb-3 uppercase tracking-wide flex items-center gap-2">
            <Inbox className="size-4" />
            Queue Behavior
          </h3>
          <div className="grid grid-cols-3 gap-4 text-sm">
            <div>
              <span className="text-zinc-500">Peak Backlog</span>
              <p className="text-lg font-semibold text-zinc-200 tabular-nums">
                {report.queue_metrics.max_visible_messages}
              </p>
            </div>
            <div>
              <span className="text-zinc-500">Peak In-Flight</span>
              <p className="text-lg font-semibold text-zinc-200 tabular-nums">
                {report.queue_metrics.max_inflight_messages}
              </p>
            </div>
            <div>
              <span className="text-zinc-500">Converged</span>
              <p className={`text-lg font-semibold ${report.queue_metrics.backlog_converged ? "text-emerald-400" : "text-red-400"}`}>
                {report.queue_metrics.backlog_converged ? "Yes" : "No"}
              </p>
            </div>
          </div>
        </div>

        {/* Recommendations */}
        <div className="border-t border-zinc-800/80 pt-4">
          <h3 className="text-sm font-medium text-zinc-400 mb-3 uppercase tracking-wide">
            Recommendations
          </h3>
          <div className="space-y-3 text-sm">
            <div className="rounded-lg border border-zinc-800/80 bg-zinc-800/20 px-4 py-3">
              <span className="text-zinc-400 font-medium">VisibilityTimeout</span>
              <div className="flex items-center gap-2 mt-1">
                <span className="text-zinc-500">{report.recommendations.visibility_timeout.current}s</span>
                <span className="text-zinc-600">→</span>
                <span className="text-blue-400 font-semibold">{report.recommendations.visibility_timeout.recommended}s</span>
                <span className="text-zinc-600 text-xs ml-2">({report.recommendations.visibility_timeout.formula})</span>
              </div>
            </div>

            <div className="rounded-lg border border-zinc-800/80 bg-zinc-800/20 px-4 py-3">
              <span className="text-zinc-400 font-medium">Queue Depth</span>
              <p className="text-zinc-300 mt-1">{report.recommendations.queue_max_depth.reason}</p>
            </div>

            <div className="rounded-lg border border-zinc-800/80 bg-zinc-800/20 px-4 py-3">
              <span className="text-zinc-400 font-medium">Worker Scaling</span>
              <p className="text-zinc-300 mt-1">{report.recommendations.worker_concurrency.observation}</p>
            </div>
          </div>
        </div>
      </CardContent>
    </Card>
  );
}
```

- [ ] **Step 2: Add CapacityReport to the dashboard layout**

Modify `frontend/components/dashboard-client.tsx`:

```tsx
"use client";

import { SSEProvider } from "@/components/providers/sse-provider";
import { TaskForm } from "@/components/task-form";
import { EventFeed } from "@/components/event-feed";
import { OperationsSummary } from "@/components/operations-summary";
import { CapacityReport } from "@/components/capacity-report";

export function DashboardClient() {
  return (
    <SSEProvider>
      <div className="w-full space-y-8">
        <OperationsSummary />
        <CapacityReport />
        <div className="grid grid-cols-1 lg:grid-cols-3 gap-8">
          <TaskForm />
          <div className="lg:col-span-2">
            <EventFeed />
          </div>
        </div>
      </div>
    </SSEProvider>
  );
}
```

- [ ] **Step 3: Verify frontend builds**

Run: `cd frontend && npx tsc --noEmit && npm run build`
Expected: no errors.

---

## Task 10: End-to-end validation

This is a manual validation task — no code changes. All previous tasks must be complete.

- [ ] **Step 1: Ensure platform is running with AI enabled**

```bash
docker compose --profile full up -d
```

Wait for all services to be healthy: `make health`

- [ ] **Step 2: Run loadtest with small batch**

```bash
make loadtest TASKS=5 RATE=1 CONCURRENCY=1
```

Verify:
- Terminal prints latency summary with all 4 segments
- `results/loadtest-*.json` file exists and contains correct structure
- Status is PASS, WARNING, or FAIL (not empty)
- Recommendations section is populated

- [ ] **Step 3: Verify API endpoint**

```bash
curl -s http://localhost:8082/api/capacity/latest | python3 -m json.tool
```

Expected: returns the loadtest JSON.

- [ ] **Step 4: Verify frontend**

Open `http://localhost:3001` in browser.

Verify:
- Capacity Report panel is visible below Operations
- Shows "Last Run" timestamp
- Shows config (tasks, rate, concurrency)
- Shows throughput, error rate, duration
- Shows latency breakdown table with all 4 segments
- Shows queue behavior (peak backlog, peak in-flight, converged)
- Shows recommendations section
- Status badge shows correct color (green/amber/red)

If no loadtest has been run, panel should show "No capacity results available. Run `make loadtest` to generate baselines."

- [ ] **Step 5: Run larger loadtest for real baselines**

```bash
make loadtest TASKS=20 RATE=1 CONCURRENCY=2
```

Refresh the frontend — verify the panel updates to show the new result.

Compare the JSON in `results/` with what the frontend displays.

---

## Notes for the implementing agent

- The API runs on port **8082** (not 8080).
- The loadtest binary runs on the **host machine**, not inside Docker. It connects to exposed ports: API 8082, PostgreSQL 5432, LocalStack 4566.
- The `cmd/loadtest/` is a **standalone Go module** with its own `go.mod`. It does NOT import from `services/api/` or `services/worker/`.
- The `results/` directory is **bind-mounted read-only** into the API container so it can serve `GET /api/capacity/latest`.
- The frontend fetches from `http://localhost:8082/api/capacity/latest` — same origin pattern as the existing `OPS_SUMMARY_URL`.
- When running `go mod tidy` in `cmd/loadtest/`, the AWS SDK will pull many indirect dependencies. This is expected.
- For the AI-enabled loadtest (Task 10), `AI_RUNTIME_ENABLED=true` must be set in the worker env and Ollama must be running on the host.
