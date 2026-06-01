# Phase 6+7 — Distributed Queue & IaC Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the in-process Go channel queue with SQS on LocalStack, move the worker to a separate container, preserve W3C trace propagation end-to-end, and provision all AWS resources via Terraform.

**Architecture:** The API publishes tasks to SQS via AWS SDK when `QUEUE_BACKEND=sqs`. A separate `services/worker` binary polls SQS with long polling, processes tasks, writes AI outputs to S3, and notifies the frontend via the API's `/internal/events` endpoint. Terraform provisions the same SQS+DLQ+S3 resources already created by the bootstrap script.

**Tech Stack:** Go 1.22+, AWS SDK for Go v2, LocalStack (SQS + S3), Docker Compose profiles, awslocal CLI, Terraform + LocalStack provider, Prometheus, OpenTelemetry.

**VALIDATION RULE:** After each task, STOP and wait for explicit user confirmation before proceeding to the next task. Never accumulate multiple tasks without validation. Never commit without user confirmation.

---

## File Map

### New files — `services/worker/`
```
services/worker/
  cmd/worker/main.go              bootstrap, env, signal handling, graceful shutdown
  internal/processor/processor.go task processing logic (extracted from api worker)
  internal/metrics/metrics.go     Prometheus counters/gauges for worker
  internal/telemetry/otel.go      OTEL init (identical pattern to api)
  go.mod                          module github.com/runtime-platform/services/worker
  go.sum
  Dockerfile
  .dockerignore
```

### New files — `services/api/`
```
services/api/internal/http/events.go   POST /internal/events handler (worker→SSE bridge)
```

### Modified files — `services/api/`
```
services/api/internal/http/router.go   add /internal/events route
services/api/internal/http/task.go     publish to SQS when QUEUE_BACKEND=sqs
services/api/cmd/server/main.go        remove worker goroutine, add SQS publisher init
services/api/go.mod                    add aws-sdk-go-v2 deps
```

### Modified files — `services/api/` (deleted packages)
```
services/api/internal/worker/   DELETED (moved to services/worker)
services/api/internal/queue/    DELETED (replaced by SQS publish in task.go)
```

### New infrastructure files
```
scripts/bootstrap.sh             awslocal commands: create SQS, DLQ, S3
docker-compose.yml               add localstack + worker services with profiles
infra/observability/prometheus/prometheus.yaml  add worker scrape target
infra/terraform/main.tf          SQS, DLQ, S3 resources
infra/terraform/variables.tf     configurable parameters
infra/terraform/outputs.tf       queue URLs and bucket name
infra/terraform/providers.tf     AWS provider pointing to LocalStack
Makefile                         infra-bootstrap, infra-apply, infra-destroy targets
```

---

## Task 1: Add LocalStack to docker-compose with profiles

**Files:**
- Modify: `docker-compose.yml`

**Context:** The current `docker-compose.yml` has api, ai-runtime, and frontend with no profiles. We need to add LocalStack and a worker service later. We also need compose profiles so heavy services can be excluded during development.

- [ ] **Step 1: Update docker-compose.yml**

Replace the entire `docker-compose.yml` with this content (preserves all existing services, adds LocalStack and profiles):

```yaml
version: "3.9"

networks:
  traceruntime:
    driver: bridge

services:
  localstack:
    image: localstack/localstack:3.4
    ports:
      - "4566:4566"
    environment:
      - SERVICES=sqs,s3
      - DEFAULT_REGION=us-east-1
      - LOCALSTACK_HOST=localstack
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    networks: [traceruntime]
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:4566/_localstack/health"]
      interval: 10s
      timeout: 5s
      retries: 10
      start_period: 20s
    restart: unless-stopped
    init: true
    mem_limit: 512m
    cpus: "0.5"
    profiles: [core, full]

  api:
    build:
      context: ./services/api
      dockerfile: Dockerfile
    ports:
      - "8082:8082"
    environment:
      - OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317
      - AI_RUNTIME_URL=http://ai-runtime:8000
      - QUEUE_BACKEND=sqs
      - AWS_ENDPOINT_URL=http://localstack:4566
      - AWS_ACCESS_KEY_ID=test
      - AWS_SECRET_ACCESS_KEY=test
      - AWS_DEFAULT_REGION=us-east-1
      - SQS_QUEUE_URL=http://localstack:4566/000000000000/traceruntime-tasks
    networks: [traceruntime]
    depends_on:
      localstack:
        condition: service_healthy
      otel-collector:
        condition: service_started
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8082/health"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 20s
    restart: unless-stopped
    init: true
    stop_grace_period: 10s
    mem_limit: 256m
    cpus: "0.5"
    profiles: [core, full]

  worker:
    build:
      context: ./services/worker
      dockerfile: Dockerfile
    environment:
      - OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317
      - AWS_ENDPOINT_URL=http://localstack:4566
      - AWS_ACCESS_KEY_ID=test
      - AWS_SECRET_ACCESS_KEY=test
      - AWS_DEFAULT_REGION=us-east-1
      - SQS_QUEUE_URL=http://localstack:4566/000000000000/traceruntime-tasks
      - SQS_DLQ_URL=http://localstack:4566/000000000000/traceruntime-tasks-dlq
      - S3_BUCKET=traceruntime-outputs
      - API_INTERNAL_URL=http://api:8082
      - AI_RUNTIME_URL=http://ai-runtime:8000
      - AI_RUNTIME_ENABLED=true
    ports:
      - "9091:9091"
    networks: [traceruntime]
    depends_on:
      localstack:
        condition: service_healthy
      api:
        condition: service_healthy
      otel-collector:
        condition: service_started
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:9091/health"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 20s
    restart: unless-stopped
    init: true
    stop_grace_period: 15s
    mem_limit: 128m
    cpus: "0.5"
    profiles: [core, full]

  worker-no-ai:
    build:
      context: ./services/worker
      dockerfile: Dockerfile
    environment:
      - OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317
      - AWS_ENDPOINT_URL=http://localstack:4566
      - AWS_ACCESS_KEY_ID=test
      - AWS_SECRET_ACCESS_KEY=test
      - AWS_DEFAULT_REGION=us-east-1
      - SQS_QUEUE_URL=http://localstack:4566/000000000000/traceruntime-tasks
      - SQS_DLQ_URL=http://localstack:4566/000000000000/traceruntime-tasks-dlq
      - S3_BUCKET=traceruntime-outputs
      - API_INTERNAL_URL=http://api:8082
      - AI_RUNTIME_ENABLED=false
    ports:
      - "9092:9091"
    networks: [traceruntime]
    depends_on:
      localstack:
        condition: service_healthy
      api:
        condition: service_healthy
      otel-collector:
        condition: service_started
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:9091/health"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 20s
    restart: unless-stopped
    init: true
    stop_grace_period: 15s
    mem_limit: 128m
    cpus: "0.5"
    profiles: [no-ai]

  ai-runtime:
    build:
      context: ./ai-runtime
      dockerfile: Dockerfile
    ports:
      - "8001:8000"
    environment:
      - OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317
      - OLLAMA_BASE_URL=http://host.docker.internal:11434
    extra_hosts:
      - "host.docker.internal:host-gateway"
    networks: [traceruntime]
    depends_on:
      otel-collector:
        condition: service_started
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:8000/health"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 20s
    restart: unless-stopped
    init: true
    stop_grace_period: 10s
    mem_limit: 768m
    cpus: "1.0"
    profiles: [full]

  frontend:
    build:
      context: ./frontend
      dockerfile: Dockerfile
    ports:
      - "3001:3001"
    environment:
      - OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317
    networks: [traceruntime]
    depends_on:
      api:
        condition: service_healthy
    healthcheck:
      test: ["CMD-SHELL", "wget -qO- http://127.0.0.1:3001/ > /dev/null 2>&1 || exit 1"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 15s
    restart: unless-stopped
    init: true
    mem_limit: 256m
    cpus: "0.5"
    profiles: [core, full, no-ai]
```

