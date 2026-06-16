# Phase 8 — Auto-Healing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement operational anomaly detection, classification, and visibility for the TraceRuntime platform — a dedicated watchdog service that monitors worker health, queue state, and task progress, with all detections visible in the frontend dashboard.

**Architecture:** A new Go watchdog service polls PostgreSQL (heartbeats, stuck tasks) and SQS (queue depth, DLQ) every 15s. The existing worker gains a heartbeat goroutine that UPSERTs to a `worker_heartbeats` table every 10s. Detected anomalies are persisted as `healing_events` in PostgreSQL and published as SSE events via the existing API `/internal/events` endpoint. The frontend adds an Operations Summary card and tabs (Tasks | Operations | All) to the EventFeed.

**Tech Stack:** Go 1.25, pgx/v5, AWS SDK v2 (SQS), Prometheus, OpenTelemetry, Next.js, TypeScript, Tailwind, shadcn/ui

---

## File Map

### New files (watchdog service)

| File | Responsibility |
|---|---|
| `services/watchdog/cmd/watchdog/main.go` | Entry point, config loading, startup, graceful shutdown |
| `services/watchdog/internal/config/config.go` | Env var parsing into typed Config struct |
| `services/watchdog/internal/db/db.go` | pgx pool setup (same pattern as worker/api) |
| `services/watchdog/internal/db/heartbeats.go` | Query worker_heartbeats for stale detection |
| `services/watchdog/internal/db/healing.go` | healing_events CRUD (insert, resolve, list active) |
| `services/watchdog/internal/detector/detector.go` | Main detection loop, condition map, deduplication |
| `services/watchdog/internal/detector/heartbeat.go` | Heartbeat staleness check |
| `services/watchdog/internal/detector/health.go` | HTTP health check with consecutive failure tracking |
| `services/watchdog/internal/detector/queue.go` | SQS queue depth check |
| `services/watchdog/internal/detector/tasks.go` | Stuck task detection |
| `services/watchdog/internal/detector/dlq.go` | DLQ nonempty and growing detection |
| `services/watchdog/internal/metrics/metrics.go` | Prometheus metric definitions |
| `services/watchdog/internal/publisher/sse.go` | HTTP client for POST /internal/events |
| `services/watchdog/internal/telemetry/otel.go` | OpenTelemetry init (same pattern as worker) |
| `services/watchdog/go.mod` | Go module definition |
| `services/watchdog/Dockerfile` | Multi-stage Docker build |

### New files (migrations)

| File | Responsibility |
|---|---|
| `infra/database/migrations/000002_create_watchdog_tables.up.sql` | Create worker_heartbeats + healing_events tables |
| `infra/database/migrations/000002_create_watchdog_tables.down.sql` | Drop watchdog tables |

### New files (frontend)

| File | Responsibility |
|---|---|
| `frontend/components/operations-summary.tsx` | Operations Summary card (current system state) |

### Modified files

| File | Change |
|---|---|
| `services/worker/cmd/worker/main.go` | Add heartbeat goroutine, track startTime, add WORKER_ID env |
| `services/worker/internal/db/tasks.go` | Add UpsertHeartbeat function |
| `services/api/internal/http/router.go` | Add GET /api/operations/summary route |
| `services/api/internal/http/operations.go` | New handler for operations summary endpoint |
| `services/api/internal/db/tasks.go` | Add queries for stuck tasks, heartbeats, healing events |
| `docker-compose.yml` | Add watchdog service, add WATCHDOG_HEARTBEAT_INTERVAL_SECONDS to worker |
| `Makefile` | Add watchdog health check |
| `frontend/lib/types.ts` | Add operational event types |
| `frontend/components/providers/sse-provider.tsx` | Add ops state management, bootstrap fetch, reconnect refetch |
| `frontend/components/event-feed.tsx` | Add tabs (Tasks/Operations/All), operational event rendering |
| `frontend/components/dashboard-client.tsx` | Add OperationsSummary to layout |

---

## Task 1: Database Migration (watchdog tables)

**Files:**
- Create: `infra/database/migrations/000002_create_watchdog_tables.up.sql`
- Create: `infra/database/migrations/000002_create_watchdog_tables.down.sql`

- [ ] **Step 1: Create up migration**

```sql
-- infra/database/migrations/000002_create_watchdog_tables.up.sql

CREATE TABLE IF NOT EXISTS worker_heartbeats (
    worker_id       TEXT PRIMARY KEY,
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    tasks_processed BIGINT NOT NULL DEFAULT 0,
    tasks_failed    BIGINT NOT NULL DEFAULT 0,
    current_task_id UUID,
    goroutines      INT NOT NULL DEFAULT 0,
    uptime_seconds  BIGINT NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS healing_events (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    event_type  TEXT NOT NULL,
    severity    TEXT NOT NULL,
    source      TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'active',
    worker_id   TEXT,
    details     JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    resolved_at TIMESTAMPTZ,
    CONSTRAINT chk_severity CHECK (severity IN ('info', 'warning', 'critical')),
    CONSTRAINT chk_status   CHECK (status IN ('active', 'resolved'))
);

CREATE INDEX IF NOT EXISTS idx_healing_events_type_created
    ON healing_events (event_type, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_healing_events_status_created
    ON healing_events (status, created_at DESC);
```

- [ ] **Step 2: Create down migration**

```sql
-- infra/database/migrations/000002_create_watchdog_tables.down.sql

DROP TABLE IF EXISTS healing_events;
DROP TABLE IF EXISTS worker_heartbeats;
```

- [ ] **Step 3: Validate migration up**

Run: `make bootstrap` (or just the migrate container)

```bash
docker compose up -d postgres
docker compose up migrate
```

Expected: migration 000002 applies successfully. Verify:

```bash
docker exec -it traceruntime-postgres-1 psql -U traceruntime -c "\dt"
```

Expected output includes `worker_heartbeats` and `healing_events` tables.

- [ ] **Step 4: Validate migration rollback**

```bash
docker run --rm --network traceruntime -v "$(pwd)/infra/database/migrations:/migrations" migrate/migrate:v4.18.1 -path=/migrations -database "postgres://traceruntime:traceruntime@postgres:5432/traceruntime?sslmode=disable" down 1
```

Verify tables are gone:

```bash
docker exec -it traceruntime-postgres-1 psql -U traceruntime -c "\dt"
```

Expected: `worker_heartbeats` and `healing_events` no longer listed.

- [ ] **Step 5: Re-apply migration**

```bash
docker compose up migrate
```

Verify tables exist again:

```bash
docker exec -it traceruntime-postgres-1 psql -U traceruntime -c "\dt"
```

Expected: both tables present — migration is idempotent (uses `IF NOT EXISTS`).

- [ ] **Step 6: Commit**

```bash
git add infra/database/migrations/000002_create_watchdog_tables.up.sql infra/database/migrations/000002_create_watchdog_tables.down.sql
git commit -m "feat(phase8): add worker_heartbeats and healing_events migration"
```

---

## Task 2: Worker Heartbeat (UPSERT to PostgreSQL)

**Files:**
- Modify: `services/worker/internal/db/tasks.go` (add UpsertHeartbeat)
- Modify: `services/worker/cmd/worker/main.go` (add heartbeat goroutine)

- [ ] **Step 1: Add Heartbeat struct and UpsertHeartbeat to worker db**

Add to `services/worker/internal/db/tasks.go`:

```go
type Heartbeat struct {
	WorkerID       string
	TasksProcessed int64
	TasksFailed    int64
	CurrentTaskID  string // empty string = no current task
	Goroutines     int
	UptimeSeconds  int64
}

func (d *DB) UpsertHeartbeat(ctx context.Context, hb Heartbeat) error {
	var currentTaskID *string
	if hb.CurrentTaskID != "" {
		currentTaskID = &hb.CurrentTaskID
	}
	_, err := d.pool.Exec(ctx,
		`INSERT INTO worker_heartbeats (worker_id, last_seen_at, tasks_processed, tasks_failed, current_task_id, goroutines, uptime_seconds)
		 VALUES ($1, NOW(), $2, $3, $4::uuid, $5, $6)
		 ON CONFLICT (worker_id) DO UPDATE SET
		   last_seen_at = NOW(),
		   tasks_processed = $2,
		   tasks_failed = $3,
		   current_task_id = $4::uuid,
		   goroutines = $5,
		   uptime_seconds = $6`,
		hb.WorkerID, hb.TasksProcessed, hb.TasksFailed, currentTaskID, hb.Goroutines, hb.UptimeSeconds,
	)
	if err != nil {
		return fmt.Errorf("db.UpsertHeartbeat %s: %w", hb.WorkerID, err)
	}
	return nil
}
```

- [ ] **Step 2: Add heartbeat goroutine to worker main.go**

Modify `services/worker/cmd/worker/main.go`. Add imports for `"os"`, `"runtime"`, `"sync/atomic"` and `"strconv"`. Add a `startTime` variable and `heartbeatInterval` config. Add goroutine after existing goroutines:

After line 24 (imports), ensure these are present:
```go
"runtime"
"strconv"
"sync/atomic"
```

