# Phase 11 — Operational Control: Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Transform the dashboard from an investigation tool (Phase 10) into an operational control platform. The operator can act on the system — not just observe — directly from the web interface.

**Architecture:** 4 independent features (Alert Center, Replay Task, DLQ Explorer, Chaos Dashboard) built incrementally. Each feature adds a backend handler, frontend page, and integration test. No cross-feature dependencies except DLQ Explorer uses audit events visible in Alert Center.

**Tech Stack:** Go 1.22+ (chi, pgx/v5, aws-sdk-go-v2), Next.js 14+ (TypeScript, Tailwind, shadcn/ui, SSE), PostgreSQL 16, SQS/LocalStack, filesystem-based chaos reports.

**Specs:**
- [Baseline & Risks](../specs/phase-11-baseline-and-risks.md)
- [Features Part 1: Alert Center + Replay Task](../specs/phase-11-features-part1.md)
- [Features Part 2: DLQ Explorer + Chaos Dashboard](../specs/phase-11-features-part2.md)

---

## Summary of All Tasks

| Task | Feature | Description | Status |
|------|---------|-------------|--------|
| 1 | Alert Center | Migration 000006 — expand `chk_status` to include `acknowledged` | ✅ Done (commit `d6ceca3`) |
| 2 | Alert Center | Backend handler — List, Acknowledge, Stats endpoints | ✅ Done (commit `d6ceca3`) |
| 3 | Alert Center | Wire routes in `router.go` + watchdog resolve query update | ✅ Done (commit `d6ceca3`) |
| 4 | Alert Center | Frontend — Alerts page with filters, pagination, acknowledge, stats | ✅ Done (commit `c885b62`) |
| 5 | Alert Center | Integration test — 7 scenarios (list, filter, paginate, ack, stats) | ✅ Done (commit `c885b62`) |
| 6 | Alert Center | Sidebar nav — add "Alerts" link | ✅ Done (commit `c885b62`) |
| 7 | Replay Task | Migration 000007 — add `replay_of`, `input_payload`, `input_artifact_key` columns + index | ✅ Done (commit `242b5cc`) |
| 8 | Replay Task | Refactor `InsertTask` to use `CreateTaskParams` struct | ✅ Done (commit `242b5cc`) |
| 9 | Replay Task | Backend handler — Replay endpoint with status validation, queue depth check | ✅ Done (commit `242b5cc`) |
| 10 | Replay Task | Wire replay route in `router.go` + OPTIONS handler | ✅ Done (commit `242b5cc`) |
| 11 | Replay Task | Frontend — Replay button + modal (fire-and-close) on Tasks and Trace Details | ✅ Done (commit `242b5cc`) |
| 12 | Replay Task | Integration test — 5 scenarios (replay completed, override, 409, 404, 422) | ✅ Done (commit `242b5cc`) |
| 13 | DLQ Explorer | Migration 000008 — remove `healing_events_event_type_check` constraint | ✅ Done (commit `0306d21`) |
| 14 | DLQ Explorer | Backend handler — ListMessages, Retry, Delete, Purge, Stats endpoints | ✅ Done (commit `0306d21`) |
| 15 | DLQ Explorer | Wire 5 DLQ routes + 3 OPTIONS handlers in `router.go` | ✅ Done (commit `0306d21`) |
| 16 | DLQ Explorer | Frontend — DLQ page with message cards, retry/delete/purge actions | ✅ Done (commit `0306d21`) |
| 17 | DLQ Explorer | Sidebar nav — add "DLQ" link | ✅ Done (commit `0306d21`) |
| 18 | DLQ Explorer | Integration test — list, stats, retry, purge scenarios | ✅ Done (commit `0306d21`) |
| 19 | Chaos Dashboard | Backend handler — ListReports, GetReport (read-only from filesystem) | Pending |
| 20 | Chaos Dashboard | Backend handler — Trigger + Status endpoints (request file mechanism) | Pending |
| 21 | Chaos Dashboard | Wire 4 chaos routes + 1 OPTIONS handler in `router.go`, update `docker-compose.yml` | Pending |
| 22 | Chaos Dashboard | Frontend — Chaos list page with trigger form + report cards | Pending |
| 23 | Chaos Dashboard | Frontend — Chaos report detail page with scenario breakdown + SLO badges | Pending |
| 24 | Chaos Dashboard | Sidebar nav — add "Chaos" link, integration test with fixture JSON | Pending |

---

## Implementation Notes — Completed Features

### Key Fixes Applied During Implementation (Tasks 1-18)

These are documented here for reference and to avoid regressions:

1. **DLQ ReceiveMessage visibility:** Uses `ChangeMessageVisibility(0)` after `ReceiveMessage` instead of `VisibilityTimeout=0` in the request — this prevents messages from disappearing on page refresh (spec RISCO 1 mitigation)
2. **CORS OPTIONS handlers:** Every POST/DELETE browser route needs explicit OPTIONS handler in `router.go` — without this, browsers block preflight requests
3. **Overview DLQ card:** Changed from "Has Messages"/"Empty" to numeric depth count for better operational visibility
4. **Event stream labels:** "Dead Letters" → "DLQ" for consistency with sidebar nav
5. **DLQ badge defaults:** Default status "unknown" → "failed", "Purge All" → "Delete All" button text
6. **NewRouter signature:** NOT changed — new env vars (`SQS_DLQ_URL`) read via `os.Getenv()` inside router per spec RISCO 10 mitigation

---

## Remaining Implementation — Feature 4: Chaos Dashboard

### Architecture Decision (from spec)

The Chaos Dashboard does **NOT** use Docker API and does **NOT** add privileges to containers.

**Mechanism:**
1. API receives POST trigger → validates scenario → writes request file to `results/chaos-requests/{run_id}.json`
2. Operator executes chaos runner via `make chaos-run` (or a one-shot container)
3. Results appear in `results/chaos-suite-{timestamp}.json`
4. Dashboard polls `/api/chaos/status` until report appears

**The dashboard is primarily READ for reports + simplified trigger.**

### Risk Assessment for Chaos Dashboard

| Risk | Severity | Mitigation |
|------|----------|------------|
| Path traversal via report ID | High | Regex validation `^[a-z0-9-]+$` + `filepath.Clean` (defense in depth) |
| Large results directory | Low | Glob limited to 50 most recent files, sorted by mod time DESC |
| Chaos trigger surface attack | Medium | Protected by `X-Internal-Token`, only writes file — does NOT execute chaos |
| results/ mount currently read-only | Medium | Change `results:/app/results:ro` to `results:/app/results` (rw needed for trigger request files) |
| Request dir doesn't exist | Low | Handler checks `os.Stat` before writing, returns 500 if missing |

