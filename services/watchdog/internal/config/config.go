package config

import (
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
	return Config{
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
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		panic("required env var not set: " + key)
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
