package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"time"

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

// Trigger + Status endpoints (Task 20)

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

	command := "make chaos"
	if req.Scenario != "all" {
		command = fmt.Sprintf("make chaos SCENARIO=%s", req.Scenario)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusAccepted)
	json.NewEncoder(w).Encode(map[string]any{
		"status":   "queued",
		"run_id":   runID,
		"scenario": req.Scenario,
		"command":  command,
		"poll_url": "/api/chaos/status?run_id=" + runID,
	})
}

func (h *ChaosHandler) Cancel(w http.ResponseWriter, r *http.Request) {
	runID := chi.URLParam(r, "runId")
	if !isValidReportID(runID) {
		jsonError(w, "invalid run_id", http.StatusBadRequest)
		return
	}

	reqPath := filepath.Clean(filepath.Join(h.requestDir, runID+".json"))
	if _, err := os.Stat(reqPath); os.IsNotExist(err) {
		jsonError(w, "request not found", http.StatusNotFound)
		return
	}

	if err := os.Remove(reqPath); err != nil {
		jsonError(w, "failed to cancel", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"run_id": runID,
		"status": "cancelled",
	})
}

func (h *ChaosHandler) Status(w http.ResponseWriter, r *http.Request) {
	runID := r.URL.Query().Get("run_id")

	if runID == "" {
		h.statusGlobal(w)
		return
	}

	if !isValidReportID(runID) {
		jsonError(w, "invalid run_id", http.StatusBadRequest)
		return
	}

	h.statusByRunID(w, runID)
}

// statusGlobal scans all request files and returns the most recent pending one.
func (h *ChaosHandler) statusGlobal(w http.ResponseWriter) {
	pattern := filepath.Join(h.requestDir, "chaos-*.json")
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

	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var req chaosRequest
		if err := json.Unmarshal(data, &req); err != nil || req.RunID == "" {
			continue
		}

		if req.QueuedAt != "" {
			if queuedAt, err := time.Parse(time.RFC3339, req.QueuedAt); err == nil {
				if time.Since(queuedAt) > 1*time.Hour {
					continue
				}
			}
		}

		status := h.resolveRunStatus(req.RunID)
		if status.state == "completed" || status.state == "failed" {
			continue
		}

		command := "make chaos"
		if req.Scenario != "all" {
			command = fmt.Sprintf("make chaos SCENARIO=%s", req.Scenario)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"run_id":    req.RunID,
			"status":    status.state,
			"scenario":  req.Scenario,
			"command":   command,
			"queued_at": req.QueuedAt,
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status": "idle",
	})
}

type runStatus struct {
	state    string // idle, queued, running, completed, failed
	summary  *chaosReportSummary
	reportID string
}

// resolveRunStatus determines the lifecycle state for a given run_id by
// checking report files (correlated by run_id in JSON) and request files.
func (h *ChaosHandler) resolveRunStatus(runID string) runStatus {
	// 1. Look for a report whose run_id matches (direct correlation).
	reportFile := filepath.Clean(filepath.Join(h.resultsDir, "chaos-suite-"+runID+".json"))
	if data, err := os.ReadFile(reportFile); err == nil {
		var raw struct {
			RunID   string             `json:"run_id"`
			Summary chaosReportSummary `json:"summary"`
		}
		if json.Unmarshal(data, &raw) == nil {
			reportID := "chaos-suite-" + runID
			state := "completed"
			if raw.Summary.Failed > 0 {
				state = "failed"
			}
			return runStatus{state: state, summary: &raw.Summary, reportID: reportID}
		}
	}

	// 2. Scan all suite reports for matching run_id (backwards compat with
	//    reports generated before the --run-id flag was added).
	pattern := filepath.Join(h.resultsDir, "chaos-suite-*.json")
	suiteFiles, _ := filepath.Glob(pattern)
	for _, f := range suiteFiles {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		var raw struct {
			RunID   string             `json:"run_id"`
			Summary chaosReportSummary `json:"summary"`
		}
		if json.Unmarshal(data, &raw) != nil || raw.RunID != runID {
			continue
		}
		base := filepath.Base(f)
		reportID := base[:len(base)-len(".json")]
		state := "completed"
		if raw.Summary.Failed > 0 {
			state = "failed"
		}
		return runStatus{state: state, summary: &raw.Summary, reportID: reportID}
	}

	// 3. No report found — check if the request file exists.
	reqPath := filepath.Clean(filepath.Join(h.requestDir, runID+".json"))
	if _, err := os.Stat(reqPath); err == nil {
		return runStatus{state: "queued"}
	}

	return runStatus{state: "idle"}
}

func (h *ChaosHandler) statusByRunID(w http.ResponseWriter, runID string) {
	reqPath := filepath.Clean(filepath.Join(h.requestDir, runID+".json"))
	reqData, _ := os.ReadFile(reqPath)
	var req chaosRequest
	json.Unmarshal(reqData, &req)

	status := h.resolveRunStatus(runID)

	switch status.state {
	case "completed", "failed":
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"run_id":     runID,
			"status":     status.state,
			"scenario":   req.Scenario,
			"summary":    status.summary,
			"report_url": "/api/chaos/reports/" + status.reportID,
		})

	case "queued":
		command := "make chaos"
		if req.Scenario != "" && req.Scenario != "all" {
			command = fmt.Sprintf("make chaos SCENARIO=%s", req.Scenario)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"run_id":    runID,
			"status":    "queued",
			"scenario":  req.Scenario,
			"command":   command,
			"queued_at": req.QueuedAt,
		})

	default:
		jsonError(w, "run not found", http.StatusNotFound)
	}
}