- [ ] **Step 2: Verify compose parses correctly**

```bash
docker compose --profile core config --quiet
```

Expected: no errors, exits 0.

- [ ] **Step 3: Commit**

```bash
git add docker-compose.yml
git commit -m "feat(phase6): add LocalStack + worker to docker-compose with profiles"
```

---

## Task 2: Bootstrap script — create SQS queues and S3 bucket

**Files:**
- Create: `scripts/bootstrap.sh`

**Context:** LocalStack needs SQS queue, DLQ, and S3 bucket created before the API and worker can use them. This script runs once after LocalStack is healthy. It uses `awslocal` (bundled in LocalStack) via `docker compose exec`.

- [ ] **Step 1: Create scripts/bootstrap.sh**

```bash
#!/usr/bin/env bash
set -euo pipefail

ENDPOINT="http://localhost:4566"
REGION="us-east-1"
ACCOUNT="000000000000"

echo "Creating DLQ..."
aws --endpoint-url="$ENDPOINT" --region="$REGION" sqs create-queue \
  --queue-name traceruntime-tasks-dlq \
  --attributes '{"MessageRetentionPeriod":"86400"}'

DLQ_ARN="arn:aws:sqs:${REGION}:${ACCOUNT}:traceruntime-tasks-dlq"

echo "Creating main task queue..."
aws --endpoint-url="$ENDPOINT" --region="$REGION" sqs create-queue \
  --queue-name traceruntime-tasks \
  --attributes "{
    \"VisibilityTimeout\": \"150\",
    \"MessageRetentionPeriod\": \"86400\",
    \"ReceiveMessageWaitTimeSeconds\": \"20\",
    \"RedrivePolicy\": \"{\\\"deadLetterTargetArn\\\":\\\"${DLQ_ARN}\\\",\\\"maxReceiveCount\\\":\\\"3\\\"}\"
  }"

echo "Creating S3 bucket..."
aws --endpoint-url="$ENDPOINT" --region="$REGION" s3api create-bucket \
  --bucket traceruntime-outputs

echo "Verifying..."
aws --endpoint-url="$ENDPOINT" --region="$REGION" sqs list-queues
aws --endpoint-url="$ENDPOINT" --region="$REGION" s3api list-buckets

echo "Bootstrap complete."
```

```bash
chmod +x scripts/bootstrap.sh
```

- [ ] **Step 2: Start LocalStack and run bootstrap**

```bash
# Start only localstack first (uses infra/observability/docker-compose.yml for otel-collector)
docker compose -f infra/observability/docker-compose.yml up -d otel-collector
docker compose --profile core up localstack -d

# Wait for healthy
docker compose ps localstack

# Run bootstrap (requires awscli locally, or run inside localstack container)
AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test bash scripts/bootstrap.sh
```

Expected output:
```
Creating DLQ...
{"QueueUrl": "http://localhost:4566/000000000000/traceruntime-tasks-dlq"}
Creating main task queue...
{"QueueUrl": "http://localhost:4566/000000000000/traceruntime-tasks"}
Creating S3 bucket...
...
Bootstrap complete.
```

- [ ] **Step 3: Commit**

```bash
git add scripts/bootstrap.sh
git commit -m "feat(phase6): add bootstrap script for SQS queues and S3 bucket"
```

---

## Task 3: Create services/worker — module, telemetry, metrics, health

**Files:**
- Create: `services/worker/go.mod`
- Create: `services/worker/internal/telemetry/otel.go`
- Create: `services/worker/internal/metrics/metrics.go`
- Create: `services/worker/cmd/worker/main.go` (skeleton only — polling added in Task 5)

**Context:** The worker is a new Go binary. It follows identical patterns to `services/api`: JSON structured logging, OTEL init, Prometheus `/metrics`, `/health` endpoint. Start with the skeleton so the binary compiles and healthchecks pass before adding SQS logic.

- [ ] **Step 1: Create go.mod**

```
module github.com/runtime-platform/services/worker

go 1.22
```

- [ ] **Step 2: Initialize dependencies**

```bash
cd services/worker
go mod tidy
```

We'll add AWS SDK deps in Task 4. For now the module just needs to compile.

- [ ] **Step 3: Create internal/telemetry/otel.go**

Identical to `services/api/internal/telemetry/otel.go` — copy verbatim, changing nothing:

```go
package telemetry

import (
	"context"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func Init(ctx context.Context, serviceName string) (shutdown func(context.Context) error, err error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "localhost:4317"
	}

	conn, err := grpc.NewClient(endpoint, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}

	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithGRPCConn(conn))
	if err != nil {
		return nil, err
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(serviceName),
			semconv.ServiceVersionKey.String("0.1.0"),
		),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return tp.Shutdown, nil
}
```

- [ ] **Step 4: Create internal/metrics/metrics.go**

```go
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	QueueDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "traceruntime_worker_queue_depth",
		Help: "Approximate number of messages in the main SQS queue.",
	})

	QueueInflight = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "traceruntime_worker_queue_inflight",
		Help: "Approximate number of messages currently being processed (not visible).",
	})

	DLQDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "traceruntime_worker_dlq_depth",
		Help: "Approximate number of messages in the DLQ.",
	})

	TaskDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "traceruntime_worker_task_duration_seconds",
		Help:    "End-to-end task processing time in seconds.",
		Buckets: []float64{1, 5, 10, 30, 60, 90, 120, 150},
	})

	SSEPublishErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "traceruntime_worker_sse_publish_errors_total",
		Help: "Total failed internal SSE publish calls to the API.",
	})

	TasksProcessed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "traceruntime_worker_tasks_processed_total",
		Help: "Total tasks successfully processed.",
	})

	TasksFailed = promauto.NewCounter(prometheus.CounterOpts{
		Name: "traceruntime_worker_tasks_failed_total",
		Help: "Total tasks that failed processing.",
	})

	SQSReceiveDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "traceruntime_worker_sqs_receive_duration_seconds",
		Help:    "Time spent waiting for SQS messages (long poll duration).",
		Buckets: []float64{0.1, 1, 5, 10, 20},
	})
)
```

