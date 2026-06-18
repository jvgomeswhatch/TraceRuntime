# Phase 9A — Operational Tuning & Capacity

## Objective

Measure real inference latency across the full pipeline, characterize backlog behavior under sustained load, and produce documented operational baselines. Calibrate provisional parameters (VisibilityTimeout, QUEUE_MAX_DEPTH, worker concurrency) based on real data with human-reviewed recommendations.

## Prerequisite

- Ollama running with Qwen and DeepSeek models pulled
- Full pipeline operational (API → SQS → Worker → AI Runtime → S3 → PostgreSQL)
- Phase 8 auto-healing complete

---

## Component 1: Load Test Tool (`cmd/loadtest/`)

Go binary that generates controlled load against the API and measures pipeline performance end-to-end.

### Parameters

| Flag | Default | Description |
|---|---|---|
| `--tasks` | 50 | Total tasks to submit |
| `--rate` | 2 | Requests per second |
| `--concurrency` | 4 | Parallel sender goroutines |
| `--api-url` | `http://localhost:8082` | API endpoint |
| `--db-url` | `DATABASE_URL` env | PostgreSQL connection for result collection |
| `--output-dir` | `results/` | Directory for JSON output |

### Latency Breakdown

The loadtest must capture segmented latency, not just end-to-end. Each task records:

| Metric | Definition |
|---|---|
| `api_request_latency` | Time from HTTP POST to 201 response |
| `submit_to_processing_latency` | Time from task creation (`pending`) to worker pickup (`processing`) — measures queue wait |
| `processing_duration` | Time from `processing_started_at` to `completed_at` — measures worker + AI Runtime |
| `end_to_end_latency` | Time from HTTP POST to task `completed` in PostgreSQL — full pipeline |

This segmentation allows identifying whether the bottleneck is in the API, the queue, the worker, or the inference.

### Result Collection

The loadtest queries PostgreSQL directly to collect task timestamps for latency calculation. No polling against the API — the database is the source of truth.

Flow:
1. Submit all tasks at controlled rate
2. Wait for completion (poll PostgreSQL with 3s interval until all tasks reach terminal state or timeout)
3. Query task timestamps for latency breakdown
4. Calculate percentiles and aggregates
5. Capture queue metrics from SQS (max visible, max inflight observed during test)
6. Print summary to terminal
7. Write JSON to `results/loadtest-YYYY-MM-DD-HHmm.json`

### Queue Metrics Capture

During the loadtest, a background goroutine polls SQS attributes every 5 seconds and records high-water marks:

- `max_visible_messages` — peak backlog observed
- `max_inflight_messages` — peak concurrent processing
- `backlog_converged` — whether visible messages returned to 0 after load stopped

---

## Component 2: JSON Result Format

```json
{
  "timestamp": "2026-06-18T14:30:00Z",
  "config": {
    "tasks": 100,
    "rate": 2,
    "concurrency": 4,
    "api_url": "http://localhost:8080"
  },
  "results": {
    "duration_seconds": 312,
    "tasks_submitted": 100,
    "tasks_completed": 98,
    "tasks_failed": 2,
    "throughput_rps": 1.92,
    "latency_ms": {
      "api_request": { "p50": 45, "p95": 120, "p99": 180, "min": 12, "max": 250 },
      "submit_to_processing": { "p50": 890, "p95": 2100, "p99": 3400, "min": 210, "max": 4500 },
      "processing_duration": { "p50": 1240, "p95": 3870, "p99": 5120, "min": 890, "max": 6200 },
      "end_to_end": { "p50": 2100, "p95": 5800, "p99": 8200, "min": 1100, "max": 10500 }
    }
  },
  "queue_metrics": {
    "max_visible_messages": 12,
    "max_inflight_messages": 4,
    "backlog_converged": true
  },
  "recommendations": {
    "visibility_timeout": {
      "current": 150,
      "recommended": 210,
      "formula": "p95_processing_duration + 30s margin"
    },
    "queue_max_depth": {
      "current": 50,
      "recommendation": "review",
      "reason": "no saturation observed at 100 tasks — current threshold adequate"
    },
    "worker_concurrency": {
      "current": 1,
      "observation": "no resource contention at concurrency=1, consider testing concurrency=2"
    }
  },
  "status": "PASS",
  "status_criteria": {
    "error_rate_pct": 2.0,
    "timeouts": 0,
    "backlog_converged": true,
    "dlq_triggered": false
  }
}
```

### Status Logic

| Status | Criteria |
|---|---|
| PASS | error rate < 5%, no timeouts, backlog converges, no DLQ |
| WARNING | backlog growing, timeouts observed, error rate 5-15% |
| FAIL | DLQ triggered, error rate > 15%, backlog does not converge |

---

## Component 3: API Endpoint

### `GET /api/capacity/latest`

