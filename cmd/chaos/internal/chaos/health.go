package chaos

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/runtime-platform/cmd/chaos/internal/chaos/checker"
	"github.com/runtime-platform/cmd/chaos/internal/chaos/docker"
)

type PreflightResult struct {
	Passed bool
	Errors []string
}

func Preflight(ctx context.Context, chk *checker.Checker, requiredContainers []string) PreflightResult {
	result := PreflightResult{Passed: true}

	slog.Info("preflight: checking container health")
	for _, name := range requiredContainers {
		status, err := chk.ContainerHealth(ctx, name)
		if err != nil {
			result.addError(fmt.Sprintf("container %s: %v", name, err))
			continue
		}
		if status != docker.HealthHealthy {
			result.addError(fmt.Sprintf("container %s is %s (expected healthy)", name, status))
		}
	}

	slog.Info("preflight: checking active healing events")
	var events []checker.HealingEvent
	var healErr error
	for attempt := 0; attempt < 3; attempt++ {
		events, healErr = chk.ActiveHealingEvents(ctx)
		if healErr != nil {
			result.addError(fmt.Sprintf("healing events query: %v", healErr))
			break
		}
		if len(events) == 0 {
			break
		}
		slog.Info("preflight: active healing events found, waiting for resolution", "count", len(events), "attempt", attempt+1)
		time.Sleep(5 * time.Second)
	}
	if healErr == nil && len(events) > 0 {
		result.addError(fmt.Sprintf("%d active healing events — resolve before running chaos", len(events)))
	}

	slog.Info("preflight: checking queue depth")
	depth, err := chk.QueueDepth(ctx)
	if err != nil {
		result.addError(fmt.Sprintf("queue depth: %v", err))
	} else if depth > 0 {
		result.addError(fmt.Sprintf("queue depth is %d (must be 0)", depth))
	}

	slog.Info("preflight: checking DLQ depth")
	dlqDepth, err := chk.DLQDepth(ctx)
	if err != nil {
		result.addError(fmt.Sprintf("dlq depth: %v", err))
	} else if dlqDepth > 0 {
		result.addError(fmt.Sprintf("DLQ depth is %d (must be 0)", dlqDepth))
	}

	return result
}

func (r *PreflightResult) addError(msg string) {
	r.Passed = false
	r.Errors = append(r.Errors, msg)
	slog.Error("preflight FAIL", "reason", msg)
}