- [ ] **Step 5: Create cmd/worker/main.go (skeleton)**

```go
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/runtime-platform/services/worker/internal/telemetry"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	shutdown, err := telemetry.Init(context.Background(), "traceruntime-worker")
	if err != nil {
		slog.Error("failed to init tracer", "error", err)
		os.Exit(1)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(ctx); err != nil {
			slog.Error("tracer shutdown error", "error", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{
		Addr:         ":9091",
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("worker metrics server starting", "addr", ":9091")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("metrics server error", "error", err)
			os.Exit(1)
		}
	}()

	slog.Info("worker started — polling not yet implemented")
	<-ctx.Done()

	slog.Info("worker shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		slog.Error("metrics server shutdown error", "error", err)
	}
}
```

- [ ] **Step 6: Create Dockerfile**

```dockerfile
FROM golang:1.22-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o worker ./cmd/worker

FROM alpine:3.19
RUN apk add --no-cache curl
WORKDIR /app
COPY --from=builder /app/worker .
EXPOSE 9091
CMD ["./worker"]
```

- [ ] **Step 7: Create .dockerignore**

```
*.exe
*.test
.git
```

- [ ] **Step 8: Install deps and verify build**

```bash
cd services/worker
go get github.com/prometheus/client_golang@v1.19.1
go get go.opentelemetry.io/otel@v1.43.0
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc@v1.43.0
go get go.opentelemetry.io/otel/sdk@v1.43.0
go get google.golang.org/grpc@v1.64.0
go mod tidy
go build ./cmd/worker
```

Expected: `worker` binary created, no errors.

- [ ] **Step 9: Commit**

```bash
cd ../..
git add services/worker/
git commit -m "feat(phase6): scaffold worker service — health, metrics, OTEL skeleton"
```

---

## Task 4: Add SQS polling to worker (receive + log only, no processing)

**Files:**
- Modify: `services/worker/cmd/worker/main.go`

**Context:** Add the SQS long-polling loop to the worker skeleton. In this task the worker only receives and logs messages — it does not process them or delete them from SQS. This lets us validate that SQS polling works correctly before adding processing logic.

- [ ] **Step 1: Install AWS SDK**

```bash
cd services/worker
go get github.com/aws/aws-sdk-go-v2@v1.26.0
go get github.com/aws/aws-sdk-go-v2/config@v1.27.0
go get github.com/aws/aws-sdk-go-v2/service/sqs@v1.32.0
go mod tidy
```

- [ ] **Step 2: Update main.go with SQS polling loop**

```go
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/runtime-platform/services/worker/internal/metrics"
	"github.com/runtime-platform/services/worker/internal/telemetry"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	shutdown, err := telemetry.Init(context.Background(), "traceruntime-worker")
	if err != nil {
		slog.Error("failed to init tracer", "error", err)
		os.Exit(1)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdown(ctx); err != nil {
			slog.Error("tracer shutdown error", "error", err)
		}
	}()

	queueURL := mustEnv("SQS_QUEUE_URL")
	dlqURL := mustEnv("SQS_DLQ_URL")

	cfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		slog.Error("failed to load AWS config", "error", err)
		os.Exit(1)
	}
	sqsClient := sqs.NewFromConfig(cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Periodic queue depth polling (every 10s)
	go pollQueueDepth(ctx, sqsClient, queueURL, dlqURL)

	// Main SQS polling loop
	go pollMessages(ctx, sqsClient, queueURL)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{
		Addr:         ":9091",
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("worker metrics server starting", "addr", ":9091")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("metrics server error", "error", err)
			os.Exit(1)
		}
	}()

	slog.Info("worker started", "queue_url", queueURL)
	<-ctx.Done()

	slog.Info("worker shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		slog.Error("metrics server shutdown error", "error", err)
	}
}

func pollMessages(ctx context.Context, client *sqs.Client, queueURL string) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		start := time.Now()
		out, err := client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:              aws.String(queueURL),
			MaxNumberOfMessages:   1,
			WaitTimeSeconds:       20,
			MessageAttributeNames: []string{"traceparent"},
			AttributeNames:        []string{"ApproximateReceiveCount"},
		})
		metrics.SQSReceiveDuration.Observe(time.Since(start).Seconds())

		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("sqs receive error", "error", err)
			time.Sleep(2 * time.Second)
			continue
		}

		for _, msg := range out.Messages {
			traceparent := ""
			if attr, ok := msg.MessageAttributes["traceparent"]; ok && attr.StringValue != nil {
				traceparent = *attr.StringValue
			}
			receiveCount := ""
			if attr, ok := msg.Attributes["ApproximateReceiveCount"]; ok {
				receiveCount = attr
			}
			slog.Info("received sqs message",
				"message_id", aws.ToString(msg.MessageId),
				"traceparent", traceparent,
				"receive_count", receiveCount,
				"body_len", len(aws.ToString(msg.Body)),
			)
			// Task 5 will add processing + delete here
		}
	}
}

func pollQueueDepth(ctx context.Context, client *sqs.Client, queueURL, dlqURL string) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			updateDepth(ctx, client, queueURL, metrics.QueueDepth.Set, metrics.QueueInflight.Set)
			updateDepth(ctx, client, dlqURL, metrics.DLQDepth.Set, nil)
		}
	}
}

func updateDepth(ctx context.Context, client *sqs.Client, url string, setDepth func(float64), setInflight func(float64)) {
	out, err := client.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl: aws.String(url),
		AttributeNames: []string{
			"ApproximateNumberOfMessages",
			"ApproximateNumberOfMessagesNotVisible",
		},
	})
	if err != nil {
		slog.Warn("failed to get queue attributes", "url", url, "error", err)
		return
	}
	if v, ok := out.Attributes["ApproximateNumberOfMessages"]; ok {
		var n float64
		if _, err := fmt.Sscanf(v, "%f", &n); err == nil {
			setDepth(n)
		}
	}
	if setInflight != nil {
		if v, ok := out.Attributes["ApproximateNumberOfMessagesNotVisible"]; ok {
			var n float64
			if _, err := fmt.Sscanf(v, "%f", &n); err == nil {
				setInflight(n)
			}
		}
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("required env var not set", "key", key)
		os.Exit(1)
	}
	return v
}
```

Add `"fmt"` to the import block.

- [ ] **Step 3: Verify build**

```bash
cd services/worker
go build ./cmd/worker
```

Expected: compiles without errors.

- [ ] **Step 4: Validate polling locally**

With LocalStack running and bootstrap done:

```bash
# Send a test message
AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test \
aws --endpoint-url=http://localhost:4566 sqs send-message \
  --queue-url http://localhost:4566/000000000000/traceruntime-tasks \
  --message-body '{"task_id":"test-1","trace_id":"abc","payload":"hello"}' \
  --message-attributes '{"traceparent":{"DataType":"String","StringValue":"00-abc123-def456-01"}}'

# Run worker
SQS_QUEUE_URL=http://localhost:4566/000000000000/traceruntime-tasks \
SQS_DLQ_URL=http://localhost:4566/000000000000/traceruntime-tasks-dlq \
AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test \
AWS_DEFAULT_REGION=us-east-1 \
AWS_ENDPOINT_URL=http://localhost:4566 \
./services/worker/worker
```