### Files to Create/Modify

| Action | File | Responsibility | Risk |
|--------|------|----------------|------|
| **Create** | `services/api/internal/handlers/chaos.go` | ChaosHandler: ListReports, GetReport, Trigger, Status | None — new file |
| **Modify** | `services/api/internal/http/router.go` | Register 4 chaos routes + 1 OPTIONS handler | None — addition only |
| **Modify** | `docker-compose.yml` | Change `results` mount to rw, add `CHAOS_REQUESTS_DIR` env | Low — mount change |
| **Modify** | `frontend/components/sidebar.tsx` | Add "Chaos" nav item with Zap icon | None — addition |
| **Create** | `frontend/app/chaos/page.tsx` | Chaos list page + trigger form | None — new file |
| **Create** | `frontend/app/chaos/[reportId]/page.tsx` | Report detail with scenario breakdown | None — new file |
| **Create** | `tests/integration/chaos_test.go` | Integration tests with fixture JSON | None — new file |

**No database changes. No migrations. No existing handler logic altered.**

### Existing Chaos Infrastructure (already built in Phase 9B)

The chaos runner already exists at `cmd/chaos/` with:
- **6 scenarios:** `worker-crash`, `runtime-hang`, `ai-failure`, `postgres-failure`, `queue-flood`, `slow-inference`
- **Report format:** `SuiteReport` struct in `cmd/chaos/internal/chaos/report.go`
- **Output:** JSON files at `results/chaos-suite-{timestamp}.json`
- **Makefile targets:** `make chaos-build`, `make chaos-up`, `make chaos-down`, `make chaos-reset`, `make chaos`

The `SuiteReport` JSON structure:
```json
{
  "version": "1.0",
  "run_id": "chaos-suite-{timestamp}",
  "git_commit": "abc123",
  "environment": "local-docker",
  "timestamp": "2024-01-15T14:35:00Z",
  "duration_seconds": 480,
  "summary": {"total": 6, "passed": 5, "warned": 1, "failed": 0},
  "scenarios": [
    {
      "name": "worker-crash",
      "status": "PASS",
      "duration_seconds": 61,
      "stages": {
        "setup": {"duration_ms": 5000, "status": "ok"},
        "inject": {"duration_ms": 1000, "status": "ok"},
        "observe": {"duration_ms": 45000, "status": "ok"},
        "validate": {"duration_ms": 8000, "status": "ok"},
        "cleanup": {"duration_ms": 2000, "status": "ok"}
      },
      "metrics": {"detection_time_seconds": 3, "recovery_time_seconds": 58},
      "slo_results": [
        {"name": "detection_under_60s", "type": "timing", "expected": 60, "actual": 3, "status": "PASS"}
      ],
      "warnings": [],
      "error": ""
    }
  ]
}
```

---

### Task 19: Backend — ChaosHandler (ListReports + GetReport)

**Files:**
- Create: `services/api/internal/handlers/chaos.go`

**What this does:** Creates the read-only portion of the chaos handler. Reads JSON report files from `results/` directory, parses metadata, returns list and detail.

**Security:** Report ID validated with `^[a-z0-9-]+$` regex + `filepath.Clean` to prevent path traversal. Max 100 chars.

- [ ] **Step 1: Create `services/api/internal/handlers/chaos.go` with ListReports and GetReport**

```go
package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/go-chi/chi/v5"
)

type ChaosHandler struct {
	resultsDir string
	requestDir string
}

func NewChaosHandler(resultsDir, requestDir string) *ChaosHandler {
	return &ChaosHandler{resultsDir: resultsDir, requestDir: requestDir}
}

var validReportID = regexp.MustCompile(`^[a-z0-9-]+$`)

func isValidReportID(id string) bool {
	return len(id) > 0 && len(id) < 100 && validReportID.MatchString(id)
}

type chaosReportSummary struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Warned int `json:"warned"`
	Failed int `json:"failed"`
}

type chaosReportEntry struct {
	ID              string             `json:"id"`
	File            string             `json:"file"`
	Timestamp       string             `json:"timestamp"`
	DurationSeconds float64            `json:"duration_seconds"`
	Summary         chaosReportSummary `json:"summary"`
	Scenarios       []string           `json:"scenarios"`
}

func (h *ChaosHandler) ListReports(w http.ResponseWriter, r *http.Request) {
	pattern := filepath.Join(h.resultsDir, "chaos-suite-*.json")
	files, err := filepath.Glob(pattern)
	if err != nil {
		files = []string{}
	}

	sort.Slice(files, func(i, j int) bool {
		infoI, errI := os.Stat(files[i])
		infoJ, errJ := os.Stat(files[j])
		if errI != nil || errJ != nil {
			return false
		}
		return infoI.ModTime().After(infoJ.ModTime())
	})

	if len(files) > 50 {
		files = files[:50]
	}

	reports := make([]chaosReportEntry, 0, len(files))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var raw struct {
			RunID           string             `json:"run_id"`
			Timestamp       string             `json:"timestamp"`
			DurationSeconds float64            `json:"duration_seconds"`
			Summary         chaosReportSummary `json:"summary"`
			Scenarios       []struct {
				Name string `json:"name"`
			} `json:"scenarios"`
		}
		if err := json.Unmarshal(data, &raw); err != nil {
			continue
		}

		base := filepath.Base(f)
		id := base[:len(base)-len(".json")]

		scenarioNames := make([]string, 0, len(raw.Scenarios))
		for _, s := range raw.Scenarios {
			scenarioNames = append(scenarioNames, s.Name)
		}

		reports = append(reports, chaosReportEntry{
			ID:              id,
			File:            base,
			Timestamp:       raw.Timestamp,
			DurationSeconds: raw.DurationSeconds,
			Summary:         raw.Summary,
			Scenarios:       scenarioNames,
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"reports": reports,
		"total":   len(reports),
	})
}

func (h *ChaosHandler) GetReport(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "reportId")
	if !isValidReportID(id) {
		jsonError(w, "invalid report id", http.StatusBadRequest)
		return
	}

	path := filepath.Clean(filepath.Join(h.resultsDir, id+".json"))
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		jsonError(w, "report not found", http.StatusNotFound)
		return
	}
	if err != nil {
		jsonError(w, "failed to read report", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd services/api && go build ./...
```

Expected: no errors. The `jsonError` function is already defined in `services/api/internal/handlers/` (used by dlq.go and alerts.go).

---

### Task 20: Backend — Trigger + Status endpoints

**Files:**
- Modify: `services/api/internal/handlers/chaos.go` (append methods)