After line 70 (proc creation), add:

```go
startTime := time.Now()
workerID := os.Getenv("WORKER_ID")
if workerID == "" {
	hostname, _ := os.Hostname()
	workerID = hostname
}
heartbeatInterval := 10
if v := os.Getenv("WATCHDOG_HEARTBEAT_INTERVAL_SECONDS"); v != "" {
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		heartbeatInterval = n
	}
}
```

After line 76 (`go pollMessages(...)`), add:

```go
var tasksProcessed, tasksFailed atomic.Int64
var currentTaskID atomic.Value
currentTaskID.Store("")

go func() {
	ticker := time.NewTicker(time.Duration(heartbeatInterval) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hb := db.Heartbeat{
				WorkerID:       workerID,
				TasksProcessed: tasksProcessed.Load(),
				TasksFailed:    tasksFailed.Load(),
				CurrentTaskID:  currentTaskID.Load().(string),
				Goroutines:     runtime.NumGoroutine(),
				UptimeSeconds:  int64(time.Since(startTime).Seconds()),
			}
			if err := database.UpsertHeartbeat(ctx, hb); err != nil {
				slog.Warn("heartbeat upsert failed", "error", err)
			}
		}
	}
}()
```

Note: The `tasksProcessed` and `tasksFailed` atomic counters need to be incremented from the processor. For Phase 8, read them from the Prometheus counters or pass callback functions. The simplest approach: add a `ProcessCallback` to the processor that increments these atomics. However, to keep changes minimal, read from the existing Prometheus counters:

Replace the atomic approach with reading from Prometheus directly in the heartbeat:

```go
go func() {
	ticker := time.NewTicker(time.Duration(heartbeatInterval) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			hb := db.Heartbeat{
				WorkerID:       workerID,
				TasksProcessed: int64(metrics.GetTasksProcessed()),
				TasksFailed:    int64(metrics.GetTasksFailed()),
				CurrentTaskID:  proc.CurrentTaskID(),
				Goroutines:     runtime.NumGoroutine(),
				UptimeSeconds:  int64(time.Since(startTime).Seconds()),
			}
			if err := database.UpsertHeartbeat(ctx, hb); err != nil {
				slog.Warn("heartbeat upsert failed", "error", err)
			}
		}
	}
}()
```

- [ ] **Step 3: Add counter reader functions to metrics package**

Add to `services/worker/internal/metrics/metrics.go`:

```go
import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	dto "github.com/prometheus/client_model/go"
)

func GetTasksProcessed() float64 {
	m := &dto.Metric{}
	if err := TasksProcessed.Write(m); err != nil {
		return 0
	}
	return m.GetCounter().GetValue()
}

func GetTasksFailed() float64 {
	m := &dto.Metric{}
	if err := TasksFailed.Write(m); err != nil {
		return 0
	}
	return m.GetCounter().GetValue()
}
```

- [ ] **Step 4: Add CurrentTaskID accessor to processor**

Add to `services/worker/internal/processor/processor.go`:

Add a field to the `Processor` struct:

```go
type Processor struct {
	cfg           Config
	sqs           *sqssdk.Client
	s3            *s3.Client
	httpClient    *http.Client
	db            *db.DB
	currentTaskID atomic.Value
}
```

Add import `"sync/atomic"`.

In the `New` function, after creating the processor, initialize:

```go
func New(cfg Config, sqsClient *sqssdk.Client, s3Client *s3.Client, database *db.DB) *Processor {
	p := &Processor{
		cfg:        cfg,
		sqs:        sqsClient,
		s3:         s3Client,
		httpClient: &http.Client{Timeout: 5 * time.Second},
		db:         database,
	}
	p.currentTaskID.Store("")
	return p
}
```

At the start of `Process()`, after parsing the message (after line 84), add:

```go
p.currentTaskID.Store(msg.TaskID)
defer p.currentTaskID.Store("")
```

Add the accessor:

```go
func (p *Processor) CurrentTaskID() string {
	return p.currentTaskID.Load().(string)
}
```

- [ ] **Step 5: Add WATCHDOG_HEARTBEAT_INTERVAL_SECONDS to worker in docker-compose.yml**

In the `worker` service environment section, add:

```yaml
      - WATCHDOG_HEARTBEAT_INTERVAL_SECONDS=10
```

- [ ] **Step 6: Build and test worker heartbeat**

```bash
docker compose build worker
docker compose --profile no-ai up -d
```

Wait 15 seconds, then check:

```bash
docker exec -it traceruntime-postgres-1 psql -U traceruntime -c "SELECT * FROM worker_heartbeats;"
```

Expected: one row with the worker's hostname, `last_seen_at` within the last 10s, `goroutines > 0`, `uptime_seconds > 0`.

Wait another 10s and query again — `last_seen_at` should have advanced.

- [ ] **Step 7: Commit**

```bash
git add services/worker/internal/db/tasks.go services/worker/cmd/worker/main.go services/worker/internal/metrics/metrics.go services/worker/internal/processor/processor.go docker-compose.yml
git commit -m "feat(phase8): worker heartbeat UPSERT to PostgreSQL every 10s"
```

---

## Task 3: Watchdog Service — Infrastructure

**Files:**
- Create: `services/watchdog/go.mod`
- Create: `services/watchdog/internal/config/config.go`
- Create: `services/watchdog/internal/db/db.go`
- Create: `services/watchdog/internal/db/heartbeats.go`
- Create: `services/watchdog/internal/db/healing.go`
- Create: `services/watchdog/internal/telemetry/otel.go`
- Create: `services/watchdog/internal/publisher/sse.go`
- Create: `services/watchdog/cmd/watchdog/main.go` (skeleton with health + metrics)
- Create: `services/watchdog/internal/metrics/metrics.go`
- Create: `services/watchdog/Dockerfile`

- [ ] **Step 1: Create go.mod**

```bash
mkdir -p services/watchdog
cd services/watchdog
go mod init github.com/runtime-platform/services/watchdog
```

Then add required dependencies:

```bash
go get github.com/jackc/pgx/v5
go get github.com/aws/aws-sdk-go-v2
go get github.com/aws/aws-sdk-go-v2/config
go get github.com/aws/aws-sdk-go-v2/service/sqs
go get github.com/prometheus/client_golang
go get github.com/google/uuid
go get go.opentelemetry.io/otel
go get go.opentelemetry.io/otel/sdk
go get go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc
go get google.golang.org/grpc
```

- [ ] **Step 2: Create config.go**

```go
// services/watchdog/internal/config/config.go
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
```

- [ ] **Step 3: Create db.go**

```go
// services/watchdog/internal/db/db.go
package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

type DB struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, dsn string) (*DB, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("pgxpool.New: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("db ping: %w", err)
	}
	return &DB{pool: pool}, nil
}

func (d *DB) Close() {
	d.pool.Close()
}
```

- [ ] **Step 4: Create heartbeats.go**

```go
// services/watchdog/internal/db/heartbeats.go
package db

import (
	"context"
	"fmt"
	"time"
)

type WorkerHeartbeat struct {
	WorkerID       string
	LastSeenAt     time.Time
	TasksProcessed int64
	TasksFailed    int64
	CurrentTaskID  *string
	Goroutines     int
	UptimeSeconds  int64
}

func (d *DB) ListHeartbeats(ctx context.Context) ([]WorkerHeartbeat, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT worker_id, last_seen_at, tasks_processed, tasks_failed, current_task_id::text, goroutines, uptime_seconds
		 FROM worker_heartbeats`)
	if err != nil {
		return nil, fmt.Errorf("db.ListHeartbeats: %w", err)
	}
	defer rows.Close()

	var results []WorkerHeartbeat
	for rows.Next() {
		var hb WorkerHeartbeat
		if err := rows.Scan(&hb.WorkerID, &hb.LastSeenAt, &hb.TasksProcessed, &hb.TasksFailed, &hb.CurrentTaskID, &hb.Goroutines, &hb.UptimeSeconds); err != nil {
			return nil, fmt.Errorf("db.ListHeartbeats scan: %w", err)
		}
		results = append(results, hb)
	}
	return results, rows.Err()
}
```

- [ ] **Step 5: Create healing.go**

```go
// services/watchdog/internal/db/healing.go
package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type HealingEvent struct {
	ID         string
	EventType  string
	Severity   string
	Source     string
	Status     string
	WorkerID   string
	Details    json.RawMessage
	CreatedAt  time.Time
	ResolvedAt *time.Time
}

func (d *DB) InsertHealingEvent(ctx context.Context, eventType, severity, source, workerID string, details json.RawMessage) (string, error) {
	id := uuid.New().String()
	_, err := d.pool.Exec(ctx,
		`INSERT INTO healing_events (id, event_type, severity, source, status, worker_id, details)
		 VALUES ($1::uuid, $2, $3, $4, 'active', $5, $6)`,
		id, eventType, severity, source, workerID, details,
	)
	if err != nil {
		return "", fmt.Errorf("db.InsertHealingEvent: %w", err)
	}
	return id, nil
}