Expected log line:
```json
{"level":"INFO","msg":"received sqs message","message_id":"...","traceparent":"00-abc123-def456-01","receive_count":"1","body_len":65}
```

- [ ] **Step 5: Commit**

```bash
git add services/worker/
git commit -m "feat(phase6): worker SQS long-polling loop with depth metrics"
```

---

## Task 5: Worker — task processing and SQS delete

**Files:**
- Create: `services/worker/internal/processor/processor.go`
- Modify: `services/worker/cmd/worker/main.go`

**Context:** The worker now needs to actually process messages: extract traceparent, create OTEL child span, call AI runtime (or mock), write to S3, publish SSE event via API, and delete the message from SQS. The message is only deleted after successful processing to preserve at-least-once delivery semantics.

- [ ] **Step 1: Install S3 SDK**

```bash
cd services/worker
go get github.com/aws/aws-sdk-go-v2/service/s3@v1.54.0
go mod tidy
```

- [ ] **Step 2: Create internal/processor/processor.go**

```go
package processor

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	sqssdk "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"

	"github.com/runtime-platform/services/worker/internal/metrics"
)

var tracer = otel.Tracer("traceruntime-worker/processor")

type Config struct {
	SQSURL         string
	S3Bucket       string
	APIInternalURL string
	AIRuntimeURL   string
	AIEnabled      bool
}

type Processor struct {
	cfg    Config
	sqs    *sqssdk.Client
	s3     *s3.Client
	http   *http.Client
}

func New(cfg Config, sqsClient *sqssdk.Client, s3Client *s3.Client) *Processor {
	return &Processor{
		cfg:  cfg,
		sqs:  sqsClient,
		s3:   s3Client,
		http: &http.Client{Timeout: 5 * time.Second},
	}
}

type sqsMessage struct {
	TaskID      string `json:"task_id"`
	TraceID     string `json:"trace_id"`
	Traceparent string `json:"traceparent"`
	Payload     string `json:"payload"`
}

type sseEvent struct {
	EventID             string `json:"event_id"`
	EventType           string `json:"event_type"`
	TraceID             string `json:"trace_id"`
	Traceparent         string `json:"traceparent"`
	TaskID              string `json:"task_id"`
	Timestamp           string `json:"timestamp"`
	Source              string `json:"source"`
	Output              string `json:"output,omitempty"`
	Model               string `json:"model,omitempty"`
	ExecutionStatus     string `json:"execution_status,omitempty"`
	InferenceDurationMs int    `json:"inference_duration_ms,omitempty"`
	ErrorReason         string `json:"error_reason,omitempty"`
	S3Key               string `json:"s3_key,omitempty"`
}

// Process handles a single SQS message. Deletes it from SQS only on success.
func (p *Processor) Process(ctx context.Context, body string, traceparentAttr string, receiptHandle string) {
	start := time.Now()

	var msg sqsMessage
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		slog.Error("failed to parse sqs message body", "error", err, "body", body)
		return // leave in queue for redelivery
	}

	// Use traceparent from message attribute (authoritative), fall back to body field
	traceparent := traceparentAttr
	if traceparent == "" {
		traceparent = msg.Traceparent
	}

	// Reconstruct OTEL context from traceparent
	carrier := propagation.MapCarrier{"traceparent": traceparent}
	parentCtx := otel.GetTextMapPropagator().Extract(ctx, carrier)

	processCtx, span := tracer.Start(parentCtx, "task.process")
	defer span.End()

	spanCtx := span.SpanContext()
	childTraceparent := fmt.Sprintf("00-%s-%s-01",
		spanCtx.TraceID().String(),
		spanCtx.SpanID().String(),
	)
	traceID := spanCtx.TraceID().String()

	slog.Info("processing task",
		"task_id", msg.TaskID,
		"trace_id", traceID,
		"traceparent", childTraceparent,
	)

	p.publishSSE(sseEvent{
		EventType:   "task.processing",
		TraceID:     traceID,
		Traceparent: childTraceparent,
		TaskID:      msg.TaskID,
		Source:      "worker",
	})

	var output, model string
	var durationMs int

	if !p.cfg.AIEnabled {
		output = fmt.Sprintf("mock output for task %s", msg.TaskID)
		model = "mock"
		durationMs = 0
		slog.Info("AI runtime disabled — using mock output", "task_id", msg.TaskID)
	} else {
		var err error
		output, model, durationMs, err = p.callAIRuntime(processCtx, msg, childTraceparent)
		if err != nil {
			slog.Error("ai runtime call failed", "task_id", msg.TaskID, "error", err)
			metrics.TasksFailed.Inc()
			p.publishSSE(sseEvent{
				EventType:   "task.failed",
				TraceID:     traceID,
				Traceparent: childTraceparent,
				TaskID:      msg.TaskID,
				Source:      "worker",
				ErrorReason: "ai_runtime_error",
			})
			return // do NOT delete from SQS — allow redelivery
		}
	}

	// Write output to S3
	s3Key := fmt.Sprintf("%s/%s.json", traceID, msg.TaskID)
	s3Payload, _ := json.Marshal(map[string]any{
		"task_id":    msg.TaskID,
		"trace_id":   traceID,
		"output":     output,
		"model":      model,
		"created_at": time.Now().UTC().Format(time.RFC3339),
	})
	_, err := p.s3.PutObject(processCtx, &s3.PutObjectInput{
		Bucket:      aws.String(p.cfg.S3Bucket),
		Key:         aws.String(s3Key),
		Body:        bytes.NewReader(s3Payload),
		ContentType: aws.String("application/json"),
	})
	if err != nil {
		slog.Error("failed to write output to s3", "task_id", msg.TaskID, "error", err)
		metrics.TasksFailed.Inc()
		p.publishSSE(sseEvent{
			EventType:   "task.failed",
			TraceID:     traceID,
			Traceparent: childTraceparent,
			TaskID:      msg.TaskID,
			Source:      "worker",
			ErrorReason: "s3_write_error",
		})
		return // do NOT delete from SQS
	}

	// Delete from SQS only after successful processing
	_, err = p.sqs.DeleteMessage(processCtx, &sqssdk.DeleteMessageInput{
		QueueUrl:      aws.String(p.cfg.SQSURL),
		ReceiptHandle: aws.String(receiptHandle),
	})
	if err != nil {
		slog.Error("failed to delete sqs message", "task_id", msg.TaskID, "error", err)
		// task was processed successfully; log but don't fail
	}

	metrics.TasksProcessed.Inc()
	metrics.TaskDuration.Observe(time.Since(start).Seconds())

	slog.Info("task completed",
		"task_id", msg.TaskID,
		"trace_id", traceID,
		"model", model,
		"duration_ms", durationMs,
		"s3_key", s3Key,
	)

	p.publishSSE(sseEvent{
		EventType:           "task.completed",
		TraceID:             traceID,
		Traceparent:         childTraceparent,
		TaskID:              msg.TaskID,
		Source:              "worker",
		Output:              output,
		Model:               model,
		ExecutionStatus:     "completed",
		InferenceDurationMs: durationMs,
		S3Key:               s3Key,
	})
}

func (p *Processor) callAIRuntime(ctx context.Context, msg sqsMessage, traceparent string) (output, model string, durationMs int, err error) {
	deadline := time.Now().Add(120 * time.Second)
	inferCtx, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()

	body, _ := json.Marshal(map[string]any{
		"task_id":          msg.TaskID,
		"input":            msg.Payload,
		"deadline_unix_ms": deadline.UnixMilli(),
	})

	req, err := http.NewRequestWithContext(inferCtx, http.MethodPost, p.cfg.AIRuntimeURL+"/infer", bytes.NewReader(body))
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("traceparent", traceparent)

	client := &http.Client{Timeout: 125 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 500 {
		return "", "", 0, fmt.Errorf("ai runtime HTTP %d", resp.StatusCode)
	}

	var result struct {
		Output              string `json:"output"`
		ExecutionStatus     string `json:"execution_status"`
		InferenceDurationMs int    `json:"inference_duration_ms"`
		ExecutionProfile    struct {
			Model string `json:"model"`
		} `json:"execution_profile"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", "", 0, err
	}
	if result.ExecutionStatus == "failed" {
		return "", "", 0, fmt.Errorf("ai runtime returned failed status")
	}
	return result.Output, result.ExecutionProfile.Model, result.InferenceDurationMs, nil
}