**What this does:** Adds the write portion — trigger creates a request file on disk, status checks if request or report exists.

**Security:**
- Scenario validated against whitelist (only 7 valid values)
- Request dir existence checked before writing
- Trigger does NOT execute chaos — only creates a file for external execution
- All IDs validated with same regex as GetReport

- [ ] **Step 1: Add imports, Trigger and Status methods to chaos.go**

Add `"fmt"` and `"time"` to the import block, then append:

```go
type chaosRequest struct {
	RunID    string `json:"run_id"`
	Scenario string `json:"scenario"`
	Timeout  int    `json:"timeout_seconds"`
	QueuedAt string `json:"queued_at"`
}

type triggerRequest struct {
	Scenario string `json:"scenario"`
	Timeout  int    `json:"timeout_seconds"`
}

var validScenarios = map[string]bool{
	"worker-crash":     true,
	"runtime-hang":     true,
	"ai-failure":       true,
	"postgres-failure": true,
	"queue-flood":      true,
	"slow-inference":   true,
	"all":              true,
}

func (h *ChaosHandler) Trigger(w http.ResponseWriter, r *http.Request) {
	var req triggerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid request body", http.StatusBadRequest)
		return
	}

	if !validScenarios[req.Scenario] {
		jsonError(w, "invalid scenario", http.StatusBadRequest)
		return
	}

	if req.Timeout <= 0 {
		req.Timeout = 300
	}

	if _, err := os.Stat(h.requestDir); os.IsNotExist(err) {
		jsonError(w, "chaos request directory unavailable", http.StatusInternalServerError)
		return
	}

	now := time.Now().UTC()
	runID := fmt.Sprintf("chaos-%s-%s", req.Scenario, now.Format("20060102-150405"))

	data, _ := json.MarshalIndent(chaosRequest{
		RunID:    runID,
		Scenario: req.Scenario,
		Timeout:  req.Timeout,
		QueuedAt: now.Format(time.RFC3339),
	}, "", "  ")

	requestFile := filepath.Clean(filepath.Join(h.requestDir, runID+".json"))
	if err := os.WriteFile(requestFile, data, 0o644); err != nil {
		jsonError(w, "failed to queue scenario", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]any{
		"status":   "queued",
		"run_id":   runID,
		"scenario": req.Scenario,
		"message":  "Scenario queued. Execute 'make chaos-run' or wait for chaos-runner watcher.",
		"poll_url": "/api/chaos/status?run_id=" + runID,
	})
}

func (h *ChaosHandler) Status(w http.ResponseWriter, r *http.Request) {
	runID := r.URL.Query().Get("run_id")
	if !isValidReportID(runID) {
		jsonError(w, "invalid run_id", http.StatusBadRequest)
		return
	}

	reportPath := filepath.Clean(filepath.Join(h.resultsDir, runID+".json"))
	if _, err := os.Stat(reportPath); err == nil {
		data, err := os.ReadFile(reportPath)
		if err == nil {
			var raw struct {
				Summary chaosReportSummary `json:"summary"`
			}
			json.Unmarshal(data, &raw)

			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"run_id":     runID,
				"status":     "completed",
				"summary":    raw.Summary,
				"report_url": "/api/chaos/reports/" + runID,
			})
			return
		}
	}

	requestPath := filepath.Clean(filepath.Join(h.requestDir, runID+".json"))
	if info, err := os.Stat(requestPath); err == nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"run_id":    runID,
			"status":    "queued",
			"queued_at": info.ModTime().UTC().Format(time.RFC3339),
		})
		return
	}

	jsonError(w, "run not found", http.StatusNotFound)
}
```

- [ ] **Step 2: Verify compilation**

```bash
cd services/api && go build ./...
```

Expected: no errors.

---

### Task 21: Wire routes in router.go + docker-compose changes

**Files:**
- Modify: `services/api/internal/http/router.go` (add routes after DLQ block)
- Modify: `docker-compose.yml` (results mount + env var)

**What changes in existing files:**

In `router.go`:
- Add `"path/filepath"` to imports
- Add 5 route registrations after the DLQ Explorer block (line ~83)
- Pattern follows exact same convention as DLQ routes (tokenMw wrapper)

In `docker-compose.yml`:
- Change `./results:/app/results:ro` to `./results:/app/results` (remove read-only — needed for trigger request files)
- Add `CHAOS_REQUESTS_DIR=/app/results/chaos-requests` env var to API service

**Risk:** Changing results mount from `ro` to `rw` means the API process can now write to results. This is intentional and limited to the `chaos-requests/` subdirectory. The chaos handler only writes to `h.requestDir` (validated path).

- [ ] **Step 1: Add chaos routes to router.go**

After the DLQ Explorer block (after line 83, after the closing `}`), add:

```go
	// Chaos Dashboard (Phase 11)
	chaosResultsDir := os.Getenv("CAPACITY_RESULTS_DIR")
	chaosRequestDir := os.Getenv("CHAOS_REQUESTS_DIR")
	if chaosResultsDir == "" {
		chaosResultsDir = "results"
	}
	if chaosRequestDir == "" {
		chaosRequestDir = filepath.Join(chaosResultsDir, "chaos-requests")
	}
	chaosHandler := handlers.NewChaosHandler(chaosResultsDir, chaosRequestDir)
	r.Get("/api/chaos/reports", tokenMw(http.HandlerFunc(chaosHandler.ListReports)).ServeHTTP)
	r.Get("/api/chaos/reports/{reportId}", tokenMw(http.HandlerFunc(chaosHandler.GetReport)).ServeHTTP)
	r.Post("/api/chaos/trigger", tokenMw(http.HandlerFunc(chaosHandler.Trigger)).ServeHTTP)
	r.Options("/api/chaos/trigger", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	r.Get("/api/chaos/status", tokenMw(http.HandlerFunc(chaosHandler.Status)).ServeHTTP)
```

Add `"path/filepath"` to the imports block.

- [ ] **Step 2: Update docker-compose.yml**

In the `api` service:

Change the results volume mount:
```yaml
# Before:
      - ./results:/app/results:ro
# After:
      - ./results:/app/results
```

Add env var (after `TEMPO_URL`):
```yaml
      - CHAOS_REQUESTS_DIR=/app/results/chaos-requests
```

- [ ] **Step 3: Create chaos-requests directory**

```bash
mkdir -p results/chaos-requests
```

Add `results/chaos-requests/` to `.gitignore` if not already covered by existing patterns.

- [ ] **Step 4: Verify compilation**

```bash
cd services/api && go build ./...
```

Expected: no errors.

---

### Task 22: Frontend — Chaos list page with trigger form

**Files:**
- Create: `frontend/app/chaos/page.tsx`

