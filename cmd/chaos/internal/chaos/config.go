package chaos

import (
	"flag"
	"fmt"
	"os"
	"time"
)

type Config struct {
	APIURL        string
	AIRuntimeURL  string
	WorkerURL     string
	DatabaseURL   string
	SQSEndpoint   string
	SQSQueueURL   string
	SQSDlqURL     string
	InternalToken string
	OutputDir     string
	GlobalTimeout time.Duration
	PollInterval  time.Duration
	Scenario      string
	List          bool
	RunID         string
}

func (c Config) ScenarioFilter() []string {
	if c.Scenario != "" {
		return []string{c.Scenario}
	}
	return nil
}

func ParseConfig() Config {
	cfg := Config{}

	flag.StringVar(&cfg.APIURL, "api-url", envOrDefault("API_URL", "http://localhost:8082"), "API endpoint")
	flag.StringVar(&cfg.AIRuntimeURL, "ai-runtime-url", envOrDefault("AI_RUNTIME_URL", "http://localhost:8001"), "AI Runtime endpoint")
	flag.StringVar(&cfg.WorkerURL, "worker-url", envOrDefault("WORKER_URL", "http://localhost:9091"), "Worker health/metrics endpoint")
	flag.StringVar(&cfg.DatabaseURL, "db-url", envOrDefault("DATABASE_URL", "postgres://traceruntime:traceruntime@localhost:5432/traceruntime?sslmode=disable"), "PostgreSQL connection")
	flag.StringVar(&cfg.SQSEndpoint, "sqs-endpoint", envOrDefault("SQS_ENDPOINT", "http://localhost:4566"), "LocalStack SQS endpoint")
	flag.StringVar(&cfg.SQSQueueURL, "sqs-queue-url", envOrDefault("SQS_QUEUE_URL", "http://localhost:4566/000000000000/traceruntime-tasks"), "Main queue URL")
	flag.StringVar(&cfg.SQSDlqURL, "sqs-dlq-url", envOrDefault("SQS_DLQ_URL", "http://localhost:4566/000000000000/traceruntime-tasks-dlq"), "DLQ URL")
	flag.StringVar(&cfg.InternalToken, "internal-token", envOrDefault("CHAOS_INTERNAL_TOKEN", ""), "Token for /internal/chaos/* endpoints")
	flag.StringVar(&cfg.OutputDir, "output-dir", envOrDefault("CHAOS_OUTPUT_DIR", "results"), "Report output directory")
	flag.DurationVar(&cfg.GlobalTimeout, "timeout", parseDurationOrDefault("CHAOS_TIMEOUT", 45*time.Minute), "Global suite timeout")
	flag.DurationVar(&cfg.PollInterval, "poll-interval", parseDurationOrDefault("CHAOS_POLL_INTERVAL", 2*time.Second), "Default polling interval")
	flag.StringVar(&cfg.Scenario, "scenario", "", "Run a single scenario by name")
	flag.BoolVar(&cfg.List, "list", false, "List available scenarios and exit")
	flag.StringVar(&cfg.RunID, "run-id", "", "Run ID from trigger (correlates report with request)")

	flag.Parse()
	return cfg
}

func (c Config) Validate() error {
	if c.DatabaseURL == "" {
		return fmt.Errorf("DATABASE_URL is required")
	}
	if c.SQSQueueURL == "" {
		return fmt.Errorf("SQS_QUEUE_URL is required")
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func parseDurationOrDefault(envKey string, fallback time.Duration) time.Duration {
	if v := os.Getenv(envKey); v != "" {
		d, err := time.ParseDuration(v)
		if err == nil {
			return d
		}
	}
	return fallback
}