- Reads the most recent JSON from `results/` directory (sorted by filename timestamp)
- Returns the full result object
- Returns 404 if no results exist
- Read-only, no side effects

The `results/` directory path is configurable via `CAPACITY_RESULTS_DIR` env var (default: `./results/`).

---

## Component 4: Frontend — Capacity Report Panel

Component `capacity-report.tsx` in the dashboard. Shows only the most recent result. Not a historical analysis tool.

### Layout

```
+-- Capacity Report -----------------------------------------+
| Last Run: 2026-06-18 14:30              Status: * PASS     |
|                                                            |
| Config               | Throughput                          |
| Tasks: 100           | 1.92 req/s                          |
| Rate: 2/s            | Error Rate: 2%                      |
| Concurrency: 4       | Duration: 5m 12s                    |
|                                                            |
| Latency (ms)                                               |
|                   p50      p95      p99                     |
| API Request        45      120      180                    |
| Queue Wait        890    2,100    3,400                    |
| Processing      1,240    3,870    5,120                    |
| End-to-End      2,100    5,800    8,200                    |
|                                                            |
| Queue Behavior                                             |
| Peak Backlog: 12 messages                                  |
| Peak In-Flight: 4 messages                                 |
| Backlog Converged: Yes                                     |
|                                                            |
| Recommendations                                            |
| VisibilityTimeout                                          |
|   Current: 150s | Suggested: 210s                          |
|   Formula: p95 processing + 30s margin                     |
|                                                            |
| Queue Depth                                                |
|   No saturation observed                                   |
|                                                            |
| Worker Scaling                                             |
|   Consider testing concurrency=2                           |
+------------------------------------------------------------+
```

Styling: shadcn/ui Card, consistent with existing operations-summary and event-feed components. Status badge uses the same color system (green=PASS, amber=WARNING, red=FAIL).

---

## Component 5: Makefile Target

```makefile
loadtest:
	go run ./cmd/loadtest \
		--tasks=$(or $(TASKS),50) \
		--rate=$(or $(RATE),2) \
		--concurrency=$(or $(CONCURRENCY),4)
```

Usage: `make loadtest TASKS=100 RATE=2 CONCURRENCY=4`

---

## Complete Flow

```
make loadtest TASKS=100 RATE=2 CONCURRENCY=4
    |
    v
cmd/loadtest --> POST /tasks (controlled rate)
    |
    v
API --> SQS --> Worker --> AI Runtime (Ollama) --> S3 --> PostgreSQL
    |
    v
SSE: operator observes tasks flowing in real-time on dashboard
    |
    v
loadtest background: captures queue high-water marks from SQS
    |
    v
loadtest: queries PostgreSQL for task timestamps
    |
    v
loadtest: calculates segmented latency percentiles + recommendations
    |
    v
Terminal: summary printed
results/loadtest-2026-06-18-1430.json written
    |
    v
GET /api/capacity/latest --> Frontend Capacity Report panel
    |
    v
Operator analyzes --> manually calibrates Terraform/compose/env
```

---

## Out of Scope

- Grafana dashboard for capacity (metrics already in Prometheus)
- Auto-calibration (`make calibrate`)
- Historical runs in frontend (JSON files for manual comparison)
- Ramp-up/ramp-down patterns (constant rate sufficient for baselines)
- Multiple endpoint load (POST `/tasks` only)
- Frontend as benchmark analysis tool

---

## Files to Create/Modify

| Action | File | Description |
|---|---|---|
| Create | `cmd/loadtest/main.go` | Load test binary |
| Create | `services/api/internal/http/capacity.go` | Handler for `/api/capacity/latest` |
| Modify | `services/api/internal/http/router.go` | Register capacity endpoint |
| Create | `frontend/components/capacity-report.tsx` | Capacity Report panel |
| Modify | `frontend/app/page.tsx` or dashboard layout | Include Capacity Report panel |
| Modify | `Makefile` | Add `loadtest` target |
| Create | `results/.gitkeep` | Ensure directory exists in repo |
| Modify | `.gitignore` | Ignore `results/*.json` |

---

## Runtime Note

The loadtest binary runs on the host machine (via `go run` or compiled binary), not inside Docker. It connects to:
- API via `http://localhost:8080` (exposed port)
- PostgreSQL via `localhost:5432` (exposed port)
- SQS via LocalStack `localhost:4566` (exposed port)

This is intentional: the loadtest is an operator tool, not a platform service. It should not consume container resources that would skew measurements.

---

## Dependencies

- `cmd/loadtest/` needs: `net/http`, `pgx/v5`, `aws-sdk-go-v2` (SQS attributes), `encoding/json`, `math`, `sort`, `flag`
- No new external dependencies for the API (reads files from disk)
- Frontend: no new packages (shadcn/ui Card already available)