**What this does:** Main chaos page showing:
- Trigger form (scenario dropdown + timeout + Run button)
- Trigger status indicator (queued → polling → completed)
- List of recent reports as clickable cards

**Frontend patterns followed (matching existing pages):**
- Same API_URL/TOKEN pattern as `dlq/page.tsx`
- Same card styling (rounded-xl, border-zinc-800/80, bg-zinc-900/60)
- Same header pattern (icon + title + subtitle)
- Same button styling (emerald for primary actions, zinc for secondary)
- Same loading/empty state pattern
- Same Badge component usage from shadcn/ui
- Auto-polling for trigger status (5s interval while queued)

**IMPORTANT:** Check `frontend/AGENTS.md` — "This is NOT the Next.js you know. Read the relevant guide in `node_modules/next/dist/docs/` before writing any code." The existing pages (dlq, alerts) use standard `"use client"` pattern with `useState`/`useEffect` — follow exactly.

- [ ] **Step 1: Create `frontend/app/chaos/page.tsx`**

```tsx
"use client";

import React, { useCallback, useEffect, useState } from "react";
import Link from "next/link";
import { Zap, RefreshCw, Play, CheckCircle, AlertTriangle, XCircle } from "lucide-react";
import { Badge } from "@/components/ui/badge";

const API_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8082";
const TOKEN = process.env.NEXT_PUBLIC_INTERNAL_TOKEN || "";

const SCENARIOS = [
  "worker-crash",
  "runtime-hang",
  "ai-failure",
  "postgres-failure",
  "queue-flood",
  "slow-inference",
  "all",
] as const;

interface ReportSummary {
  total: number;
  passed: number;
  warned: number;
  failed: number;
}

interface ChaosReport {
  id: string;
  file: string;
  timestamp: string;
  duration_seconds: number;
  summary: ReportSummary;
  scenarios: string[];
}

interface TriggerState {
  run_id: string;
  status: "idle" | "queued" | "completed";
  summary?: ReportSummary;
}

function formatDuration(seconds: number): string {
  const m = Math.floor(seconds / 60);
  const s = Math.round(seconds % 60);
  return m > 0 ? `${m}m ${s}s` : `${s}s`;
}

function SummaryBadges({ summary }: { summary: ReportSummary }) {
  return (
    <div className="flex items-center gap-2">
      {summary.passed > 0 && (
        <span className="flex items-center gap-1 text-xs text-emerald-400">
          <CheckCircle className="size-3" />
          {summary.passed} passed
        </span>
      )}
      {summary.warned > 0 && (
        <span className="flex items-center gap-1 text-xs text-amber-400">
          <AlertTriangle className="size-3" />
          {summary.warned} warned
        </span>
      )}
      {summary.failed > 0 && (
        <span className="flex items-center gap-1 text-xs text-red-400">
          <XCircle className="size-3" />
          {summary.failed} failed
        </span>
      )}
    </div>
  );
}

export default function ChaosPage() {
  const [reports, setReports] = useState<ChaosReport[]>([]);
  const [loading, setLoading] = useState(false);
  const [scenario, setScenario] = useState<string>("worker-crash");
  const [timeout, setTimeoutVal] = useState(300);
  const [trigger, setTrigger] = useState<TriggerState>({ run_id: "", status: "idle" });

  const headers: Record<string, string> = TOKEN ? { "X-Internal-Token": TOKEN } : {};

  const fetchReports = useCallback(async () => {
    setLoading(true);
    try {
      const res = await fetch(`${API_URL}/api/chaos/reports`, { headers });
      if (res.ok) {
        const data = await res.json();
        setReports(data.reports || []);
      }
    } catch {
      /* ignore */
    }
    setLoading(false);
  }, []);

  useEffect(() => {
    fetchReports();
  }, [fetchReports]);

  useEffect(() => {
    if (trigger.status !== "queued" || !trigger.run_id) return;
    const interval = setInterval(async () => {
      try {
        const res = await fetch(
          `${API_URL}/api/chaos/status?run_id=${encodeURIComponent(trigger.run_id)}`,
          { headers }
        );
        if (res.ok) {
          const data = await res.json();
          if (data.status === "completed") {
            setTrigger({
              run_id: trigger.run_id,
              status: "completed",
              summary: data.summary,
            });
            fetchReports();
          }
        }
      } catch {
        /* ignore */
      }
    }, 5000);
    return () => clearInterval(interval);
  }, [trigger.status, trigger.run_id, fetchReports]);

  const triggerScenario = async () => {
    try {
      const res = await fetch(`${API_URL}/api/chaos/trigger`, {
        method: "POST",
        headers: { ...headers, "Content-Type": "application/json" },
        body: JSON.stringify({ scenario, timeout_seconds: timeout }),
      });
      if (res.ok) {
        const data = await res.json();
        setTrigger({ run_id: data.run_id, status: "queued" });
      }
    } catch {
      /* ignore */
    }
  };

  return (
    <div className="px-6 py-5 lg:px-8">
      <header className="mb-5">
        <h1 className="text-xl font-semibold text-zinc-100 tracking-tight flex items-center gap-2.5">
          <Zap className="size-5 text-zinc-400" />
          Chaos Dashboard
        </h1>
        <p className="text-sm text-zinc-500 mt-0.5">
          Trigger chaos scenarios and review resilience reports
        </p>
      </header>

      {/* Trigger */}
      <div className="rounded-xl border border-zinc-800/80 bg-zinc-900/60 p-4 mb-5">
        <h2 className="text-xs font-medium text-zinc-500 uppercase tracking-wide mb-3">
          Trigger Scenario
        </h2>
        <div className="flex items-end gap-3 flex-wrap">
          <div className="flex-1 min-w-[160px]">
            <label className="text-[10px] text-zinc-500 block mb-1">Scenario</label>
            <select
              value={scenario}
              onChange={(e) => setScenario(e.target.value)}
              className="w-full bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-1.5 text-sm text-zinc-200 focus:outline-none focus:border-zinc-600"
            >
              {SCENARIOS.map((s) => (
                <option key={s} value={s}>{s}</option>
              ))}
            </select>
          </div>
          <div className="w-28">
            <label className="text-[10px] text-zinc-500 block mb-1">Timeout (s)</label>
            <input
              type="number"
              value={timeout}
              onChange={(e) => setTimeoutVal(Number(e.target.value))}
              min={30}
              max={3600}
              className="w-full bg-zinc-950 border border-zinc-800 rounded-lg px-3 py-1.5 text-sm text-zinc-200 focus:outline-none focus:border-zinc-600"
            />
          </div>
          <button
            onClick={triggerScenario}
            disabled={trigger.status === "queued"}
            className="inline-flex items-center gap-1.5 text-xs font-medium text-emerald-400 hover:text-emerald-300 border border-emerald-500/30 bg-emerald-500/5 hover:bg-emerald-500/10 rounded-lg px-4 py-1.5 transition-colors disabled:opacity-50"
          >
            <Play className="size-3" />
            Run Chaos
          </button>
        </div>
        {trigger.status === "queued" && (
          <div className="mt-3 text-xs text-amber-400 flex items-center gap-1.5">
            <span className="inline-block size-2 rounded-full bg-amber-400 animate-pulse" />
            Queued — run &apos;make chaos-run&apos; to execute (polling for result...)
          </div>
        )}
        {trigger.status === "completed" && trigger.summary && (
          <div className="mt-3 flex items-center gap-3">
            <span className="text-xs text-emerald-400 flex items-center gap-1.5">
              <CheckCircle className="size-3" />
              Completed
            </span>
            <SummaryBadges summary={trigger.summary} />
            <Link
              href={`/chaos/${trigger.run_id}`}
              className="text-xs text-zinc-400 hover:text-zinc-200 underline"
            >
              View Report
            </Link>
          </div>
        )}
      </div>

      {/* Reports */}
      <div className="flex items-center justify-between mb-3">
        <h2 className="text-xs font-medium text-zinc-500 uppercase tracking-wide">
          Recent Reports
        </h2>
        <button
          onClick={fetchReports}
          disabled={loading}
          className="inline-flex items-center gap-1.5 text-xs text-zinc-400 hover:text-zinc-200 border border-zinc-800 bg-zinc-900/60 rounded-lg px-3 py-1.5 transition-colors disabled:opacity-50"
        >
          <RefreshCw className={`size-3 ${loading ? "animate-spin" : ""}`} />
          Refresh
        </button>
      </div>

      {reports.length === 0 && !loading ? (
        <div className="flex flex-col items-center justify-center py-16 text-zinc-500">
          <Zap className="size-8 text-zinc-600 mb-3" />
          <p className="text-sm">No chaos reports found</p>
          <p className="text-xs text-zinc-600 mt-1">
            Run &apos;make chaos&apos; to generate reports
          </p>
        </div>
      ) : (
        <div className="space-y-3">
          {reports.map((report) => (
            <Link
              key={report.id}
              href={`/chaos/${report.id}`}
              className="block rounded-xl border border-zinc-800/80 bg-zinc-900/60 p-4 hover:border-zinc-700 transition-colors"
            >
              <div className="flex items-start justify-between mb-2">
                <div>
                  <p className="text-sm font-mono text-zinc-200">{report.id}</p>
                  <p className="text-[10px] text-zinc-500 mt-0.5">
                    {report.timestamp ? new Date(report.timestamp).toLocaleString() : "—"}
                    {report.duration_seconds > 0 &&
                      ` · ${formatDuration(report.duration_seconds)}`}
                    {` · ${report.summary.total} scenarios`}
                  </p>
                </div>
                <Badge
                  variant="outline"
                  className={`text-[10px] uppercase font-semibold px-1.5 py-0 ${
                    report.summary.failed > 0
                      ? "text-red-400 border-red-500/40 bg-red-500/10"
                      : report.summary.warned > 0
                        ? "text-amber-400 border-amber-500/40 bg-amber-500/10"
                        : "text-emerald-400 border-emerald-500/40 bg-emerald-500/10"
                  }`}
                >
                  {report.summary.failed > 0
                    ? "FAIL"
                    : report.summary.warned > 0
                      ? "WARN"
                      : "PASS"}
                </Badge>
              </div>
              <SummaryBadges summary={report.summary} />
            </Link>
          ))}
        </div>
      )}
    </div>
  );
}
```

