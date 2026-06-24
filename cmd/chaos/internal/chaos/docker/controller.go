package docker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
)

type HealthStatus string

const (
	HealthHealthy   HealthStatus = "healthy"
	HealthUnhealthy HealthStatus = "unhealthy"
	HealthStarting  HealthStatus = "starting"
	HealthNone      HealthStatus = "none"
	HealthExited    HealthStatus = "exited"
)

type Controller struct {
	client  *client.Client
	project string
	retry   RetryPolicy
}

type RetryPolicy struct {
	MaxAttempts int
	Backoff     time.Duration
}

func NewController(project string, retry RetryPolicy) (*Controller, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &Controller{client: cli, project: project, retry: retry}, nil
}

func (c *Controller) Close() error {
	return c.client.Close()
}

func (c *Controller) resolveContainerName(ctx context.Context, service string) string {
	primary := c.project + "-" + service + "-1"
	_, err := c.client.ContainerInspect(ctx, primary)
	if err == nil {
		return primary
	}
	fallback := c.project + "-" + service
	_, err = c.client.ContainerInspect(ctx, fallback)
	if err == nil {
		return fallback
	}
	return primary
}

func (c *Controller) Kill(ctx context.Context, service string, signal string) error {
	name := c.resolveContainerName(ctx, service)
	slog.Info("docker.kill", "container", name, "signal", signal)
	return c.withRetry(ctx, func() error {
		return c.client.ContainerKill(ctx, name, signal)
	})
}

func (c *Controller) Stop(ctx context.Context, service string) error {
	name := c.resolveContainerName(ctx, service)
	slog.Info("docker.stop", "container", name)
	timeout := 10
	return c.withRetry(ctx, func() error {
		return c.client.ContainerStop(ctx, name, container.StopOptions{Timeout: &timeout})
	})
}

func (c *Controller) Start(ctx context.Context, service string) error {
	name := c.resolveContainerName(ctx, service)
	slog.Info("docker.start", "container", name)
	return c.withRetry(ctx, func() error {
		return c.client.ContainerStart(ctx, name, container.StartOptions{})
	})
}

func (c *Controller) Pause(ctx context.Context, service string) error {
	name := c.resolveContainerName(ctx, service)
	slog.Info("docker.pause", "container", name)
	return c.withRetry(ctx, func() error {
		return c.client.ContainerPause(ctx, name)
	})
}

func (c *Controller) Unpause(ctx context.Context, service string) error {
	name := c.resolveContainerName(ctx, service)
	slog.Info("docker.unpause", "container", name)
	return c.withRetry(ctx, func() error {
		return c.client.ContainerUnpause(ctx, name)
	})
}

func (c *Controller) Health(ctx context.Context, service string) (HealthStatus, error) {
	name := c.resolveContainerName(ctx, service)
	info, err := c.client.ContainerInspect(ctx, name)
	if err != nil {
		if errdefs.IsNotFound(err) {
			return HealthExited, nil
		}
		return "", fmt.Errorf("inspect %s: %w", name, err)
	}

	if !info.State.Running {
		return HealthExited, nil
	}

	if info.State.Health == nil {
		return HealthNone, nil
	}

	switch info.State.Health.Status {
	case "healthy":
		return HealthHealthy, nil
	case "unhealthy":
		return HealthUnhealthy, nil
	case "starting":
		return HealthStarting, nil
	default:
		return HealthNone, nil
	}
}

func (c *Controller) WaitForHealthy(ctx context.Context, service string, timeout time.Duration) error {
	name := c.resolveContainerName(ctx, service)
	deadline := time.After(timeout)
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline:
			return fmt.Errorf("timeout waiting for %s to become healthy", name)
		case <-ticker.C:
			status, err := c.Health(ctx, service)
			if err != nil {
				slog.Warn("health check error", "container", name, "error", err)
				continue
			}
			if status == HealthHealthy {
				slog.Info("container healthy", "container", name)
				return nil
			}
		}
	}
}

func (c *Controller) RestartCount(ctx context.Context, service string) (int, error) {
	name := c.resolveContainerName(ctx, service)
	info, err := c.client.ContainerInspect(ctx, name)
	if err != nil {
		return 0, fmt.Errorf("inspect %s: %w", name, err)
	}
	return info.RestartCount, nil
}

func (c *Controller) DisableRestart(ctx context.Context, service string) error {
	name := c.resolveContainerName(ctx, service)
	slog.Info("docker.disable_restart", "container", name)
	_, err := c.client.ContainerUpdate(ctx, name, container.UpdateConfig{
		RestartPolicy: container.RestartPolicy{Name: container.RestartPolicyDisabled},
	})
	if err != nil {
		return fmt.Errorf("disable restart %s: %w", name, err)
	}
	return nil
}

func (c *Controller) EnableRestart(ctx context.Context, service string) error {
	name := c.resolveContainerName(ctx, service)
	slog.Info("docker.enable_restart", "container", name)
	_, err := c.client.ContainerUpdate(ctx, name, container.UpdateConfig{
		RestartPolicy: container.RestartPolicy{Name: "unless-stopped"},
	})
	if err != nil {
		return fmt.Errorf("enable restart %s: %w", name, err)
	}
	return nil
}

func (c *Controller) withRetry(ctx context.Context, fn func() error) error {
	var lastErr error
	for i := 0; i < c.retry.MaxAttempts; i++ {
		if err := fn(); err != nil {
			lastErr = err
			slog.Warn("docker retry", "attempt", i+1, "error", err)
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.retry.Backoff):
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("after %d retries: %w", c.retry.MaxAttempts, lastErr)
}