func (d *DB) ResolveHealingEvent(ctx context.Context, id string) error {
	_, err := d.pool.Exec(ctx,
		`UPDATE healing_events SET status = 'resolved', resolved_at = NOW()
		 WHERE id = $1::uuid AND status = 'active'`,
		id,
	)
	if err != nil {
		return fmt.Errorf("db.ResolveHealingEvent %s: %w", id, err)
	}
	return nil
}

func (d *DB) ListActiveHealingEvents(ctx context.Context) ([]HealingEvent, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT id, event_type, severity, source, status, COALESCE(worker_id, ''), details, created_at, resolved_at
		 FROM healing_events
		 WHERE status = 'active'
		 ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("db.ListActiveHealingEvents: %w", err)
	}
	defer rows.Close()

	var results []HealingEvent
	for rows.Next() {
		var he HealingEvent
		if err := rows.Scan(&he.ID, &he.EventType, &he.Severity, &he.Source, &he.Status, &he.WorkerID, &he.Details, &he.CreatedAt, &he.ResolvedAt); err != nil {
			return nil, fmt.Errorf("db.ListActiveHealingEvents scan: %w", err)
		}
		results = append(results, he)
	}
	return results, rows.Err()
}

func (d *DB) ListRecentHealingEvents(ctx context.Context, limit int) ([]HealingEvent, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT id, event_type, severity, source, status, COALESCE(worker_id, ''), details, created_at, resolved_at
		 FROM healing_events
		 ORDER BY created_at DESC
		 LIMIT $1`, limit)
	if err != nil {
		return nil, fmt.Errorf("db.ListRecentHealingEvents: %w", err)
	}
	defer rows.Close()

	var results []HealingEvent
	for rows.Next() {
		var he HealingEvent
		if err := rows.Scan(&he.ID, &he.EventType, &he.Severity, &he.Source, &he.Status, &he.WorkerID, &he.Details, &he.CreatedAt, &he.ResolvedAt); err != nil {
			return nil, fmt.Errorf("db.ListRecentHealingEvents scan: %w", err)
		}
		results = append(results, he)
	}
	return results, rows.Err()
}

func (d *DB) StuckTasks(ctx context.Context, stuckSeconds int) ([]StuckTask, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT id::text, trace_id, processing_started_at
		 FROM tasks
		 WHERE status = 'processing'
		   AND processing_started_at < NOW() - make_interval(secs => $1)`,
		stuckSeconds,
	)
	if err != nil {
		return nil, fmt.Errorf("db.StuckTasks: %w", err)
	}
	defer rows.Close()

	var results []StuckTask
	for rows.Next() {
		var st StuckTask
		if err := rows.Scan(&st.TaskID, &st.TraceID, &st.ProcessingStartedAt); err != nil {
			return nil, fmt.Errorf("db.StuckTasks scan: %w", err)
		}
		results = append(results, st)
	}
	return results, rows.Err()
}

type StuckTask struct {
	TaskID              string
	TraceID             string
	ProcessingStartedAt time.Time
}
```

- [ ] **Step 6: Create telemetry/otel.go**

Copy the exact pattern from `services/worker/internal/telemetry/otel.go`:

```go
// services/watchdog/internal/telemetry/otel.go
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

- [ ] **Step 7: Create metrics.go**

```go
// services/watchdog/internal/metrics/metrics.go
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	WorkersTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "traceruntime_watchdog_workers_total",
		Help: "Total registered workers.",
	})

	WorkersHealthy = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "traceruntime_watchdog_workers_healthy",
		Help: "Workers with status healthy.",
	})

	WorkersStale = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "traceruntime_watchdog_workers_stale",
		Help: "Workers with status stale.",
	})

	WorkersDown = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "traceruntime_watchdog_workers_down",
		Help: "Workers with status down.",
	})

	HealingEventsTotal = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "traceruntime_watchdog_healing_events_total",
		Help: "Total healing events created.",
	}, []string{"event_type", "severity"})

	ActiveIncidents = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "traceruntime_watchdog_active_incidents",
		Help: "Currently active incidents.",
	})

	QueueLag = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "traceruntime_watchdog_queue_lag",
		Help: "Current main queue depth.",
	})

	DLQDepth = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "traceruntime_watchdog_dlq_depth",
		Help: "Current DLQ depth.",
	})

	PollDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "traceruntime_watchdog_poll_duration_seconds",
		Help:    "Time taken per watchdog poll cycle.",
		Buckets: []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2, 5},
	})
)
```

- [ ] **Step 8: Create SSE publisher client**

```go
// services/watchdog/internal/publisher/sse.go
package publisher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
)

type SSEEvent struct {
	EventID        string          `json:"event_id"`
	EventType      string          `json:"event_type"`
	TraceID        string          `json:"trace_id"`
	TaskID         string          `json:"task_id"`
	Timestamp      string          `json:"timestamp"`
	Source         string          `json:"source"`
	Severity       string          `json:"severity,omitempty"`
	Status         string          `json:"status,omitempty"`
	HealingEventID string          `json:"healing_event_id,omitempty"`
	Details        json.RawMessage `json:"details,omitempty"`
}

type SSEPublisher struct {
	url    string
	token  string
	client *http.Client
}

func NewSSEPublisher(apiEventsURL, internalToken string) *SSEPublisher {
	return &SSEPublisher{
		url:    apiEventsURL,
		token:  internalToken,
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (p *SSEPublisher) Publish(ctx context.Context, ev SSEEvent) error {
	ev.EventID = uuid.New().String()
	ev.Timestamp = time.Now().UTC().Format(time.RFC3339)
	ev.Source = "watchdog"

	body, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal sse event: %w", err)
	}

	reqCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, p.url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build sse request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.token != "" {
		req.Header.Set("X-Internal-Token", p.token)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("sse publish: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("sse publish returned %d", resp.StatusCode)
	}
	return nil
}
```

- [ ] **Step 9: Create main.go skeleton**

```go
// services/watchdog/cmd/watchdog/main.go
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
	"github.com/runtime-platform/services/watchdog/internal/config"
	"github.com/runtime-platform/services/watchdog/internal/db"
	"github.com/runtime-platform/services/watchdog/internal/telemetry"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg := config.Load()

	shutdown, err := telemetry.Init(context.Background(), "traceruntime-watchdog")
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

	database, err := db.Open(context.Background(), cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer database.Close()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{
		Addr:         ":9093",
		Handler:      mux,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("watchdog server starting", "addr", ":9093")
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("watchdog server error", "error", err)
			os.Exit(1)
		}
	}()

	slog.Info("watchdog started",
		"poll_interval", cfg.PollIntervalSeconds,
		"heartbeat_stale", cfg.HeartbeatStaleSeconds,
		"healthcheck_failures", cfg.HealthcheckFailures,
		"queue_lag_threshold", cfg.QueueLagThreshold,
		"task_stuck_seconds", cfg.TaskStuckSeconds,
	)

	// Detection loop will be added in Task 4
	<-ctx.Done()

	slog.Info("watchdog shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		slog.Error("watchdog server shutdown error", "error", err)
	}
}
```

- [ ] **Step 10: Create Dockerfile**

```dockerfile
# services/watchdog/Dockerfile
FROM golang:1.25-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o watchdog ./cmd/watchdog

FROM alpine:3.19

RUN apk add --no-cache curl && \
    addgroup -S app && adduser -S app -G app

WORKDIR /app

COPY --from=builder /app/watchdog .

USER app

EXPOSE 9093

CMD ["./watchdog"]
```

- [ ] **Step 11: Add watchdog to docker-compose.yml**

Add the watchdog service after the worker service block in `docker-compose.yml`:

```yaml
  watchdog:
    build:
      context: ./services/watchdog
      dockerfile: Dockerfile
    container_name: traceruntime-watchdog
    profiles: [core, no-ai, full]
    ports:
      - "9093:9093"
    environment:
      - DATABASE_URL=postgres://traceruntime:traceruntime@postgres:5432/traceruntime?sslmode=disable&pool_max_conns=3
      - WORKER_HEALTH_URL=http://worker:9091/health
      - API_EVENTS_URL=http://api:8082/internal/events
      - INTERNAL_TOKEN=local-dev-token-change-in-prod
      - AWS_ENDPOINT_URL=http://localstack:4566
      - AWS_ACCESS_KEY_ID=test
      - AWS_SECRET_ACCESS_KEY=test
      - AWS_DEFAULT_REGION=us-east-1
      - SQS_QUEUE_URL=http://localstack:4566/000000000000/traceruntime-tasks
      - SQS_DLQ_URL=http://localstack:4566/000000000000/traceruntime-tasks-dlq
      - WATCHDOG_POLL_INTERVAL_SECONDS=15
      - WATCHDOG_HEARTBEAT_STALE_SECONDS=60
      - WATCHDOG_HEALTHCHECK_INTERVAL_SECONDS=15
      - WATCHDOG_HEALTHCHECK_FAILURES=3
      - WATCHDOG_QUEUE_LAG_THRESHOLD=50
      - WATCHDOG_TASK_STUCK_SECONDS=300
      - WATCHDOG_DLQ_DELTA_THRESHOLD=1
      - OTEL_EXPORTER_OTLP_ENDPOINT=otel-collector:4317
      - OTEL_SERVICE_NAME=watchdog
    networks: [traceruntime]
    depends_on:
      migrate:
        condition: service_completed_successfully
      api:
        condition: service_healthy
    healthcheck:
      test: ["CMD", "curl", "-f", "http://localhost:9093/health"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 20s
    restart: unless-stopped
    init: true
    mem_limit: 64m
    cpus: "0.25"
```