- [ ] **Step 2: Verify frontend compiles**

```bash
cd frontend && npm run build
```

Expected: no errors.

---

### Task 23: Frontend — Chaos report detail page

**Files:**
- Create: `frontend/app/chaos/[reportId]/page.tsx`

**What this does:** Displays the full chaos report — scenario breakdown with stages (setup→inject→observe→validate→cleanup), metrics (detection/recovery time), SLO badges, warnings, and errors.

**Matches the SuiteReport JSON structure** from `cmd/chaos/internal/chaos/report.go`.

- [ ] **Step 1: Create `frontend/app/chaos/[reportId]/page.tsx`**

```tsx
"use client";

import React, { useEffect, useState } from "react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { ArrowLeft, CheckCircle, AlertTriangle, XCircle, Zap } from "lucide-react";
import { Badge } from "@/components/ui/badge";

const API_URL = process.env.NEXT_PUBLIC_API_URL || "http://localhost:8082";
const TOKEN = process.env.NEXT_PUBLIC_INTERNAL_TOKEN || "";

interface SLOResult {
  name: string;
  type: string;
  expected: number | string;
  actual: number | string;
  status: "PASS" | "WARN" | "FAIL";
}

interface StageResult {
  duration_ms: number;
  status: string;
}

interface ScenarioReport {
  name: string;
  status: "PASS" | "WARN" | "FAIL";
  duration_seconds: number;
  stages: Record<string, StageResult>;
  metrics: Record<string, number | string>;
  slo_results: SLOResult[];
  warnings: string[];
  error?: string;
}

interface SuiteReport {
  version: string;
  run_id: string;
  git_commit: string;
  environment: string;
  timestamp: string;
  duration_seconds: number;
  summary: { total: number; passed: number; warned: number; failed: number };
  scenarios: ScenarioReport[];
}

const STAGE_ORDER = ["setup", "inject", "observe", "validate", "cleanup"];

function StatusIcon({ status }: { status: string }) {
  switch (status) {
    case "PASS":
      return <CheckCircle className="size-4 text-emerald-400" />;
    case "WARN":
      return <AlertTriangle className="size-4 text-amber-400" />;
    case "FAIL":
      return <XCircle className="size-4 text-red-400" />;
    default:
      return <span className="size-4" />;
  }
}

function formatMs(ms: number): string {
  if (ms < 1000) return `${ms}ms`;
  return `${(ms / 1000).toFixed(1)}s`;
}

export default function ChaosReportDetailPage() {
  const params = useParams();
  const reportId = params.reportId as string;
  const [report, setReport] = useState<SuiteReport | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    const fetchReport = async () => {
      try {
        const res = await fetch(`${API_URL}/api/chaos/reports/${reportId}`, {
          headers: TOKEN ? { "X-Internal-Token": TOKEN } : {},
        });
        if (!res.ok) {
          setError(res.status === 404 ? "Report not found" : "Failed to load report");
          return;
        }
        setReport(await res.json());
      } catch {
        setError("Failed to load report");
      }
    };
    fetchReport();
  }, [reportId]);

  if (error) {
    return (
      <div className="px-6 py-5 lg:px-8">
        <Link
          href="/chaos"
          className="flex items-center gap-1 text-xs text-zinc-400 hover:text-zinc-200 mb-4"
        >
          <ArrowLeft className="size-3" />
          Back to Chaos Dashboard
        </Link>
        <div className="flex flex-col items-center justify-center py-16 text-zinc-500">
          <XCircle className="size-8 text-red-500/50 mb-3" />
          <p className="text-sm">{error}</p>
        </div>
      </div>
    );
  }

  if (!report) {
    return (
      <div className="px-6 py-5 lg:px-8">
        <div className="animate-pulse space-y-4">
          <div className="h-4 bg-zinc-800 rounded w-48" />
          <div className="h-32 bg-zinc-900 rounded-xl" />
        </div>
      </div>
    );
  }

  return (
    <div className="px-6 py-5 lg:px-8">
      <Link
        href="/chaos"
        className="flex items-center gap-1 text-xs text-zinc-400 hover:text-zinc-200 mb-4"
      >
        <ArrowLeft className="size-3" />
        Back to Chaos Dashboard
      </Link>

      <header className="mb-5">
        <div className="flex items-center gap-2.5">
          <Zap className="size-5 text-zinc-400" />
          <h1 className="text-lg font-semibold text-zinc-100 font-mono">{report.run_id}</h1>
        </div>
        <p className="text-xs text-zinc-500 mt-1">
          {new Date(report.timestamp).toLocaleString()}
          {` · Duration: ${Math.floor(report.duration_seconds / 60)}m ${Math.round(report.duration_seconds % 60)}s`}
          {report.git_commit && ` · Git: ${report.git_commit.slice(0, 7)}`}
          {report.environment && ` · ${report.environment}`}
        </p>
      </header>

      {/* Summary */}
      <div className="grid grid-cols-4 gap-3 mb-5">
        {[
          { label: "Total", value: report.summary.total, color: "text-zinc-100" },
          { label: "Passed", value: report.summary.passed, color: "text-emerald-400" },
          { label: "Warned", value: report.summary.warned, color: "text-amber-400" },
          { label: "Failed", value: report.summary.failed, color: "text-red-400" },
        ].map((item) => (
          <div
            key={item.label}
            className="rounded-xl border border-zinc-800/80 bg-zinc-900/60 px-4 py-3 text-center"
          >
            <p className="text-[10px] font-medium text-zinc-500 uppercase tracking-wide">
              {item.label}
            </p>
            <p className={`text-lg font-semibold tabular-nums ${item.color}`}>
              {item.value}
            </p>
          </div>
        ))}
      </div>

      {/* Scenarios */}
      <div className="space-y-3">
        {report.scenarios.map((sc) => (
          <div
            key={sc.name}
            className="rounded-xl border border-zinc-800/80 bg-zinc-900/60 p-4"
          >
            <div className="flex items-center justify-between mb-3">
              <div className="flex items-center gap-2">
                <StatusIcon status={sc.status} />
                <span className="text-sm font-mono text-zinc-200">{sc.name}</span>
                <span className="text-xs text-zinc-500">
                  {sc.duration_seconds.toFixed(0)}s
                </span>
              </div>
              <Badge
                variant="outline"
                className={`text-[10px] uppercase font-semibold px-1.5 py-0 ${
                  sc.status === "FAIL"
                    ? "text-red-400 border-red-500/40 bg-red-500/10"
                    : sc.status === "WARN"
                      ? "text-amber-400 border-amber-500/40 bg-amber-500/10"
                      : "text-emerald-400 border-emerald-500/40 bg-emerald-500/10"
                }`}
              >
                {sc.status}
              </Badge>
            </div>

            {/* Metrics */}
            {Object.keys(sc.metrics || {}).length > 0 && (
              <div className="flex gap-4 mb-3 flex-wrap">
                {Object.entries(sc.metrics).map(([key, val]) => (
                  <div key={key} className="text-xs">
                    <span className="text-zinc-500">
                      {key.replace(/_/g, " ")}:{" "}
                    </span>
                    <span className="text-zinc-300 tabular-nums">
                      {typeof val === "number" ? `${val}s` : String(val)}
                    </span>
                  </div>
                ))}
              </div>
            )}

            {/* Stages */}
            <div className="space-y-1">
              {STAGE_ORDER.map((stage) => {
                const s = sc.stages?.[stage];
                if (!s) return null;
                return (
                  <div key={stage} className="flex items-center gap-2 text-xs">
                    <span
                      className={
                        s.status === "ok"
                          ? "text-emerald-400"
                          : "text-amber-400"
                      }
                    >
                      {s.status === "ok" ? "✓" : "▲"}
                    </span>
                    <span className="text-zinc-400 w-16">{stage}</span>
                    <span className="text-zinc-500 tabular-nums">
                      {formatMs(s.duration_ms)}
                    </span>
                  </div>
                );
              })}
            </div>

            {/* SLOs */}
            {sc.slo_results && sc.slo_results.length > 0 && (
              <div className="mt-3 pt-3 border-t border-zinc-800/60">
                <p className="text-[10px] text-zinc-500 uppercase tracking-wide mb-1.5">
                  SLOs
                </p>
                <div className="space-y-1">
                  {sc.slo_results.map((slo, i) => (
                    <div key={i} className="flex items-center gap-2 text-xs">
                      <StatusIcon status={slo.status} />
                      <span className="text-zinc-300">{slo.name}</span>
                      <span className="text-zinc-500">
                        actual: {String(slo.actual)} / expected:{" "}
                        {String(slo.expected)}
                      </span>
                    </div>
                  ))}
                </div>
              </div>
            )}

            {/* Warnings */}
            {sc.warnings && sc.warnings.length > 0 && (
              <div className="mt-3 pt-3 border-t border-zinc-800/60">
                {sc.warnings.map((w, i) => (
                  <p
                    key={i}
                    className="text-xs text-amber-400 flex items-center gap-1.5"
                  >
                    <AlertTriangle className="size-3 shrink-0" />
                    {w}
                  </p>
                ))}
              </div>
            )}

            {/* Error */}
            {sc.error && (
              <div className="mt-3 pt-3 border-t border-zinc-800/60">
                <p className="text-xs text-red-400 flex items-center gap-1.5">
                  <XCircle className="size-3 shrink-0" />
                  {sc.error}
                </p>
              </div>
            )}
          </div>
        ))}
      </div>
    </div>
  );
}
```

