package chaos

import (
	"context"
	"time"

	"github.com/runtime-platform/cmd/chaos/internal/chaos/checker"
	"github.com/runtime-platform/cmd/chaos/internal/chaos/docker"
)

type ScenarioContext struct {
	RunID         string
	ChaosRunDBID  string
	ScenarioName  string
	StartTime     time.Time
	Config        Config
	Timeout       time.Duration
	PollInterval  time.Duration
	Checker       *checker.Checker
	Docker        *docker.Controller
}

type ObserveResult struct {
	Metrics map[string]any
}

func NewObserveResult() *ObserveResult {
	return &ObserveResult{Metrics: make(map[string]any)}
}

type Scenario interface {
	Name() string
	Timeout() time.Duration
	Setup(ctx context.Context, sc *ScenarioContext) error
	Inject(ctx context.Context, sc *ScenarioContext) error
	Observe(ctx context.Context, sc *ScenarioContext) (*ObserveResult, error)
	Validate(ctx context.Context, sc *ScenarioContext, observed *ObserveResult) (*ScenarioReport, error)
	Cleanup(ctx context.Context, sc *ScenarioContext) error
}
