package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Report struct {
	Timestamp string          `json:"timestamp"`
	Config    ReportConfig    `json:"config"`
	Results   ReportResults   `json:"results"`
	QueueMets ReportQueue     `json:"queue_metrics"`
	Recs      Recommendations `json:"recommendations"`
	Status    string          `json:"status"`
	Criteria  StatusCriteria  `json:"status_criteria"`
}

type ReportConfig struct {
	Tasks       int     `json:"tasks"`
	Rate        float64 `json:"rate"`
	Concurrency int     `json:"concurrency"`
	APIURL      string  `json:"api_url"`
}

type ReportResults struct {
	DurationSeconds float64             `json:"duration_seconds"`
	TasksSubmitted  int                 `json:"tasks_submitted"`
	TasksCompleted  int                 `json:"tasks_completed"`
	TasksFailed     int                 `json:"tasks_failed"`
	ThroughputRPS   float64             `json:"throughput_rps"`
	LatencyMs       ReportLatency       `json:"latency_ms"`
	TokenMetrics    TokenMetricsSummary  `json:"token_metrics"`
}

type ReportLatency struct {
	APIRequest   LatencyBucket `json:"api_request"`
	SubmitToProc LatencyBucket `json:"submit_to_processing"`
	Processing   LatencyBucket `json:"processing_duration"`
	EndToEnd     LatencyBucket `json:"end_to_end"`
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
	ErrorRatePct      float64 `json:"error_rate_pct"`
	Timeouts          int     `json:"timeouts"`
	NegativeDurations int     `json:"negative_durations"`
	Converged         bool    `json:"backlog_converged"`
	DLQTriggered      bool    `json:"dlq_triggered"`
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
	currentVT := 360
	recommendedVT := int(p95Processing/1000) + 30
	recommendedVT = max(recommendedVT, currentVT)

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

	status := determineStatus(errorRate, submitErrors, collected.NegativeDurations, converged)

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
			TokenMetrics: collected.TokenMetrics,
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
			ErrorRatePct:      errorRate,
			Timeouts:          submitErrors,
			NegativeDurations: collected.NegativeDurations,
			Converged:         converged,
			DLQTriggered:      false,
		},
	}
}

func determineStatus(errorRate float64, timeouts int, negativeDurations int, converged bool) string {
	if errorRate > 15 || !converged {
		return "FAIL"
	}
	if errorRate > 5 || timeouts > 0 || negativeDurations > 0 {
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
	fmt.Printf("  Anomalies:   %d negative durations\n", report.Criteria.NegativeDurations)
	fmt.Println()
	fmt.Println("  Latency (ms)          p50       p95       p99       min       max")
	fmt.Println("  ─────────────────────────────────────────────────────────────────")
	printLatencyRow("  API Request     ", report.Results.LatencyMs.APIRequest)
	printLatencyRow("  Queue Wait      ", report.Results.LatencyMs.SubmitToProc)
	printLatencyRow("  Processing      ", report.Results.LatencyMs.Processing)
	printLatencyRow("  End-to-End      ", report.Results.LatencyMs.EndToEnd)
	fmt.Println()

	tm := report.Results.TokenMetrics
	if tm.TotalTokens > 0 {
		fmt.Println("  Token Metrics")
		if tm.Model != "" {
			fmt.Printf("    Model:             %s\n", tm.Model)
		}
		fmt.Printf("    Total Tokens:      %d (%d prompt + %d completion)\n",
			tm.TotalTokens, tm.TotalPromptTokens, tm.TotalCompletionTokens)
		fmt.Printf("    Avg Tokens/s:      %.2f\n", tm.AvgTokensPerSecond)
		fmt.Printf("    Tokens/s (p95):    %.2f\n", tm.TokensPerSecond.P95)
		fmt.Printf("    Output Tokens p95: %.0f\n", tm.OutputTokens.P95)
		fmt.Println()
	}

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

	var statusColor string
	switch report.Status {
	case "WARNING":
		statusColor = "\033[33m"
	case "FAIL":
		statusColor = "\033[31m"
	default:
		statusColor = "\033[32m"
	}
	fmt.Printf("  Status: %s● %s\033[0m\n", statusColor, report.Status)
	fmt.Println()
}

func printLatencyRow(label string, b LatencyBucket) {
	fmt.Printf("%s %8.0f  %8.0f  %8.0f  %8.0f  %8.0f\n",
		label, b.P50, b.P95, b.P99, b.Min, b.Max)
}