- [ ] **Step 12: Build and verify watchdog starts**

```bash
docker compose build watchdog
docker compose --profile no-ai up -d
```

Wait for health check:

```bash
curl -sf http://localhost:9093/health
```

Expected: `{"status":"ok"}`

```bash
curl -sf http://localhost:9093/metrics | head -20
```

Expected: Prometheus metrics including `traceruntime_watchdog_*`

- [ ] **Step 13: Add watchdog to Makefile health check**

In the `health` target, add:

```makefile
	@curl -sf http://localhost:9093/health > /dev/null && echo " watchdog: ok" || echo " watchdog: FAIL"
```

- [ ] **Step 14: Commit**

```bash
git add services/watchdog/ docker-compose.yml Makefile
git commit -m "feat(phase8): watchdog service infrastructure — config, db, publisher, health, metrics"
```

---

## Task 4: Watchdog Detection Engine

**Files:**
- Create: `services/watchdog/internal/detector/detector.go`
- Create: `services/watchdog/internal/detector/heartbeat.go`
- Create: `services/watchdog/internal/detector/health.go`
- Create: `services/watchdog/internal/detector/queue.go`
- Create: `services/watchdog/internal/detector/tasks.go`
- Create: `services/watchdog/internal/detector/dlq.go`
- Modify: `services/watchdog/cmd/watchdog/main.go` (start detection loop)

- [ ] **Step 1: Create heartbeat detector**

```go
// services/watchdog/internal/detector/heartbeat.go
package detector

import (
	"context"
	"encoding/json"
	"math"
	"time"

	"github.com/runtime-platform/services/watchdog/internal/db"
)

func (d *Detector) checkHeartbeats(ctx context.Context) {
	heartbeats, err := d.db.ListHeartbeats(ctx)
	if err != nil {
		d.logger.Warn("failed to list heartbeats", "error", err)
		return
	}

	d.metrics.WorkersTotal.Set(float64(len(heartbeats)))

	activeWorkers := make(map[string]bool)
	for _, hb := range heartbeats {
		activeWorkers[hb.WorkerID] = true
		secondsAgo := time.Since(hb.LastSeenAt).Seconds()
		key := conditionKey("worker.stale", hb.WorkerID)

		if secondsAgo > float64(d.cfg.HeartbeatStaleSeconds) {
			details, _ := json.Marshal(map[string]any{
				"worker_id":            hb.WorkerID,
				"last_seen_seconds_ago": int(math.Round(secondsAgo)),
				"threshold":            d.cfg.HeartbeatStaleSeconds,
			})
			d.openCondition(ctx, key, "worker.stale", "warning", hb.WorkerID, details)
		} else {
			d.resolveCondition(ctx, key)
		}
	}

	d.resolveConditionsForMissingWorkers("worker.stale", activeWorkers)
}

func (d *Detector) workerHeartbeatForSummary(ctx context.Context) []db.WorkerHeartbeat {
	heartbeats, err := d.db.ListHeartbeats(ctx)
	if err != nil {
		d.logger.Warn("failed to list heartbeats for summary", "error", err)
		return nil
	}
	return heartbeats
}
```

- [ ] **Step 2: Create health check detector**

```go
// services/watchdog/internal/detector/health.go
package detector

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type healthState struct {
	consecutiveFailures int
}

func (d *Detector) checkHealth(ctx context.Context) {
	workerID := "worker-1"
	key := conditionKey("worker.down", workerID)

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, d.cfg.WorkerHealthURL, nil)
	if err != nil {
		d.logger.Warn("failed to build health check request", "error", err)
		return
	}

	resp, err := d.healthClient.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		d.healthStates[workerID] = healthState{
			consecutiveFailures: d.healthStates[workerID].consecutiveFailures + 1,
		}
		if resp != nil {
			resp.Body.Close()
		}

		failures := d.healthStates[workerID].consecutiveFailures
		if failures >= d.cfg.HealthcheckFailures {
			details, _ := json.Marshal(map[string]any{
				"worker_id":            workerID,
				"consecutive_failures": failures,
				"threshold":            d.cfg.HealthcheckFailures,
			})
			d.openCondition(ctx, key, "worker.down", "critical", workerID, details)
		}
		return
	}
	resp.Body.Close()

	d.healthStates[workerID] = healthState{consecutiveFailures: 0}
	d.resolveCondition(ctx, key)
}
```

- [ ] **Step 3: Create queue lag detector**

```go
// services/watchdog/internal/detector/queue.go
package detector

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

func (d *Detector) checkQueueLag(ctx context.Context) {
	depth, inflight := d.getQueueAttributes(ctx, d.cfg.SQSQueueURL)
	d.lastQueueDepth = int(depth)
	d.lastQueueInflight = int(inflight)

	d.metrics.QueueLag.Set(depth)

	key := conditionKey("queue.lag.high", "")
	if int(depth) > d.cfg.QueueLagThreshold {
		details, _ := json.Marshal(map[string]any{
			"queue_depth": int(depth),
			"threshold":   d.cfg.QueueLagThreshold,
		})
		d.openCondition(ctx, key, "queue.lag.high", "warning", "", details)
	} else {
		d.resolveCondition(ctx, key)
	}
}

func (d *Detector) getQueueAttributes(ctx context.Context, queueURL string) (depth, inflight float64) {
	out, err := d.sqsClient.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(queueURL),
		AttributeNames: []sqstypes.QueueAttributeName{"ApproximateNumberOfMessages", "ApproximateNumberOfMessagesNotVisible"},
	})
	if err != nil {
		d.logger.Warn("failed to get queue attributes", "url", queueURL, "error", err)
		return 0, 0
	}
	if v, ok := out.Attributes["ApproximateNumberOfMessages"]; ok {
		fmt.Sscanf(string(v), "%f", &depth)
	}
	if v, ok := out.Attributes["ApproximateNumberOfMessagesNotVisible"]; ok {
		fmt.Sscanf(string(v), "%f", &inflight)
	}
	return depth, inflight
}
```

- [ ] **Step 4: Create stuck task detector**

```go
// services/watchdog/internal/detector/tasks.go
package detector

import (
	"context"
	"encoding/json"
	"math"
	"time"
)

func (d *Detector) checkStuckTasks(ctx context.Context) {
	stuck, err := d.db.StuckTasks(ctx, d.cfg.TaskStuckSeconds)
	if err != nil {
		d.logger.Warn("failed to check stuck tasks", "error", err)
		return
	}

	activeStuckKeys := make(map[string]bool)
	for _, t := range stuck {
		key := conditionKey("task.stuck", t.TaskID)
		activeStuckKeys[key] = true
		duration := time.Since(t.ProcessingStartedAt).Seconds()
		details, _ := json.Marshal(map[string]any{
			"task_id":                      t.TaskID,
			"trace_id":                     t.TraceID,
			"processing_duration_seconds":  int(math.Round(duration)),
			"threshold":                    d.cfg.TaskStuckSeconds,
		})
		d.openCondition(ctx, key, "task.stuck", "warning", "", details)
	}

	d.resolveConditionsNotInSet("task.stuck", activeStuckKeys)
}
```

- [ ] **Step 5: Create DLQ detector**

```go
// services/watchdog/internal/detector/dlq.go
package detector

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
)

func (d *Detector) checkDLQ(ctx context.Context) {
	out, err := d.sqsClient.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       aws.String(d.cfg.SQSDlqURL),
		AttributeNames: []sqstypes.QueueAttributeName{"ApproximateNumberOfMessages"},
	})
	if err != nil {
		d.logger.Warn("failed to get DLQ attributes", "error", err)
		return
	}

	var depth float64
	if v, ok := out.Attributes["ApproximateNumberOfMessages"]; ok {
		fmt.Sscanf(string(v), "%f", &depth)
	}

	currentDLQDepth := int(depth)
	d.metrics.DLQDepth.Set(depth)

	// dlq.nonempty — fixed threshold, depth > 0
	nonemptyKey := conditionKey("dlq.nonempty", "")
	if currentDLQDepth > 0 {
		details, _ := json.Marshal(map[string]any{
			"dlq_depth": currentDLQDepth,
		})
		d.openCondition(ctx, nonemptyKey, "dlq.nonempty", "info", "", details)
	} else {
		d.resolveCondition(ctx, nonemptyKey)
	}

	// dlq.growing — delta since last poll
	growingKey := conditionKey("dlq.growing", "")
	delta := currentDLQDepth - d.lastDLQDepth
	if d.lastDLQDepth >= 0 && delta >= d.cfg.DLQDeltaThreshold {
		details, _ := json.Marshal(map[string]any{
			"dlq_depth":      currentDLQDepth,
			"previous_depth": d.lastDLQDepth,
			"delta":          delta,
			"threshold":      d.cfg.DLQDeltaThreshold,
		})
		d.openCondition(ctx, growingKey, "dlq.growing", "warning", "", details)
	} else {
		d.resolveCondition(ctx, growingKey)
	}

	d.lastDLQDepth = currentDLQDepth
}
```

