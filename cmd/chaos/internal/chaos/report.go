package chaos

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type SLOStatus string

const (
	SLOPass SLOStatus = "PASS"
	SLOWarn SLOStatus = "WARN"
	SLOFail SLOStatus = "FAIL"
)

type SLOType string

const (
	SLOFunctional SLOType = "functional"
	SLOTiming     SLOType = "timing"
)

type SLOResult struct {
	Name     string    `json:"name"`
	Type     SLOType   `json:"type"`
	Expected any       `json:"expected"`
	Actual   any       `json:"actual"`
	Status   SLOStatus `json:"status"`
}

type StageResult struct {
	DurationMs int64  `json:"duration_ms"`
	Status     string `json:"status"`
}

type ScenarioReport struct {
	Name            string                  `json:"name"`
	Status          SLOStatus               `json:"status"`
	DurationSeconds float64                 `json:"duration_seconds"`
	Stages          map[string]*StageResult `json:"stages"`
	Metrics         map[string]any          `json:"metrics"`
	SLOResults      []SLOResult             `json:"slo_results"`
	Warnings        []string                `json:"warnings"`
	Error           string                  `json:"error,omitempty"`
}

func NewScenarioReport(name string) *ScenarioReport {
	return &ScenarioReport{
		Name:   name,
		Status: SLOPass,
		Stages: map[string]*StageResult{
			"setup":    {},
			"inject":   {},
			"observe":  {},
			"validate": {},
			"cleanup":  {},
		},
		Metrics:  make(map[string]any),
		Warnings: []string{},
	}
}

func (r *ScenarioReport) ComputeStatus() {
	for _, slo := range r.SLOResults {
		if slo.Type == SLOFunctional && slo.Status == SLOFail {
			r.Status = SLOFail
			return
		}
	}
	for _, slo := range r.SLOResults {
		if slo.Status == SLOFail {
			r.Status = SLOWarn
			return
		}
	}
	r.Status = SLOPass
}

type SuiteReport struct {
	Version         string            `json:"version"`
	RunID           string            `json:"run_id"`
	GitCommit       string            `json:"git_commit"`
	Environment     string            `json:"environment"`
	Timestamp       time.Time         `json:"timestamp"`
	DurationSeconds float64           `json:"duration_seconds"`
	Summary         SuiteSummary      `json:"summary"`
	Scenarios       []*ScenarioReport `json:"scenarios"`
}

type SuiteSummary struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Warned int `json:"warned"`
	Failed int `json:"failed"`
}

func (s *SuiteReport) ComputeSummary() {
	s.Summary = SuiteSummary{Total: len(s.Scenarios)}
	for _, sc := range s.Scenarios {
		switch sc.Status {
		case SLOPass:
			s.Summary.Passed++
		case SLOWarn:
			s.Summary.Warned++
		case SLOFail:
			s.Summary.Failed++
		}
	}
}

func WriteReport(outputDir string, filename string, v any) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output dir: %w", err)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal report: %w", err)
	}
	path := filepath.Join(outputDir, filename)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}