- [ ] **Step 2: Verify frontend compiles**

```bash
cd frontend && npm run build
```

Expected: no errors.

---

### Task 24: Sidebar nav + Integration tests

**Files:**
- Modify: `frontend/components/sidebar.tsx` (add Chaos nav item)
- Create: `tests/integration/chaos_test.go`

**What this does:**
1. Adds "Chaos" as the last item in sidebar nav (after DLQ)
2. Creates integration tests using a fixture JSON file written to `results/` during test setup

**Integration test approach:** The chaos test is unique — it doesn't need running services beyond the API. It writes a fixture JSON file to the results directory and tests the API endpoints against it. The trigger test writes to `chaos-requests/` and verifies the file was created.

- [ ] **Step 1: Add "Chaos" to sidebar**

In `frontend/components/sidebar.tsx`:

Add `Zap` to the lucide-react import:
```typescript
import { Activity, LayoutDashboard, ListTodo, GitBranch, Bell, Inbox, Zap } from "lucide-react";
```

Add to `NAV_ITEMS` array (last item):
```typescript
  { href: "/chaos", label: "Chaos", icon: Zap },
```

- [ ] **Step 2: Create `tests/integration/chaos_test.go`**

```go
package integration

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestChaosEndpoints(t *testing.T) {
	token := envOr("INTERNAL_TOKEN", "")
	resultsDir := envOr("CAPACITY_RESULTS_DIR", "../../results")

	fixture := map[string]any{
		"version":          "1.0",
		"run_id":           "chaos-suite-test-fixture",
		"git_commit":       "abc1234",
		"environment":      "test",
		"timestamp":        "2024-01-15T14:35:00Z",
		"duration_seconds": 61,
		"summary":          map[string]int{"total": 1, "passed": 1, "warned": 0, "failed": 0},
		"scenarios": []map[string]any{
			{
				"name":             "worker-crash",
				"status":           "PASS",
				"duration_seconds": 61,
				"stages": map[string]any{
					"setup":    map[string]any{"duration_ms": 5000, "status": "ok"},
					"inject":   map[string]any{"duration_ms": 1000, "status": "ok"},
					"observe":  map[string]any{"duration_ms": 45000, "status": "ok"},
					"validate": map[string]any{"duration_ms": 8000, "status": "ok"},
					"cleanup":  map[string]any{"duration_ms": 2000, "status": "ok"},
				},
				"metrics":     map[string]any{"detection_time_seconds": 3, "recovery_time_seconds": 58},
				"slo_results": []map[string]any{{"name": "detection_under_60s", "type": "timing", "expected": 60, "actual": 3, "status": "PASS"}},
				"warnings":    []string{},
			},
		},
	}

	fixtureData, _ := json.MarshalIndent(fixture, "", "  ")
	fixturePath := filepath.Join(resultsDir, "chaos-suite-test-fixture.json")
	if err := os.MkdirAll(resultsDir, 0o755); err != nil {
		t.Fatalf("create results dir: %v", err)
	}
	if err := os.WriteFile(fixturePath, fixtureData, 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	t.Cleanup(func() { os.Remove(fixturePath) })

	t.Run("ListReports", func(t *testing.T) {
		req, _ := http.NewRequest("GET", apiURL+"/api/chaos/reports", nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
		}
		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		reports, ok := result["reports"].([]any)
		if !ok {
			t.Fatal("reports is not an array")
		}
		found := false
		for _, r := range reports {
			entry, ok := r.(map[string]any)
			if ok && entry["id"] == "chaos-suite-test-fixture" {
				found = true
				break
			}
		}
		if !found {
			t.Fatal("fixture report not found in list")
		}
	})

	t.Run("GetReport", func(t *testing.T) {
		req, _ := http.NewRequest("GET", apiURL+"/api/chaos/reports/chaos-suite-test-fixture", nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
		}
		var report map[string]any
		json.NewDecoder(resp.Body).Decode(&report)
		if report["run_id"] != "chaos-suite-test-fixture" {
			t.Fatalf("expected run_id=chaos-suite-test-fixture, got %v", report["run_id"])
		}
		scenarios, ok := report["scenarios"].([]any)
		if !ok || len(scenarios) != 1 {
			t.Fatalf("expected 1 scenario, got %v", report["scenarios"])
		}
	})

	t.Run("GetReportNotFound", func(t *testing.T) {
		req, _ := http.NewRequest("GET", apiURL+"/api/chaos/reports/nonexistent-report", nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("PathTraversalBlocked", func(t *testing.T) {
		paths := []string{
			"/api/chaos/reports/..%2F..%2Fetc%2Fpasswd",
			"/api/chaos/reports/../../etc/passwd",
		}
		for _, p := range paths {
			req, _ := http.NewRequest("GET", apiURL+p, nil)
			req.Header.Set("X-Internal-Token", token)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != 400 && resp.StatusCode != 404 {
				t.Fatalf("expected 400 or 404 for path traversal, got %d on %s", resp.StatusCode, p)
			}
		}
	})

	t.Run("TriggerScenario", func(t *testing.T) {
		requestDir := filepath.Join(resultsDir, "chaos-requests")
		os.MkdirAll(requestDir, 0o755)
		t.Cleanup(func() {
			files, _ := filepath.Glob(filepath.Join(requestDir, "chaos-worker-crash-*.json"))
			for _, f := range files {
				os.Remove(f)
			}
		})

		body := `{"scenario":"worker-crash","timeout_seconds":60}`
		req, _ := http.NewRequest("POST", apiURL+"/api/chaos/trigger", strings.NewReader(body))
		req.Header.Set("X-Internal-Token", token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 202 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 202, got %d: %s", resp.StatusCode, string(b))
		}
		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		if result["status"] != "queued" {
			t.Fatalf("expected status=queued, got %v", result["status"])
		}
		runID, ok := result["run_id"].(string)
		if !ok || !strings.HasPrefix(runID, "chaos-worker-crash-") {
			t.Fatalf("expected run_id starting with chaos-worker-crash-, got %v", result["run_id"])
		}

		reqFile := filepath.Join(requestDir, runID+".json")
		if _, err := os.Stat(reqFile); os.IsNotExist(err) {
			t.Fatal("request file not created")
		}
	})

	t.Run("TriggerInvalidScenario", func(t *testing.T) {
		body := `{"scenario":"drop-database"}`
		req, _ := http.NewRequest("POST", apiURL+"/api/chaos/trigger", strings.NewReader(body))
		req.Header.Set("X-Internal-Token", token)
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 400 {
			t.Fatalf("expected 400, got %d", resp.StatusCode)
		}
	})

	t.Run("StatusQueued", func(t *testing.T) {
		requestDir := filepath.Join(resultsDir, "chaos-requests")
		os.MkdirAll(requestDir, 0o755)
		testRunID := "chaos-status-test-queued"
		os.WriteFile(filepath.Join(requestDir, testRunID+".json"), []byte(`{}`), 0o644)
		t.Cleanup(func() { os.Remove(filepath.Join(requestDir, testRunID+".json")) })

		req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/chaos/status?run_id=%s", apiURL, testRunID), nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
		}
		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		if result["status"] != "queued" {
			t.Fatalf("expected status=queued, got %v", result["status"])
		}
	})

	t.Run("StatusCompleted", func(t *testing.T) {
		req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/chaos/status?run_id=chaos-suite-test-fixture", apiURL), nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			b, _ := io.ReadAll(resp.Body)
			t.Fatalf("expected 200, got %d: %s", resp.StatusCode, string(b))
		}
		var result map[string]any
		json.NewDecoder(resp.Body).Decode(&result)
		if result["status"] != "completed" {
			t.Fatalf("expected status=completed, got %v", result["status"])
		}
	})

	t.Run("StatusNotFound", func(t *testing.T) {
		req, _ := http.NewRequest("GET", apiURL+"/api/chaos/status?run_id=nonexistent-run", nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("expected 404, got %d", resp.StatusCode)
		}
	})

	t.Run("EmptyReportsList", func(t *testing.T) {
		os.Remove(fixturePath)
		defer os.WriteFile(fixturePath, fixtureData, 0o644)

		req, _ := http.NewRequest("GET", apiURL+"/api/chaos/reports", nil)
		req.Header.Set("X-Internal-Token", token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("expected 200, got %d", resp.StatusCode)
		}
	})
}
```