- [ ] **Step 6: Create main detector with deduplication and condition lifecycle**

```go
// services/watchdog/internal/detector/detector.go
package detector

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	sqssdk "github.com/aws/aws-sdk-go-v2/service/sqs"
	"go.opentelemetry.io/otel"

	"github.com/runtime-platform/services/watchdog/internal/config"
	"github.com/runtime-platform/services/watchdog/internal/db"
	"github.com/runtime-platform/services/watchdog/internal/metrics"
	"github.com/runtime-platform/services/watchdog/internal/publisher"
)

var tracer = otel.Tracer("traceruntime-watchdog/detector")

type activeCondition struct {
	healingEventID string
}

type Detector struct {
	cfg              config.Config
	db               *db.DB
	sqsClient        *sqssdk.Client
	publisher        *publisher.SSEPublisher
	logger           *slog.Logger
	metrics          *metrics.Metrics
	healthClient     *http.Client
	activeConditions map[string]activeCondition
	healthStates     map[string]healthState
	lastDLQDepth     int
	lastQueueDepth   int
	lastQueueInflight int
}

type Metrics = metrics

func New(cfg config.Config, database *db.DB, sqsClient *sqssdk.Client, pub *publisher.SSEPublisher) *Detector {
	return &Detector{
		cfg:              cfg,
		db:               database,
		sqsClient:        sqsClient,
		publisher:        pub,
		logger:           slog.Default(),
		metrics:          nil, // uses package-level metrics
		healthClient:     &http.Client{Timeout: 5 * time.Second},
		activeConditions: make(map[string]activeCondition),
		healthStates:     make(map[string]healthState),
		lastDLQDepth:     -1,
	}
}

func (d *Detector) LoadActiveConditions(ctx context.Context) error {
	events, err := d.db.ListActiveHealingEvents(ctx)
	if err != nil {
		return fmt.Errorf("load active conditions: %w", err)
	}
	for _, ev := range events {
		key := conditionKey(ev.EventType, ev.WorkerID)
		d.activeConditions[key] = activeCondition{healingEventID: ev.ID}
	}
	d.logger.Info("active conditions loaded", "count", len(events))
	return nil
}

func (d *Detector) Run(ctx context.Context) {
	ticker := time.NewTicker(time.Duration(d.cfg.PollIntervalSeconds) * time.Second)
	defer ticker.Stop()

	d.poll(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			d.poll(ctx)
		}
	}
}

func (d *Detector) poll(ctx context.Context) {
	pollCtx, span := tracer.Start(ctx, "watchdog.poll")
	defer span.End()

	start := time.Now()

	d.checkHeartbeats(pollCtx)
	d.checkHealth(pollCtx)
	d.checkQueueLag(pollCtx)
	d.checkStuckTasks(pollCtx)
	d.checkDLQ(pollCtx)

	d.updateMetrics()

	metrics.PollDuration.Observe(time.Since(start).Seconds())
}

func (d *Detector) updateMetrics() {
	var healthy, stale, down int
	for key := range d.activeConditions {
		switch {
		case len(key) > 12 && key[:12] == "worker.stale":
			stale++
		case len(key) > 11 && key[:11] == "worker.down":
			down++
		}
	}

	heartbeats, _ := d.db.ListHeartbeats(context.Background())
	total := len(heartbeats)
	healthy = total - stale - down
	if healthy < 0 {
		healthy = 0
	}

	metrics.WorkersTotal.Set(float64(total))
	metrics.WorkersHealthy.Set(float64(healthy))
	metrics.WorkersStale.Set(float64(stale))
	metrics.WorkersDown.Set(float64(down))
	metrics.ActiveIncidents.Set(float64(len(d.activeConditions)))
}

func (d *Detector) openCondition(ctx context.Context, key, eventType, severity, workerID string, details json.RawMessage) {
	if _, exists := d.activeConditions[key]; exists {
		return
	}

	id, err := d.db.InsertHealingEvent(ctx, eventType, severity, "watchdog", workerID, details)
	if err != nil {
		d.logger.Error("failed to insert healing event", "event_type", eventType, "error", err)
		return
	}

	d.activeConditions[key] = activeCondition{healingEventID: id}
	metrics.HealingEventsTotal.WithLabelValues(eventType, severity).Inc()

	d.logger.Info("healing event opened",
		"event_type", eventType,
		"severity", severity,
		"worker_id", workerID,
		"healing_event_id", id,
	)

	taskID := ""
	traceID := ""
	var detailMap map[string]any
	if err := json.Unmarshal(details, &detailMap); err == nil {
		if v, ok := detailMap["task_id"].(string); ok {
			taskID = v
		}
		if v, ok := detailMap["trace_id"].(string); ok {
			traceID = v
		}
	}

	if err := d.publisher.Publish(ctx, publisher.SSEEvent{
		EventType:      eventType,
		TraceID:        traceID,
		TaskID:         taskID,
		Severity:       severity,
		Status:         "active",
		HealingEventID: id,
		Details:        details,
	}); err != nil {
		d.logger.Warn("failed to publish SSE event", "event_type", eventType, "error", err)
	}
}

func (d *Detector) resolveCondition(ctx context.Context, key string) {
	cond, exists := d.activeConditions[key]
	if !exists {
		return
	}

	if err := d.db.ResolveHealingEvent(ctx, cond.healingEventID); err != nil {
		d.logger.Error("failed to resolve healing event", "id", cond.healingEventID, "error", err)
		return
	}

	delete(d.activeConditions, key)

	d.logger.Info("healing event resolved",
		"healing_event_id", cond.healingEventID,
		"key", key,
	)

	// Extract event_type from key for SSE
	eventType := key
	for i, c := range key {
		if c == ':' {
			eventType = key[:i]
			break
		}
	}

	if err := d.publisher.Publish(ctx, publisher.SSEEvent{
		EventType:      eventType,
		Status:         "resolved",
		HealingEventID: cond.healingEventID,
	}); err != nil {
		d.logger.Warn("failed to publish SSE resolve event", "error", err)
	}
}

func (d *Detector) resolveConditionsForMissingWorkers(eventType string, activeWorkers map[string]bool) {
	for key, cond := range d.activeConditions {
		et, workerID := parseConditionKey(key)
		if et != eventType {
			continue
		}
		if workerID != "" && activeWorkers[workerID] {
			continue
		}
		if err := d.db.ResolveHealingEvent(context.Background(), cond.healingEventID); err != nil {
			d.logger.Error("failed to resolve missing worker condition", "id", cond.healingEventID, "error", err)
			continue
		}
		delete(d.activeConditions, key)
	}
}

func (d *Detector) resolveConditionsNotInSet(eventType string, activeKeys map[string]bool) {
	for key, cond := range d.activeConditions {
		et, _ := parseConditionKey(key)
		if et != eventType {
			continue
		}
		if activeKeys[key] {
			continue
		}
		d.resolveCondition(context.Background(), key)
		_ = cond // avoid unused warning
	}
}

func (d *Detector) LastQueueDepth() int   { return d.lastQueueDepth }
func (d *Detector) LastQueueInflight() int { return d.lastQueueInflight }
func (d *Detector) LastDLQDepth() int      { return d.lastDLQDepth }

func conditionKey(eventType, id string) string {
	if id == "" {
		return eventType
	}
	return eventType + ":" + id
}

func parseConditionKey(key string) (eventType, id string) {
	for i, c := range key {
		if c == ':' {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}
```

- [ ] **Step 7: Wire detection loop into main.go**

Update `services/watchdog/cmd/watchdog/main.go` to add AWS config and start the detector. Add these imports:

```go
awsconfig "github.com/aws/aws-sdk-go-v2/config"
sqssdk "github.com/aws/aws-sdk-go-v2/service/sqs"
"github.com/runtime-platform/services/watchdog/internal/detector"
"github.com/runtime-platform/services/watchdog/internal/publisher"
```

After `database` setup and before `ctx, stop := ...`, add:

```go
awsCfg, err := awsconfig.LoadDefaultConfig(context.Background())
if err != nil {
	slog.Error("failed to load AWS config", "error", err)
	os.Exit(1)
}
sqsClient := sqssdk.NewFromConfig(awsCfg)

pub := publisher.NewSSEPublisher(cfg.APIEventsURL, cfg.InternalToken)

det := detector.New(cfg, database, sqsClient, pub)
if err := det.LoadActiveConditions(context.Background()); err != nil {
	slog.Warn("failed to load active conditions", "error", err)
}
```

Replace `// Detection loop will be added in Task 4` and `<-ctx.Done()` with:

```go
go det.Run(ctx)
<-ctx.Done()
```

- [ ] **Step 8: Build and test detection loop**

