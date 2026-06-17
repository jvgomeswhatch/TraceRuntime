package config

import (
	"log/slog"
	"os"
	"strconv"
)

type Config struct {
	DatabaseURL                string
	WorkerHealthURL            string
	APIEventsURL               string
	InternalToken              string
	SQSQueueURL                string
	SQSDlqURL                  string
	PollIntervalSeconds        int
	HeartbeatStaleSeconds      int
	HealthcheckIntervalSeconds int
	HealthcheckFailures        int
	QueueLagThreshold          int
	TaskStuckSeconds           int
	DLQDeltaThreshold          int
}

func Load() Config {
	cfg := Config{
		DatabaseURL:                mustEnv("DATABASE_URL"),
		WorkerHealthURL:            mustEnv("WORKER_HEALTH_URL"),
		APIEventsURL:               mustEnv("API_EVENTS_URL"),
		InternalToken:              os.Getenv("INTERNAL_TOKEN"),
		SQSQueueURL:                mustEnv("SQS_QUEUE_URL"),
		SQSDlqURL:                  mustEnv("SQS_DLQ_URL"),
		PollIntervalSeconds:        envInt("WATCHDOG_POLL_INTERVAL_SECONDS", 15),
		HeartbeatStaleSeconds:      envInt("WATCHDOG_HEARTBEAT_STALE_SECONDS", 60),
		HealthcheckIntervalSeconds: envInt("WATCHDOG_HEALTHCHECK_INTERVAL_SECONDS", 15),
		HealthcheckFailures:        envInt("WATCHDOG_HEALTHCHECK_FAILURES", 3),
		QueueLagThreshold:          envInt("WATCHDOG_QUEUE_LAG_THRESHOLD", 50),
		TaskStuckSeconds:           envInt("WATCHDOG_TASK_STUCK_SECONDS", 300),
		DLQDeltaThreshold:          envInt("WATCHDOG_DLQ_DELTA_THRESHOLD", 1),
	}

	valid := true
	if cfg.PollIntervalSeconds < 5 {
		slog.Error("config validation failed: PollIntervalSeconds must be >= 5", "value", cfg.PollIntervalSeconds)
		valid = false
	}
	if cfg.HeartbeatStaleSeconds < 10 {
		slog.Error("config validation failed: HeartbeatStaleSeconds must be >= 10", "value", cfg.HeartbeatStaleSeconds)
		valid = false
	}
	if cfg.HealthcheckIntervalSeconds < 5 {
		slog.Error("config validation failed: HealthcheckIntervalSeconds must be >= 5", "value", cfg.HealthcheckIntervalSeconds)
		valid = false
	}
	if cfg.HealthcheckFailures < 1 {
		slog.Error("config validation failed: HealthcheckFailures must be >= 1", "value", cfg.HealthcheckFailures)
		valid = false
	}
	if cfg.QueueLagThreshold < 1 {
		slog.Error("config validation failed: QueueLagThreshold must be >= 1", "value", cfg.QueueLagThreshold)
		valid = false
	}
	if cfg.TaskStuckSeconds < 30 {
		slog.Error("config validation failed: TaskStuckSeconds must be >= 30", "value", cfg.TaskStuckSeconds)
		valid = false
	}
	if cfg.DLQDeltaThreshold < 1 {
		slog.Error("config validation failed: DLQDeltaThreshold must be >= 1", "value", cfg.DLQDeltaThreshold)
		valid = false
	}
	if !valid {
		os.Exit(1)
	}

	return cfg
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("missing required env var", "name", key)
		os.Exit(1)
	}
	return v
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