- [ ] **Step 3: Verify test compiles**

```bash
cd tests/integration && go build ./...
```

Expected: no errors (tests compile but need live services to run).

- [ ] **Step 4: Verify frontend compiles with sidebar change**

```bash
cd frontend && npm run build
```

Expected: no errors.

---

## Validation Checklist (after all tasks complete)

### Backend Validation

```bash
# Build
cd services/api && go build ./...

# List reports (may be empty or have existing reports from Phase 9B chaos runs)
curl -s http://localhost:8082/api/chaos/reports -H "X-Internal-Token: $TOKEN" | jq .

# Report detail (with existing report)
curl -s http://localhost:8082/api/chaos/reports/{report-id} -H "X-Internal-Token: $TOKEN" | jq .

# Path traversal blocked
curl -s http://localhost:8082/api/chaos/reports/../../etc/passwd -H "X-Internal-Token: $TOKEN"
# Expected: 400 Bad Request

# Trigger
curl -s -X POST http://localhost:8082/api/chaos/trigger \
  -H "X-Internal-Token: $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"scenario":"worker-crash","timeout_seconds":60}' | jq .
# Expected: 202 with run_id and status=queued

# Invalid trigger
curl -s -X POST http://localhost:8082/api/chaos/trigger \
  -H "X-Internal-Token: $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"scenario":"drop-database"}' | jq .
# Expected: 400 Bad Request

# Status (queued — run_id from trigger)
curl -s "http://localhost:8082/api/chaos/status?run_id={run_id}" -H "X-Internal-Token: $TOKEN" | jq .
# Expected: 200 with status=queued

# Status (not found)
curl -s "http://localhost:8082/api/chaos/status?run_id=nonexistent" -H "X-Internal-Token: $TOKEN" | jq .
# Expected: 404
```