```bash
docker compose build watchdog
docker compose --profile no-ai up -d
```

Wait 30 seconds and check logs:

```bash
docker logs traceruntime-watchdog --tail 20
```

Expected: structured JSON logs showing poll cycles with no errors. Since the worker is healthy, no healing events should be created.

Verify no false positives:

```bash
docker exec -it traceruntime-postgres-1 psql -U traceruntime -c "SELECT * FROM healing_events;"
```

Expected: 0 rows.

- [ ] **Step 9: Test stale detection by stopping worker**

```bash
docker compose stop worker
```

Wait 75 seconds (60s stale threshold + 15s poll interval), then check:

```bash
docker exec -it traceruntime-postgres-1 psql -U traceruntime -c "SELECT event_type, severity, status, worker_id FROM healing_events;"
```

Expected: `worker.stale` and/or `worker.down` events with status `active`.

Restart worker:

```bash
docker compose --profile no-ai up -d worker
```

Wait 30 seconds:

```bash
docker exec -it traceruntime-postgres-1 psql -U traceruntime -c "SELECT event_type, status, resolved_at IS NOT NULL as resolved FROM healing_events;"
```

Expected: events now have status `resolved`.

- [ ] **Step 10: Commit**

```bash
git add services/watchdog/
git commit -m "feat(phase8): watchdog detection engine — heartbeat, health, queue, task, DLQ monitoring"
```

---

## Task 5: API Operations Summary Endpoint

**Files:**
- Create: `services/api/internal/http/operations.go`
- Modify: `services/api/internal/http/router.go`
- Modify: `services/api/internal/db/tasks.go`

- [ ] **Step 1: Add queries to API db package**

Add to `services/api/internal/db/tasks.go`:

```go
import (
	"encoding/json"
)

type WorkerSummary struct {
	WorkerID       string  `json:"worker_id"`
	LastSeenAt     string  `json:"last_seen_at"`
	LastSeenSecondsAgo int `json:"last_seen_seconds_ago"`
	TasksProcessed int64   `json:"tasks_processed"`
	TasksFailed    int64   `json:"tasks_failed"`
	CurrentTaskID  *string `json:"current_task_id"`
	Goroutines     int     `json:"goroutines"`
	UptimeSeconds  int64   `json:"uptime_seconds"`
}

func (d *DB) ListWorkerHeartbeats(ctx context.Context) ([]WorkerSummary, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT worker_id, last_seen_at, EXTRACT(EPOCH FROM (NOW() - last_seen_at))::int,
		        tasks_processed, tasks_failed, current_task_id::text, goroutines, uptime_seconds
		 FROM worker_heartbeats`)
	if err != nil {
		return nil, fmt.Errorf("db.ListWorkerHeartbeats: %w", err)
	}
	defer rows.Close()

	var results []WorkerSummary
	for rows.Next() {
		var ws WorkerSummary
		if err := rows.Scan(&ws.WorkerID, &ws.LastSeenAt, &ws.LastSeenSecondsAgo, &ws.TasksProcessed, &ws.TasksFailed, &ws.CurrentTaskID, &ws.Goroutines, &ws.UptimeSeconds); err != nil {
			return nil, fmt.Errorf("db.ListWorkerHeartbeats scan: %w", err)
		}
		results = append(results, ws)
	}
	return results, rows.Err()
}

type ActiveIncident struct {
	ID        string          `json:"id"`
	EventType string          `json:"event_type"`
	Severity  string          `json:"severity"`
	Source    string          `json:"source"`
	WorkerID  *string         `json:"worker_id"`
	Details   json.RawMessage `json:"details"`
	CreatedAt string          `json:"created_at"`
}