func (p *Processor) publishSSE(ev sseEvent) {
	ev.EventID = uuid.New().String()
	ev.Timestamp = time.Now().UTC().Format(time.RFC3339)

	body, err := json.Marshal(ev)
	if err != nil {
		slog.Error("failed to marshal sse event", "error", err)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.cfg.APIInternalURL+"/internal/events",
		bytes.NewReader(body),
	)
	if err != nil {
		slog.Error("failed to build sse publish request", "error", err)
		metrics.SSEPublishErrors.Inc()
		return
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.http.Do(req)
	if err != nil {
		slog.Warn("sse publish failed — api unreachable", "error", err)
		metrics.SSEPublishErrors.Inc()
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		slog.Warn("sse publish returned error status", "status", resp.StatusCode)
		metrics.SSEPublishErrors.Inc()
	}
}

func envBool(key string, def bool) bool {
	v := strings.ToLower(os.Getenv(key))
	if v == "false" || v == "0" {
		return false
	}
	if v == "true" || v == "1" {
		return true
	}
	return def
}
```

- [ ] **Step 3: Update main.go to use Processor**

Replace `pollMessages` in main.go to wire the processor:

```go
// Add to imports: s3sdk "github.com/aws/aws-sdk-go-v2/service/s3"
// Add to imports: "github.com/runtime-platform/services/worker/internal/processor"
// Add to imports: "strings"

// After sqsClient := sqs.NewFromConfig(cfg), add:
s3Client := s3sdk.NewFromConfig(cfg)

proc := processor.New(processor.Config{
    SQSURL:         queueURL,
    S3Bucket:       mustEnv("S3_BUCKET"),
    APIInternalURL: mustEnv("API_INTERNAL_URL"),
    AIRuntimeURL:   os.Getenv("AI_RUNTIME_URL"),
    AIEnabled:      strings.ToLower(os.Getenv("AI_RUNTIME_ENABLED")) != "false",
}, sqsClient, s3Client)
```

Update `pollMessages` to call `proc.Process`:

```go
func pollMessages(ctx context.Context, client *sqs.Client, queueURL string, proc *processor.Processor) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		start := time.Now()
		out, err := client.ReceiveMessage(ctx, &sqs.ReceiveMessageInput{
			QueueUrl:              aws.String(queueURL),
			MaxNumberOfMessages:   1,
			WaitTimeSeconds:       20,
			MessageAttributeNames: []string{"traceparent"},
			AttributeNames:        []string{"ApproximateReceiveCount"},
		})
		metrics.SQSReceiveDuration.Observe(time.Since(start).Seconds())

		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.Error("sqs receive error", "error", err)
			time.Sleep(2 * time.Second)
			continue
		}

		for _, msg := range out.Messages {
			traceparent := ""
			if attr, ok := msg.MessageAttributes["traceparent"]; ok && attr.StringValue != nil {
				traceparent = *attr.StringValue
			}
			proc.Process(ctx, aws.ToString(msg.Body), traceparent, aws.ToString(msg.ReceiptHandle))
		}
	}
}
```

Pass `proc` to the `pollMessages` goroutine call in `main()`:
```go
go pollMessages(ctx, sqsClient, queueURL, proc)
```

- [ ] **Step 4: Build and validate**

```bash
cd services/worker
go get github.com/google/uuid
go mod tidy
go build ./cmd/worker
```

Expected: compiles without errors.

- [ ] **Step 5: Commit**

```bash
cd ../..
git add services/worker/
git commit -m "feat(phase6): worker task processing — SQS receive, S3 write, SSE publish"
```

---

## Task 6: Add /internal/events endpoint to the API

**Files:**
- Create: `services/api/internal/http/events.go`
- Modify: `services/api/internal/http/router.go`

**Context:** The worker publishes SSE events by calling `POST /internal/events` on the API. The API receives the event JSON and fans it out via the existing SSE broker. This handler must never block — it accepts, publishes, and returns 202 immediately.

- [ ] **Step 1: Create services/api/internal/http/events.go**

```go
package http

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"

	"github.com/runtime-platform/services/api/internal/event"
)

type EventsHandler struct {
	broker *event.Broker
}

func NewEventsHandler(broker *event.Broker) *EventsHandler {
	return &EventsHandler{broker: broker}
}