### Frontend Validation

1. Open `http://localhost:3001/chaos`
2. Verify sidebar has "Chaos" link (last item)
3. If chaos reports exist from Phase 9B runs, verify they appear as cards
4. Click a report to see detail page with scenario breakdown
5. Test trigger form: select scenario, click "Run Chaos", see "Queued" status
6. Verify "Back to Chaos Dashboard" link works on detail page
7. Verify empty state message when no reports exist

### Integration Tests

```bash
make test-integration
# or specifically:
cd tests/integration && go test -v -run TestChaosEndpoints
```

Expected: all 10 subtests pass.

### Security Checklist

- [ ] All 4 chaos endpoints protected by `internalTokenMiddleware`
- [ ] Report ID validated with `^[a-z0-9-]+$` regex
- [ ] `filepath.Clean` used on all file paths (defense in depth)
- [ ] Scenario validated against whitelist (7 values only)
- [ ] Trigger does NOT execute chaos — only writes request file
- [ ] results mount changed to rw but handler only writes to `chaos-requests/` subdirectory
- [ ] OPTIONS handler registered for POST `/api/chaos/trigger` (CORS preflight)

---

## Final Sidebar Navigation (Phase 11 complete)

```
Dashboard        (existing — Phase 2)
Tasks            (existing — Phase 2)
Traces           (existing — Phase 10)
Alerts           (Phase 11 — Feature 1) ✅
DLQ              (Phase 11 — Feature 3) ✅
Chaos            (Phase 11 — Feature 4) ← this task
```

## Definition of Done — Chaos Dashboard

Per spec `phase-11-features-part2.md` section 4:

1. ✅ Endpoint `/api/chaos/reports` lists reports sorted by date
2. ✅ Endpoint `/api/chaos/reports/{id}` returns full report JSON
3. ✅ Endpoint `/api/chaos/trigger` creates request file and returns 202
4. ✅ Endpoint `/api/chaos/status` shows queued/completed/not_found
5. ✅ Path traversal blocked via report ID validation
6. ✅ Frontend lists reports with summary cards
7. ✅ Frontend shows report detail with scenario breakdown and SLO badges
8. ✅ Frontend trigger form allows selecting scenario and firing
9. ✅ Frontend poll shows trigger status in real time
10. ✅ Integration tests pass (with fixture JSON)
11. ✅ Sidebar updated with Chaos navigation

## Definition of Done — Phase 11 (all features)

| Feature | Backend | Frontend | Tests | Sidebar | Status |
|---------|---------|----------|-------|---------|--------|
| Alert Center | 3 endpoints, migration 000006 | Alerts page with filters/pagination/ack | 7 scenarios | ✅ Alerts | ✅ Done |
| Replay Task | 1 endpoint, migration 000007, InsertTask refactor | Replay button + modal | 5 scenarios | — (button in existing pages) | ✅ Done |
| DLQ Explorer | 5 endpoints, migration 000008 | DLQ page with cards/actions | 4 scenarios | ✅ DLQ | ✅ Done |
| Chaos Dashboard | 4 endpoints, filesystem I/O | List + detail + trigger | 10 scenarios | ✅ Chaos | Pending |