func (d *DB) ListActiveIncidents(ctx context.Context) ([]ActiveIncident, error) {
	rows, err := d.pool.Query(ctx,
		`SELECT id::text, event_type, severity, source, worker_id, details, created_at
		 FROM healing_events
		 WHERE status = 'active'
		 ORDER BY created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("db.ListActiveIncidents: %w", err)
	}
	defer rows.Close()

	var results []ActiveIncident
	for rows.Next() {
		var ai ActiveIncident
		if err := rows.Scan(&ai.ID, &ai.EventType, &ai.Severity, &ai.Source, &ai.WorkerID, &ai.Details, &ai.CreatedAt); err != nil {
			return nil, fmt.Errorf("db.ListActiveIncidents scan: %w", err)
		}
		results = append(results, ai)
	}
	return results, rows.Err()
}
```

- [ ] **Step 2: Create operations handler**

```go
// services/api/internal/http/operations.go
package http

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/runtime-platform/services/api/internal/db"
)

type OperationsHandler struct {
	database *db.DB
}

func NewOperationsHandler(database *db.DB) *OperationsHandler {
	return &OperationsHandler{database: database}
}

type operationsSummary struct {
	Workers         []db.WorkerSummary  `json:"workers"`
	ActiveIncidents []db.ActiveIncident `json:"active_incidents"`
	Summary         summaryStats        `json:"summary"`
}

type summaryStats struct {
	TotalWorkers         int `json:"total_workers"`
	HealthyWorkers       int `json:"healthy_workers"`
	ActiveIncidentsCount int `json:"active_incidents_count"`
}

func (h *OperationsHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	workers, err := h.database.ListWorkerHeartbeats(ctx)
	if err != nil {
		slog.Error("failed to list worker heartbeats", "error", err)
		workers = []db.WorkerSummary{}
	}

	incidents, err := h.database.ListActiveIncidents(ctx)
	if err != nil {
		slog.Error("failed to list active incidents", "error", err)
		incidents = []db.ActiveIncident{}
	}

	healthy := len(workers)
	for _, inc := range incidents {
		if inc.EventType == "worker.stale" || inc.EventType == "worker.down" {
			healthy--
		}
	}
	if healthy < 0 {
		healthy = 0
	}

	resp := operationsSummary{
		Workers:         workers,
		ActiveIncidents: incidents,
		Summary: summaryStats{
			TotalWorkers:         len(workers),
			HealthyWorkers:       healthy,
			ActiveIncidentsCount: len(incidents),
		},
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "http://localhost:3001")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("failed to encode operations summary", "error", err)
	}
}
```

- [ ] **Step 3: Register route in router.go**

In `services/api/internal/http/router.go`, add the route after the existing routes (after line 32):

```go
r.Get("/api/operations/summary", NewOperationsHandler(database).ServeHTTP)
```

- [ ] **Step 4: Build and test endpoint**

```bash
docker compose build api
docker compose --profile no-ai up -d
```

```bash
curl -s http://localhost:8082/api/operations/summary | python -m json.tool
```

Expected: JSON response with `workers` array (containing the worker heartbeat), `active_incidents` (empty array if system healthy), and `summary`.

- [ ] **Step 5: Commit**

```bash
git add services/api/internal/http/operations.go services/api/internal/http/router.go services/api/internal/db/tasks.go
git commit -m "feat(phase8): GET /api/operations/summary endpoint"
```

---

## Task 6: Frontend — Types + SSE Provider Updates

**Files:**
- Modify: `frontend/lib/types.ts`
- Modify: `frontend/components/providers/sse-provider.tsx`

- [ ] **Step 1: Update types.ts with operational event fields**

Replace the content of `frontend/lib/types.ts`:

```typescript
export interface SSEEvent {
  event_id: string;
  event_type: string;
  trace_id: string;
  traceparent?: string;
  task_id: string;
  timestamp: string;
  source: string;
  output?: string;
  model?: string;
  execution_status?: string;
  validation_status?: string;
  inference_duration_ms?: number;
  error_reason?: string;
  severity?: string;
  status?: string;
  healing_event_id?: string;
  details?: Record<string, unknown>;
}

export interface CreateTaskResponse {
  task_id: string;
  trace_id: string;
  traceparent: string;
}

export interface WorkerSummary {
  worker_id: string;
  last_seen_at: string;
  last_seen_seconds_ago: number;
  tasks_processed: number;
  tasks_failed: number;
  current_task_id: string | null;
  goroutines: number;
  uptime_seconds: number;
}

export interface ActiveIncident {
  id: string;
  event_type: string;
  severity: string;
  source: string;
  worker_id: string | null;
  details: Record<string, unknown>;
  created_at: string;
}

export interface OperationsSummary {
  workers: WorkerSummary[];
  active_incidents: ActiveIncident[];
  summary: {
    total_workers: number;
    healthy_workers: number;
    active_incidents_count: number;
  };
}
```

- [ ] **Step 2: Update SSE provider with ops state and bootstrap fetch**

Replace `frontend/components/providers/sse-provider.tsx`:

```tsx
"use client";

import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useRef,
  useState,
} from "react";
import type { SSEEvent, OperationsSummary } from "@/lib/types";

const MAX_EVENTS = 100;
const SSE_URL = "http://localhost:8082/events";
const OPS_SUMMARY_URL = "http://localhost:8082/api/operations/summary";

interface SSEContextValue {
  events: SSEEvent[];
  connected: boolean;
  reconnects: number;
  lastEventAt: Date | null;
  opsSummary: OperationsSummary | null;
}

const SSEContext = createContext<SSEContextValue>({
  events: [],
  connected: false,
  reconnects: 0,
  lastEventAt: null,
  opsSummary: null,
});

export function useSSEContext() {
  return useContext(SSEContext);
}

async function fetchOpsSummary(): Promise<OperationsSummary | null> {
  try {
    const res = await fetch(OPS_SUMMARY_URL);
    if (!res.ok) return null;
    return await res.json();
  } catch {
    return null;
  }
}

export function SSEProvider({ children }: { children: React.ReactNode }) {
  const [events, setEvents] = useState<SSEEvent[]>([]);
  const [connected, setConnected] = useState(false);
  const [reconnects, setReconnects] = useState(0);
  const [lastEventAt, setLastEventAt] = useState<Date | null>(null);
  const [opsSummary, setOpsSummary] = useState<OperationsSummary | null>(null);
  const esRef = useRef<EventSource | null>(null);

  const loadOpsSummary = useCallback(async () => {
    const summary = await fetchOpsSummary();
    if (summary) setOpsSummary(summary);
  }, []);

  const connect = useCallback(
    (isReconnect: boolean) => {
      if (esRef.current) {
        esRef.current.close();
      }

      const es = new EventSource(SSE_URL);
      esRef.current = es;

      es.onopen = () => {
        setConnected(true);
        if (isReconnect) {
          setReconnects((n) => n + 1);
          loadOpsSummary();
        }
      };

      es.onmessage = (e: MessageEvent<string>) => {
        try {
          const parsed: SSEEvent = JSON.parse(e.data);
          setLastEventAt(new Date());
          setEvents((prev) => [parsed, ...prev].slice(0, MAX_EVENTS));

          if (parsed.source === "watchdog") {
            loadOpsSummary();
          }
        } catch {
          // ignore malformed messages
        }
      };

      es.onerror = () => {
        setConnected(false);
      };
    },
    [loadOpsSummary],
  );

  useEffect(() => {
    loadOpsSummary();
    connect(false);
    return () => {
      esRef.current?.close();
      esRef.current = null;
    };
  }, [connect, loadOpsSummary]);

  return (
    <SSEContext.Provider
      value={{ events, connected, reconnects, lastEventAt, opsSummary }}
    >
      {children}
    </SSEContext.Provider>
  );
}
```

- [ ] **Step 3: Verify frontend builds**

```bash
cd frontend && npm run typecheck && npm run build
```

Expected: no type errors, build succeeds.

- [ ] **Step 4: Commit**

```bash
git add frontend/lib/types.ts frontend/components/providers/sse-provider.tsx
git commit -m "feat(phase8): frontend types and SSE provider with ops summary bootstrap"
```

---

## Task 7: Frontend — Operations Summary + EventFeed Tabs

**Files:**
- Create: `frontend/components/operations-summary.tsx`
- Modify: `frontend/components/event-feed.tsx`
- Modify: `frontend/components/dashboard-client.tsx`

- [ ] **Step 1: Create Operations Summary component**

```tsx
// frontend/components/operations-summary.tsx
"use client";

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useSSEContext } from "@/components/providers/sse-provider";

export function OperationsSummary() {
  const { opsSummary } = useSSEContext();

  if (!opsSummary) {
    return (
      <Card className="bg-zinc-900 border-zinc-700 text-zinc-100">
        <CardContent className="py-4">
          <p className="text-zinc-500 text-sm font-mono text-center">
            loading operations…
          </p>
        </CardContent>
      </Card>
    );
  }

  const { summary, workers } = opsSummary;
  const worker = workers[0];
  const lastSeenAgo = worker ? `${worker.last_seen_seconds_ago}s ago` : "—";

  const statusColor =
    summary.healthy_workers === summary.total_workers
      ? "text-green-400"
      : summary.healthy_workers > 0
        ? "text-yellow-400"
        : "text-red-400";

  const statusLabel =
    summary.healthy_workers === summary.total_workers
      ? "healthy"
      : summary.healthy_workers > 0
        ? "degraded"
        : "down";

  return (
    <Card className="bg-zinc-900 border-zinc-700 text-zinc-100">
      <CardHeader className="pb-2">
        <CardTitle className="text-sm font-mono text-zinc-300 uppercase tracking-widest">
          Operations
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-1 text-xs font-mono">
        <div className="flex justify-between">
          <span className="text-zinc-500">Workers</span>
          <span>
            <span className={statusColor}>● {statusLabel}</span>
            <span className="text-zinc-500 ml-2">
              ({summary.healthy_workers}/{summary.total_workers})
            </span>
            <span className="text-zinc-600 ml-2">{lastSeenAgo}</span>
          </span>
        </div>
        <div className="flex justify-between">
          <span className="text-zinc-500">Incidents</span>
          <span
            className={
              summary.active_incidents_count > 0
                ? "text-yellow-400"
                : "text-zinc-300"
            }
          >
            {summary.active_incidents_count} active
          </span>
        </div>
      </CardContent>
    </Card>
  );
}
```

- [ ] **Step 2: Update EventFeed with tabs**

Replace `frontend/components/event-feed.tsx`:

```tsx
"use client";

import { useState } from "react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { useSSEContext } from "@/components/providers/sse-provider";
import type { SSEEvent } from "@/lib/types";

type TabFilter = "tasks" | "operations" | "all";

function isOperationalEvent(ev: SSEEvent): boolean {
  return (
    ev.source === "watchdog" || ev.event_type === "task.stuck"
  );
}

function isTaskEvent(ev: SSEEvent): boolean {
  return (
    ev.event_type.startsWith("task.") &&
    ev.event_type !== "task.stuck" &&
    ev.source !== "watchdog"
  );
}

function filterEvents(events: SSEEvent[], tab: TabFilter): SSEEvent[] {
  switch (tab) {
    case "tasks":
      return events.filter(isTaskEvent);
    case "operations":
      return events.filter(isOperationalEvent);
    case "all":
      return events;
  }
}

function severityIcon(ev: SSEEvent): string {
  if (ev.status === "resolved") return "✓";
  if (ev.severity === "critical") return "✕";
  if (ev.severity === "warning") return "⚠";
  if (ev.severity === "info") return "ℹ";
  return "";
}

function badgeClasses(ev: SSEEvent): string {
  if (ev.source === "watchdog") {
    if (ev.status === "resolved") return "text-green-400 border-green-600";
    if (ev.severity === "critical") return "text-red-400 border-red-600";
    if (ev.severity === "warning") return "text-yellow-400 border-yellow-600";
    return "text-blue-400 border-blue-600";
  }
  return "text-emerald-400 border-emerald-600";
}

export function EventFeed() {
  const { events, connected, reconnects, lastEventAt } = useSSEContext();
  const [tab, setTab] = useState<TabFilter>("all");

  const filtered = filterEvents(events, tab);
  const statusLabel = connected ? "connected" : "disconnected";
  const statusColor = connected ? "text-green-400" : "text-red-400";

  return (
    <Card className="bg-zinc-900 border-zinc-700 text-zinc-100">
      <CardHeader className="pb-2">
        <div className="flex items-center justify-between">
          <CardTitle className="text-sm font-mono text-zinc-300 uppercase tracking-widest">
            Event Stream
          </CardTitle>
          <div className="flex items-center gap-3 text-xs font-mono">
            <span className={statusColor}>● {statusLabel}</span>
            {reconnects > 0 && (
              <span className="text-zinc-500">reconnects: {reconnects}</span>
            )}
            {lastEventAt && (
              <span className="text-zinc-500">
                last: {lastEventAt.toLocaleTimeString()}
              </span>
            )}
          </div>
        </div>
        <div className="flex gap-1 mt-2">
          {(["tasks", "operations", "all"] as TabFilter[]).map((t) => (
            <button
              key={t}
              onClick={() => setTab(t)}
              className={`px-3 py-1 text-xs font-mono rounded-md border transition-colors ${
                tab === t
                  ? "bg-zinc-700 border-zinc-600 text-zinc-100"
                  : "bg-zinc-900 border-zinc-700 text-zinc-500 hover:text-zinc-300"
              }`}
            >
              {t.charAt(0).toUpperCase() + t.slice(1)}
            </button>
          ))}
        </div>
      </CardHeader>
      <CardContent>
        {filtered.length === 0 ? (
          <p className="text-zinc-500 text-sm font-mono py-4 text-center">
            {tab === "operations"
              ? "no operational events"
              : "waiting for events…"}
          </p>
        ) : (
          <ul className="space-y-2 max-h-[480px] overflow-y-auto">
            {filtered.map((ev) => (
              <li
                key={ev.event_id}
                className="border border-zinc-700 rounded-md p-3 bg-zinc-800 text-xs font-mono"
              >
                <div className="flex items-center gap-2 mb-1">
                  {ev.source === "watchdog" && (
                    <span className="text-sm">{severityIcon(ev)}</span>
                  )}
                  <Badge
                    variant="outline"
                    className={`${badgeClasses(ev)} text-[10px] uppercase`}
                  >
                    {ev.event_type}
                  </Badge>
                  {ev.status && ev.source === "watchdog" && (
                    <Badge
                      variant="outline"
                      className={`text-[10px] uppercase ${
                        ev.status === "resolved"
                          ? "text-green-400 border-green-600"
                          : "text-yellow-400 border-yellow-600"
                      }`}
                    >
                      {ev.status}
                    </Badge>
                  )}
                  <span className="text-zinc-400">{ev.timestamp}</span>
                </div>
                {ev.task_id && (
                  <div className="text-zinc-300 truncate">
                    <span className="text-zinc-500">task_id </span>
                    {ev.task_id}
                  </div>
                )}
                {ev.trace_id && (
                  <div className="text-zinc-300 truncate">
                    <span className="text-zinc-500">trace_id </span>
                    {ev.trace_id}
                  </div>
                )}
                <div className="text-zinc-300">
                  <span className="text-zinc-500">source </span>
                  {ev.source}
                </div>
                {ev.details && ev.source === "watchdog" && (
                  <div className="mt-1 text-zinc-400">
                    {Object.entries(ev.details).map(([k, v]) => (
                      <span key={k} className="mr-3">
                        <span className="text-zinc-500">{k} </span>
                        {String(v)}
                      </span>
                    ))}
                  </div>
                )}
                {ev.execution_status && (
                  <div className="text-zinc-300">
                    <span className="text-zinc-500">execution_status </span>
                    {ev.execution_status}
                  </div>
                )}
                {ev.model && (
                  <div className="text-zinc-300">
                    <span className="text-zinc-500">model </span>
                    {ev.model}
                  </div>
                )}
                {ev.inference_duration_ms && ev.inference_duration_ms > 0 && (
                  <div className="text-zinc-300">
                    <span className="text-zinc-500">
                      inference_duration_ms{" "}
                    </span>
                    {ev.inference_duration_ms}
                  </div>
                )}
                {ev.error_reason && (
                  <div className="text-red-400">
                    <span className="text-zinc-500">error_reason </span>
                    {ev.error_reason}
                  </div>
                )}
                {ev.output && (
                  <div className="mt-2 border-t border-zinc-700 pt-2">
                    <span className="text-zinc-500 block mb-1">output</span>
                    <pre className="text-zinc-200 whitespace-pre-wrap break-words text-[11px] max-h-48 overflow-y-auto">
                      {ev.output}
                    </pre>
                  </div>
                )}
              </li>
            ))}
          </ul>
        )}
      </CardContent>
    </Card>
  );
}
```

- [ ] **Step 3: Update dashboard layout**

Replace `frontend/components/dashboard-client.tsx`:

```tsx
"use client";

import { SSEProvider } from "@/components/providers/sse-provider";
import { TaskForm } from "@/components/task-form";
import { EventFeed } from "@/components/event-feed";
import { OperationsSummary } from "@/components/operations-summary";

export function DashboardClient() {
  return (
    <SSEProvider>
      <div className="space-y-6 max-w-6xl">
        <OperationsSummary />
        <div className="grid grid-cols-1 md:grid-cols-2 gap-6">
          <TaskForm />
          <EventFeed />
        </div>
      </div>
    </SSEProvider>
  );
}
```

- [ ] **Step 4: Build and test frontend**

```bash
cd frontend && npm run typecheck && npm run build
```

Expected: no errors.

Start the dev server and verify in browser at `http://localhost:3001`:
- Operations Summary card appears at the top
- EventFeed has tabs (Tasks | Operations | All)
- Worker status shows green when healthy

- [ ] **Step 5: Commit**

```bash
git add frontend/components/operations-summary.tsx frontend/components/event-feed.tsx frontend/components/dashboard-client.tsx
git commit -m "feat(phase8): frontend Operations Summary card and EventFeed tabs"
```

---

## Task 8: End-to-End Validation

This task validates the complete Phase 8 flow. No new code — only operational validation. Organized into 4 mandatory scenarios plus infrastructure checks.

### Prerequisites

- [ ] **Step 1: Full stack up**

```bash
make bootstrap
```

Or if already bootstrapped:

```bash
make up
```

- [ ] **Step 2: Verify all services healthy**

```bash
make health
```

Expected: api: ok, worker: ok, watchdog: ok, localstack: ok

- [ ] **Step 3: Verify heartbeats in PostgreSQL**

```bash
docker exec -it traceruntime-postgres-1 psql -U traceruntime -c "SELECT worker_id, last_seen_at, tasks_processed, goroutines, uptime_seconds FROM worker_heartbeats;"
```

Expected: one row with recent `last_seen_at`.

- [ ] **Step 4: Verify operations summary endpoint**

```bash
curl -s http://localhost:8082/api/operations/summary | python -m json.tool
```

Expected: JSON with `workers` array, empty `active_incidents`, `summary.healthy_workers == 1`.

### Scenario 1: Worker parado (worker.stale + worker.down)

- [ ] **Step 5: Stop the worker**

```bash
docker compose stop worker
```

Wait 75 seconds (60s stale threshold + 15s poll interval). Check:

```bash
docker exec -it traceruntime-postgres-1 psql -U traceruntime -c "SELECT event_type, severity, status, details FROM healing_events ORDER BY created_at DESC LIMIT 5;"
```

Expected: both `worker.stale` and `worker.down` events with status `active`.

Check frontend at `http://localhost:3001`:
- Operations Summary should show yellow/red worker status
- Operations tab should show both events

### Scenario 2: Worker volta (status=resolved)

- [ ] **Step 6: Restart the worker**

```bash
docker compose --profile no-ai up -d worker
```

Wait 30 seconds. Check:

```bash
docker exec -it traceruntime-postgres-1 psql -U traceruntime -c "SELECT event_type, status, resolved_at, resolved_at - created_at as duration FROM healing_events ORDER BY created_at DESC LIMIT 5;"
```

Expected: events now have status `resolved` with `resolved_at` populated and positive duration.

Frontend: green worker status. Operations tab shows resolution events.

### Scenario 3: Queue congestionada (queue.lag.high)

- [ ] **Step 7: Flood the queue to exceed threshold**

Send 60+ tasks rapidly (threshold is 50):

```bash
for i in $(seq 1 60); do curl -s -X POST http://localhost:8082/tasks -H "Content-Type: application/json" -d "{\"input\":\"test-$i\"}" > /dev/null; done
```

Wait 15 seconds (one poll cycle). Check:

```bash
docker exec -it traceruntime-postgres-1 psql -U traceruntime -c "SELECT event_type, severity, status, details FROM healing_events WHERE event_type = 'queue.lag.high';"
```

Expected: `queue.lag.high` event with status `active`, details showing `queue_depth > 50`.

Wait for the worker to drain the queue, then check again — the event should resolve.

### Scenario 4: DLQ recebe mensagem (dlq.nonempty + dlq.growing)

- [ ] **Step 8: Force messages to DLQ**

To test DLQ detection, send a message that will fail 3 times (max receive count) and land in DLQ. The easiest way: temporarily stop the worker, send tasks, start the worker briefly (tasks will fail if AI runtime is disabled and there's an issue), or directly inject a message into the DLQ:

```bash
aws --endpoint-url=http://localhost:4566 --region=us-east-1 sqs send-message --queue-url http://localhost:4566/000000000000/traceruntime-tasks-dlq --message-body '{"task_id":"test-dlq","trace_id":"test","payload":"dlq-test"}'
```

Wait 15 seconds. Check:

```bash
docker exec -it traceruntime-postgres-1 psql -U traceruntime -c "SELECT event_type, severity, status, details FROM healing_events WHERE event_type LIKE 'dlq.%';"
```

Expected: `dlq.nonempty` (info) and possibly `dlq.growing` (warning) events with status `active`.

Clean up:

```bash
aws --endpoint-url=http://localhost:4566 --region=us-east-1 sqs purge-queue --queue-url http://localhost:4566/000000000000/traceruntime-tasks-dlq
```

Wait 15 seconds — `dlq.nonempty` should resolve.

### Infrastructure Validation

- [ ] **Step 9: Verify no duplicates**

```bash
docker exec -it traceruntime-postgres-1 psql -U traceruntime -c "SELECT event_type, status, COUNT(*) FROM healing_events GROUP BY event_type, status ORDER BY event_type;"
```

Expected: no event type has more than 1 row with status `active`. Previously resolved events may appear multiple times (from repeated stop/start cycles).

- [ ] **Step 10: Verify Prometheus metrics**

```bash
curl -s http://localhost:9093/metrics | grep traceruntime_watchdog
```

Expected: all watchdog metrics present with sensible values.

- [ ] **Step 11: Verify watchdog restart reconstructs state**

```bash
docker compose restart watchdog
```

Wait 30 seconds:

```bash
docker logs traceruntime-watchdog --tail 10
```

Expected: log entry `"active conditions loaded"` with the correct count.

- [ ] **Step 12: Verify traces in Tempo** (if observability stack is running)

Open Grafana at `http://localhost:3000`, search for traces with service name `traceruntime-watchdog`. Expected: `watchdog.poll` spans visible.