func (h *EventsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, 64*1024))
	if err != nil {
		slog.Warn("failed to read /internal/events body", "error", err)
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// Validate it's valid JSON before broadcasting
	if !json.Valid(body) {
		slog.Warn("invalid json in /internal/events body")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	h.broker.Publish(body)

	w.WriteHeader(http.StatusAccepted)
}
```

- [ ] **Step 2: Register route in router.go**

In `services/api/internal/http/router.go`, add the events handler. Locate where `NewRouter` is defined and add the route. The current router uses chi:

```go
// In NewRouter function signature, add eventsHandler parameter:
func NewRouter(broker *event.Broker, q *queue.Queue) http.Handler {
```

Add this after the existing route registrations:

```go
eventsHandler := NewEventsHandler(broker)
r.Post("/internal/events", eventsHandler.ServeHTTP)
```

- [ ] **Step 3: Verify build**

```bash
cd services/api
go build ./cmd/server
```

Expected: no errors.

- [ ] **Step 4: Test the endpoint manually**

With the API running:
```bash
curl -s -X POST http://localhost:8082/internal/events \
  -H "Content-Type: application/json" \
  -d '{"event_type":"task.completed","task_id":"test-123","trace_id":"abc"}' \
  -w "\nHTTP %{http_code}\n"
```

Expected: `HTTP 202`. Check that connected SSE clients receive the event.

- [ ] **Step 5: Commit**

```bash
cd ../..
git add services/api/internal/http/events.go services/api/internal/http/router.go
git commit -m "feat(phase6): add /internal/events endpoint for worker→SSE bridge"
```

---

## Task 7: API — publish tasks to SQS when QUEUE_BACKEND=sqs

**Files:**
- Modify: `services/api/internal/http/task.go`
- Modify: `services/api/cmd/server/main.go`
- Modify: `services/api/go.mod` (add AWS SDK)

**Context:** When `QUEUE_BACKEND=sqs`, the API sends the task as a SQS message instead of enqueuing into the local channel. The `task.go` handler already has the trace context — it becomes the `traceparent` message attribute. The local queue remains for `inmemory` mode.

- [ ] **Step 1: Add AWS SDK to services/api**

```bash
cd services/api
go get github.com/aws/aws-sdk-go-v2@v1.26.0
go get github.com/aws/aws-sdk-go-v2/config@v1.27.0
go get github.com/aws/aws-sdk-go-v2/service/sqs@v1.32.0
go mod tidy
```

- [ ] **Step 2: Update services/api/internal/http/task.go**

Add a `Publisher` interface and SQS implementation at the top of the file (before the `TaskHandler` struct):

```go
// Publisher is the interface for enqueuing tasks. Implemented by local queue or SQS.
type Publisher interface {
	Publish(ctx context.Context, taskID, traceID, traceparent, payload string) (bool, error)
}
```

Add SQS publisher struct:

```go
type SQSPublisher struct {
	client   *sqssdk.Client
	queueURL string
}

func NewSQSPublisher(client *sqssdk.Client, queueURL string) *SQSPublisher {
	return &SQSPublisher{client: client, queueURL: queueURL}
}

func (p *SQSPublisher) Publish(ctx context.Context, taskID, traceID, traceparent, payload string) (bool, error) {
	body, err := json.Marshal(map[string]string{
		"task_id":     taskID,
		"trace_id":    traceID,
		"traceparent": traceparent,
		"payload":     payload,
	})
	if err != nil {
		return false, err
	}

	_, err = p.client.SendMessage(ctx, &sqssdk.SendMessageInput{
		QueueUrl:    aws.String(p.queueURL),
		MessageBody: aws.String(string(body)),
		MessageAttributes: map[string]sqssdk.types.MessageAttributeValue{
			"traceparent": {
				DataType:    aws.String("String"),
				StringValue: aws.String(traceparent),
			},
		},
	})
	if err != nil {
		return false, err
	}
	return true, nil
}
```

Add queue-based publisher for inmemory mode:

```go
type QueuePublisher struct {
	q *queue.Queue
}

func NewQueuePublisher(q *queue.Queue) *QueuePublisher {
	return &QueuePublisher{q: q}
}

func (p *QueuePublisher) Publish(_ context.Context, taskID, traceID, traceparent, payload string) (bool, error) {
	ok := p.q.Enqueue(queue.Task{
		ID:          taskID,
		TraceID:     traceID,
		Traceparent: traceparent,
		Payload:     payload,
	})
	return ok, nil
}
```

Update `TaskHandler` struct:

```go
type TaskHandler struct {
	broker    *event.Broker
	publisher Publisher
}

func NewTaskHandler(broker *event.Broker, publisher Publisher) *TaskHandler {
	return &TaskHandler{broker: broker, publisher: publisher}
}
```

Update the enqueue block in `ServeHTTP`:

```go
_, enqueueSpan := taskTracer.Start(ctx, "task.enqueue")
ok, err := h.publisher.Publish(ctx, taskID, traceID, traceparent, req.Input)
if err != nil {
    enqueueSpan.End()
    telemetry.Error(ctx, "failed to publish task", "task_id", taskID, "error", err)
    jsonError(w, "internal error", http.StatusInternalServerError)
    return
}
if !ok {
    enqueueSpan.End()
    metrics.QueueRejected.Inc()
    telemetry.Warn(ctx, "task rejected — queue full", "task_id", taskID)
    jsonError(w, "queue_full", http.StatusServiceUnavailable)
    return
}
metrics.QueueEnqueued.Inc()
enqueueSpan.End()
```

Add imports needed: `"github.com/aws/aws-sdk-go-v2/aws"`, `sqssdk "github.com/aws/aws-sdk-go-v2/service/sqs"`, `sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"`, `"context"`.

- [ ] **Step 3: Update cmd/server/main.go**

Replace the queue+worker initialization block. The worker goroutine is removed (it now runs in a separate container). Add SQS publisher initialization:

```go
import (
	// existing imports...
	"github.com/aws/aws-sdk-go-v2/config"
	sqssdk "github.com/aws/aws-sdk-go-v2/service/sqs"
)

// In main(), replace the queue/worker block:
var publisher apihttp.Publisher

queueBackend := envString("QUEUE_BACKEND", "inmemory")
switch queueBackend {
case "sqs":
	awsCfg, err := config.LoadDefaultConfig(context.Background())
	if err != nil {
		slog.Error("failed to load AWS config", "error", err)
		os.Exit(1)
	}
	sqsClient := sqssdk.NewFromConfig(awsCfg)
	sqsQueueURL := mustEnv("SQS_QUEUE_URL")
	publisher = apihttp.NewSQSPublisher(sqsClient, sqsQueueURL)
	slog.Info("queue backend: sqs", "queue_url", sqsQueueURL)
default:
	q := queue.NewQueue(envInt("QUEUE_CAPACITY", 128))
	metrics.MustRegisterAll(func() float64 { return float64(q.Depth()) })
	publisher = apihttp.NewQueuePublisher(q)
	aiClient := ai.NewClient(aiRuntimeURL, 120*time.Second)
	w := worker.New(q, broker, aiClient)
	go w.Run(ctx)
	slog.Info("queue backend: inmemory")
}

router := apihttp.NewRouter(broker, publisher)
```

Add `mustEnv` helper to main.go (alongside `envInt`/`envString`):

```go
func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("required env var not set", "key", key)
		os.Exit(1)
	}
	return v
}
```

Update `NewRouter` signature in `router.go` to accept `Publisher` instead of `*queue.Queue`:

```go
func NewRouter(broker *event.Broker, publisher Publisher) http.Handler {
    // ...
    taskHandler := NewTaskHandler(broker, publisher)
    // ...
}
```

- [ ] **Step 4: Verify build**

```bash
cd services/api
go build ./cmd/server
```

Expected: no errors.

- [ ] **Step 5: Commit**

```bash
cd ../..
git add services/api/
git commit -m "feat(phase6): API publishes to SQS when QUEUE_BACKEND=sqs"
```

---

## Task 8: Remove worker and queue packages from API (inmemory path cleanup)

**Files:**
- Modify: `services/api/internal/worker/worker.go` (keep for inmemory mode)
- Modify: `services/api/cmd/server/main.go`

**Context:** The inmemory mode still uses the existing worker goroutine. We don't delete those packages — they remain as the fallback. The change from Task 7 already handles this correctly: when `QUEUE_BACKEND=inmemory`, the old queue+worker goroutine still runs inside the API process. This task validates that inmemory mode still works end-to-end and the compile is clean.

- [ ] **Step 1: Verify inmemory mode builds and works**

```bash
cd services/api
QUEUE_BACKEND=inmemory go run ./cmd/server
```

Send a task:
```bash
curl -s -X POST http://localhost:8082/tasks \
  -H "Content-Type: application/json" \
  -d '{"input": "test inmemory path"}'
```

Expected: task created, worker processes it (or fails gracefully if AI runtime not available), no panics.

- [ ] **Step 2: Verify sqs mode builds**

```bash
cd services/api
QUEUE_BACKEND=sqs SQS_QUEUE_URL=http://localhost:4566/000000000000/traceruntime-tasks \
AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test AWS_DEFAULT_REGION=us-east-1 \
AWS_ENDPOINT_URL=http://localhost:4566 go run ./cmd/server
```

Expected: starts without errors, logs `"queue backend: sqs"`.

- [ ] **Step 3: Commit**

```bash
cd ../..
git add services/api/
git commit -m "feat(phase6): validate inmemory and sqs modes both compile and start"
```

---

## Task 9: Build worker Docker image and validate compose core profile

**Files:**
- Verify: `services/worker/Dockerfile`
- Modify: `infra/observability/prometheus/prometheus.yaml` (add worker scrape target)

**Context:** Build the worker container and verify the `core` profile starts cleanly end-to-end. Run bootstrap first, then start compose, then send a task and verify it flows through SQS → worker → S3 → SSE.

- [ ] **Step 1: Build worker image**

```bash
docker build -t traceruntime-worker services/worker/
```

Expected: image built successfully.

- [ ] **Step 2: Add worker scrape target to Prometheus**

In `infra/observability/prometheus/prometheus.yaml`, add to the `scrape_configs` list:

```yaml
  - job_name: 'traceruntime-worker'
    static_configs:
      - targets: ['worker:9091']
```

- [ ] **Step 3: Start core profile + observability**

```bash
# Start observability stack first (needed for OTEL collector)
docker compose -f infra/observability/docker-compose.yml up -d

# Run bootstrap
AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test bash scripts/bootstrap.sh

# Start core profile (localstack already running, api, worker-no-ai, frontend)
docker compose --profile no-ai up -d

# Wait for all containers healthy
docker compose ps
```

Expected: localstack, api, worker-no-ai, frontend all show `(healthy)`.

- [ ] **Step 4: Send a task and verify flow**

```bash
# Create task
curl -s -X POST http://localhost:8082/tasks \
  -H "Content-Type: application/json" \
  -d '{"input": "test distributed flow"}' | jq .
```

Expected response:
```json
{
  "task_id": "...",
  "trace_id": "...",
  "traceparent": "00-..."
}
```

Check worker logs:
```bash
docker compose logs worker-no-ai --tail=20
```

Expected log lines:
```
{"level":"INFO","msg":"received sqs message","message_id":"...","traceparent":"..."}
{"level":"INFO","msg":"processing task","task_id":"...","trace_id":"..."}
{"level":"INFO","msg":"task completed","task_id":"...","s3_key":"..."}
```

Verify S3 object was written:
```bash
AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test \
aws --endpoint-url=http://localhost:4566 s3 ls s3://traceruntime-outputs/ --recursive
```

Expected: one `.json` file per task.

Verify SSE event reached frontend: open browser at `http://localhost:3001` and confirm task appears with `completed` status.

- [ ] **Step 5: Verify metrics in Prometheus**

```bash
curl -s http://localhost:9091/metrics | grep traceruntime_worker
```

Expected: counters and gauges visible.

- [ ] **Step 6: Commit**

```bash
git add infra/observability/prometheus/prometheus.yaml
git commit -m "feat(phase6): add worker Prometheus scrape target, validate core profile"
```

---

## Task 10: Validate trace continuity end-to-end

**Files:** No code changes — validation only.

**Context:** With the full flow working, verify that the same `trace_id` appears in Tempo from the HTTP request span through the worker span. This validates that W3C trace propagation via SQS message attributes is working correctly.

- [ ] **Step 1: Start full observability + core profile**

```bash
docker compose -f infra/observability/docker-compose.yml up -d
docker compose --profile no-ai up -d
```

- [ ] **Step 2: Send a task and capture the trace_id**

```bash
TRACE_ID=$(curl -s -X POST http://localhost:8082/tasks \
  -H "Content-Type: application/json" \
  -d '{"input": "trace continuity test"}' | jq -r .trace_id)
echo "trace_id: $TRACE_ID"
```

- [ ] **Step 3: Wait 10s for spans to be exported, then query Tempo**

```bash
sleep 10
curl -s "http://localhost:3200/api/traces/$TRACE_ID" | jq '.batches[].scopeSpans[].spans[].name'
```

Expected output includes both:
```
"task.create"
"task.process"
```

Both spans share the same `traceId`. The worker span (`task.process`) is a child of the API span (`task.create`).

- [ ] **Step 4: Document result**

If trace continuity is confirmed, no further action needed. If spans are missing, check:
- Worker OTEL_EXPORTER_OTLP_ENDPOINT is set to `otel-collector:4317`
- Traceparent attribute is present in the SQS message (check worker logs for `"traceparent":"00-..."`)

---

## Task 11: Validate worker crash recovery

**Files:** No code changes — validation only.

**Context:** Verify that when the worker container is killed mid-processing, the message reappears after the visibility timeout and is processed successfully.

- [ ] **Step 1: Set a short visibility timeout for testing**

Recreate the queue with a short timeout (30s) so you don't wait 150s:

```bash
AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test \
aws --endpoint-url=http://localhost:4566 sqs delete-queue \
  --queue-url http://localhost:4566/000000000000/traceruntime-tasks

sleep 2

AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test \
aws --endpoint-url=http://localhost:4566 sqs create-queue \
  --queue-name traceruntime-tasks \
  --attributes '{"VisibilityTimeout":"30","RedrivePolicy":"{\"deadLetterTargetArn\":\"arn:aws:sqs:us-east-1:000000000000:traceruntime-tasks-dlq\",\"maxReceiveCount\":\"3\"}"}'
```

- [ ] **Step 2: Send a task, then immediately kill the worker**

```bash
curl -s -X POST http://localhost:8082/tasks \
  -H "Content-Type: application/json" \
  -d '{"input": "crash recovery test"}'

# Kill the worker immediately
docker compose kill worker-no-ai
```

- [ ] **Step 3: Verify message is in-flight**

```bash
AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test \
aws --endpoint-url=http://localhost:4566 sqs get-queue-attributes \
  --queue-url http://localhost:4566/000000000000/traceruntime-tasks \
  --attribute-names ApproximateNumberOfMessages ApproximateNumberOfMessagesNotVisible
```

Expected: `ApproximateNumberOfMessagesNotVisible: 1` (message is invisible while "being processed").

- [ ] **Step 4: Wait for visibility timeout, restart worker**

```bash
sleep 35  # wait for visibility timeout (30s + margin)

docker compose --profile no-ai up worker-no-ai -d
```

- [ ] **Step 5: Verify redelivery and successful processing**

```bash
docker compose logs worker-no-ai --tail=30
```

Expected: log shows `receive_count: 2` and task completes successfully. The task appears in the frontend as `completed`.

- [ ] **Step 6: Restore production visibility timeout**

```bash
bash scripts/bootstrap.sh
```

---

## Task 12: Terraform — provision SQS, DLQ, S3

**Files:**
- Create: `infra/terraform/providers.tf`
- Create: `infra/terraform/variables.tf`
- Create: `infra/terraform/main.tf`
- Create: `infra/terraform/outputs.tf`
- Create: `Makefile`

**Context:** Terraform provisions the same resources as `scripts/bootstrap.sh` — SQS main queue, SQS DLQ, S3 bucket — against LocalStack. Terraform does NOT run as part of compose startup. It runs explicitly via `make infra-apply`. Local `.tfstate` is committed.

- [ ] **Step 1: Create infra/terraform/providers.tf**

```hcl
terraform {
  required_version = ">= 1.6"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region                      = var.aws_region
  access_key                  = "test"
  secret_key                  = "test"
  skip_credentials_validation = true
  skip_requesting_account_id  = true
  skip_metadata_api_check     = true

  endpoints {
    sqs = var.localstack_endpoint
    s3  = var.localstack_endpoint
  }
}
```

- [ ] **Step 2: Create infra/terraform/variables.tf**

```hcl
variable "aws_region" {
  default = "us-east-1"
}

variable "localstack_endpoint" {
  default = "http://localhost:4566"
}

variable "queue_name" {
  default = "traceruntime-tasks"
}

variable "dlq_name" {
  default = "traceruntime-tasks-dlq"
}

variable "s3_bucket" {
  default = "traceruntime-outputs"
}

variable "visibility_timeout" {
  default = 150
}

variable "max_receive_count" {
  default = 3
}
```

- [ ] **Step 3: Create infra/terraform/main.tf**

```hcl
resource "aws_sqs_queue" "dlq" {
  name                       = var.dlq_name
  message_retention_seconds  = 86400
}

resource "aws_sqs_queue" "tasks" {
  name                       = var.queue_name
  visibility_timeout_seconds = var.visibility_timeout
  message_retention_seconds  = 86400
  receive_wait_time_seconds  = 20

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq.arn
    maxReceiveCount     = var.max_receive_count
  })
}

resource "aws_s3_bucket" "outputs" {
  bucket = var.s3_bucket
}
```

- [ ] **Step 4: Create infra/terraform/outputs.tf**

```hcl
output "queue_url" {
  value = aws_sqs_queue.tasks.url
}

output "dlq_url" {
  value = aws_sqs_queue.dlq.url
}

output "s3_bucket" {
  value = aws_s3_bucket.outputs.bucket
}
```

- [ ] **Step 5: Create Makefile**

```makefile
.PHONY: infra-bootstrap infra-apply infra-destroy

infra-bootstrap:
	@echo "Creating LocalStack resources via bootstrap script..."
	AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test bash scripts/bootstrap.sh

infra-apply:
	@echo "Applying Terraform against LocalStack..."
	cd infra/terraform && terraform init -upgrade && terraform apply -auto-approve

infra-destroy:
	@echo "Destroying Terraform-managed LocalStack resources..."
	cd infra/terraform && terraform destroy -auto-approve
```

- [ ] **Step 6: Verify Terraform plan against LocalStack**

With LocalStack running:

```bash
make infra-apply
```

Expected output ends with:
```
Apply complete! Resources: 3 added, 0 changed, 0 destroyed.

Outputs:
queue_url = "http://localhost:4566/000000000000/traceruntime-tasks"
dlq_url   = "http://localhost:4566/000000000000/traceruntime-tasks-dlq"
s3_bucket = "traceruntime-outputs"
```

- [ ] **Step 7: Verify destroy + re-apply reproduces clean environment**

```bash
make infra-destroy
make infra-apply

# Verify queues exist
AWS_ACCESS_KEY_ID=test AWS_SECRET_ACCESS_KEY=test \
aws --endpoint-url=http://localhost:4566 sqs list-queues
```

Expected: both queues listed after re-apply.

- [ ] **Step 8: Commit**

```bash
git add infra/terraform/ Makefile
git commit -m "feat(phase7): Terraform modules for SQS, DLQ, S3 against LocalStack"
```

---

## Self-Review

**Spec coverage check:**

| Spec requirement | Task |
|---|---|
| LocalStack in docker-compose | Task 1 |
| Compose profiles (core, no-ai, full) | Task 1 |
| Bootstrap script SQS+DLQ+S3 | Task 2 |
| Worker separate binary + container | Task 3 |
| SQS long polling WaitTimeSeconds=20 | Task 4 |
| MaxNumberOfMessages=1 | Task 4 |
| Queue depth metrics (10s poll) | Task 4 |
| DLQ depth metric (30s poll) | Task 4 |
| Task processing + S3 write | Task 5 |
| SQS delete after success only | Task 5 |
| AI_RUNTIME_ENABLED=false mock mode | Task 5 |
| SSE publish via /internal/events | Task 5 + 6 |
| SSE publish best-effort (2s timeout) | Task 5 |
| Worker→API HTTP internal endpoint | Task 6 |
| API publishes to SQS (QUEUE_BACKEND=sqs) | Task 7 |
| traceparent as SQS message attribute | Task 7 |
| inmemory fallback preserved | Task 8 |
| Trace continuity validation | Task 10 |
| Worker crash recovery validation | Task 11 |
| Terraform SQS + DLQ + S3 | Task 12 |
| Terraform via make, not compose | Task 12 |
| Admission control deferred (observation-first) | Intentionally absent — spec decision |
| DLQ SSE realtime deferred | Intentionally absent — spec decision |

**Known gaps correctly excluded:**
- Idempotency: documented in spec, not implemented
- Admission control gate: deferred, metrics-only in this plan
- SNS: out of scope

**Type consistency check:**
- `Publisher` interface defined in `task.go`, used in `router.go` and `main.go` — consistent
- `processor.Config` fields match env vars in `main.go` — consistent
- `sseEvent` struct in `processor.go` matches the event schema expected by `events.go` — consistent
- SQS message body fields (`task_id`, `trace_id`, `traceparent`, `payload`) match `sqsMessage` struct in processor — consistent
