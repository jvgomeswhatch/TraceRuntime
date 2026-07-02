package chaos

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"
)

type Stage string

const (
	StageSetup    Stage = "setup"
	StageInject   Stage = "inject"
	StageObserve  Stage = "observe"
	StageValidate Stage = "validate"
	StageCleanup  Stage = "cleanup"
)

type Runner struct {
	cfg       Config
	scenarios []Scenario
}

func NewRunner(cfg Config, scenarios []Scenario) *Runner {
	return &Runner{cfg: cfg, scenarios: scenarios}
}

func (r *Runner) Run(ctx context.Context, sc *ScenarioContext, chaosDB *ChaosRunDB) (*SuiteReport, error) {
	runID := sc.RunID

	suite := &SuiteReport{
		Version:     "1.0",
		RunID:       runID,
		GitCommit:   GitCommit(),
		Environment: "local-docker",
		Timestamp:   time.Now(),
	}

	suiteStart := time.Now()

	filter := r.cfg.ScenarioFilter()
	scenarios := r.filterScenarios(filter)

	if len(scenarios) == 0 {
		return nil, fmt.Errorf("no scenarios to run (filter: %v, available: %v)", filter, r.scenarioNames())
	}

	if err := chaosDB.SetRunning(ctx, sc.ChaosRunDBID); err != nil {
		slog.Warn("failed to set chaos run to running", "error", err)
	}

	slog.Info("chaos suite starting", "scenarios", len(scenarios), "timeout", r.cfg.GlobalTimeout)

	globalCtx, globalCancel := context.WithTimeout(ctx, r.cfg.GlobalTimeout)
	defer globalCancel()

	for _, scenario := range scenarios {
		if globalCtx.Err() != nil {
			slog.Error("global timeout reached, aborting remaining scenarios")
			break
		}

		report := r.runScenario(globalCtx, scenario, sc)
		suite.Scenarios = append(suite.Scenarios, report)

		slog.Info("scenario complete",
			"name", report.Name,
			"status", report.Status,
			"duration_s", fmt.Sprintf("%.1f", report.DurationSeconds),
		)
	}

	suite.DurationSeconds = time.Since(suiteStart).Seconds()
	suite.ComputeSummary()

	if err := chaosDB.SetCompleted(ctx, sc.ChaosRunDBID,
		suite.Summary.Passed, suite.Summary.Warned, suite.Summary.Failed,
		suite.Summary.Total, suite.DurationSeconds); err != nil {
		slog.Warn("failed to update chaos run completion", "error", err)
	}

	filename := fmt.Sprintf("chaos-suite-%s.json", runID)
	if err := WriteReport(r.cfg.OutputDir, filename, suite); err != nil {
		slog.Error("failed to write suite report", "error", err)
	}

	return suite, nil
}

func (r *Runner) runScenario(ctx context.Context, s Scenario, sc *ScenarioContext) *ScenarioReport {
	report := NewScenarioReport(s.Name())
	scenarioStart := time.Now()

	scenarioCtx, cancel := context.WithTimeout(ctx, s.Timeout())
	defer cancel()

	sc.ScenarioName = s.Name()
	sc.StartTime = scenarioStart
	sc.Timeout = s.Timeout()
	sc.PollInterval = r.cfg.PollInterval

	stages := []struct {
		name Stage
		fn   func() error
	}{
		{StageSetup, func() error { return s.Setup(scenarioCtx, sc) }},
		{StageInject, func() error { return s.Inject(scenarioCtx, sc) }},
	}

	var observed *ObserveResult
	aborted := false

	for _, stage := range stages {
		if aborted {
			break
		}
		start := time.Now()
		err := stage.fn()
		report.Stages[string(stage.name)] = &StageResult{
			DurationMs: time.Since(start).Milliseconds(),
			Status:     stageStatus(err),
		}
		if err != nil {
			report.Error = fmt.Sprintf("%s: %v", stage.name, err)
			report.Status = SLOFail
			aborted = true
		}
	}

	if !aborted {
		start := time.Now()
		var err error
		observed, err = s.Observe(scenarioCtx, sc)
		report.Stages[string(StageObserve)] = &StageResult{
			DurationMs: time.Since(start).Milliseconds(),
			Status:     stageStatus(err),
		}
		if err != nil {
			report.Error = fmt.Sprintf("observe: %v", err)
			report.Status = SLOFail
			aborted = true
		}
	}

	if !aborted && observed != nil {
		start := time.Now()
		validated, err := s.Validate(scenarioCtx, sc, observed)
		report.Stages[string(StageValidate)] = &StageResult{
			DurationMs: time.Since(start).Milliseconds(),
			Status:     stageStatus(err),
		}
		if err != nil {
			report.Error = fmt.Sprintf("validate: %v", err)
			report.Status = SLOFail
		} else if validated != nil {
			report.SLOResults = validated.SLOResults
			report.Metrics = validated.Metrics
			report.Warnings = validated.Warnings
			report.ComputeStatus()
		}
	}

	// Cleanup ALWAYS runs
	start := time.Now()
	cleanupErr := s.Cleanup(scenarioCtx, sc)
	report.Stages[string(StageCleanup)] = &StageResult{
		DurationMs: time.Since(start).Milliseconds(),
		Status:     stageStatus(cleanupErr),
	}
	if cleanupErr != nil {
		slog.Error("cleanup failed", "scenario", s.Name(), "error", cleanupErr)
		report.Warnings = append(report.Warnings, fmt.Sprintf("cleanup error: %v", cleanupErr))
	}

	report.DurationSeconds = time.Since(scenarioStart).Seconds()

	ts := time.Now().Format("20060102-150405")
	if err := WriteReport(r.cfg.OutputDir, fmt.Sprintf("chaos-%s-%s.json", s.Name(), ts), report); err != nil {
		slog.Error("failed to write scenario report", "error", err)
	}

	return report
}

func (r *Runner) filterScenarios(filter []string) []Scenario {
	if len(filter) == 0 {
		return r.scenarios
	}
	filterSet := make(map[string]bool)
	for _, f := range filter {
		filterSet[strings.TrimSpace(f)] = true
	}
	var result []Scenario
	for _, s := range r.scenarios {
		if filterSet[s.Name()] {
			result = append(result, s)
		}
	}
	return result
}

func (r *Runner) scenarioNames() []string {
	names := make([]string, len(r.scenarios))
	for i, s := range r.scenarios {
		names[i] = s.Name()
	}
	return names
}

func stageStatus(err error) string {
	if err != nil {
		return "error"
	}
	return "ok"
}

func GitCommit() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
