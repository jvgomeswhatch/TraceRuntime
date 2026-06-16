# Phase 8 — Auto-Healing: Detection & Operational Observability

## Scope

Phase 8 implements **detection, classification, and visibility** of operational anomalies. It does NOT implement automated recovery, restart, requeue, or throttling.

The watchdog is an **observer**, not an orchestrator. All healing_events represent **detected incidents**, not repair actions.

### What Phase 8 delivers

- Heartbeat tracking for worker liveness and progress
- Stale worker detection (heartbeat gap)
- Worker down detection (health check failure)
- Queue lag monitoring (SQS depth threshold)
- Task stuck detection (prolonged processing)
- DLQ monitoring (nonempty and growing)
- All detections visible in frontend via SSE and Operations Summary
- All detections persisted in PostgreSQL for audit and dashboards
- Prometheus metrics for Grafana alerting

### What Phase 8 does NOT deliver

- Automated worker restart
- Automated task requeue (processing -> pending)
- Throttling or backpressure
- Operational SLAs or definitive thresholds

These capabilities depend on measurements from Phase 8.5 (p95/p99 inference latency, queue backlog behavior, visibility timeout calibration) and will be implemented in Phase 9.

---

## Architecture

```
+---------------+    heartbeat UPSERT     +----------------+
|   Worker      | ----------------------> |   PostgreSQL   |
|  (existing)   |    every 10s            |                |
|               |                         | worker_        |
|  /health      |<--- HTTP GET -----------| heartbeats     |
+---------------+     every 15s          |                |
                                         | healing_       |
+---------------+                         | events         |
|   Watchdog    | --- query every 15s --> |                |
|  (new)        |                         +--------+-------+
|               |                                  |
|               |--- POST /internal/events -> +----+-------+
|               |    (SSE publish)            |    API     |
|  :9093/       |                             | (existing) |
|  health       |                             |            |
|  metrics      |                             | SSE broker |
+---------------+                             +----+-------+
                                                   |
                                              +----+-------+
                                              |  Frontend  |
                                              | Ops Summary|
                                              | Tabs: Tasks|
                                              | | Ops | All|
                                              +------------+
```

### Component responsibilities

| Component | Role |
|---|---|
| Worker | Publishes heartbeat UPSERT every 10s to PostgreSQL. Exposes /health for liveness. |
| Watchdog | Queries PostgreSQL and SQS every 15s. Detects anomalies. Publishes SSE events via API. Persists healing_events to PostgreSQL. Exposes Prometheus metrics. |
| API | Serves GET /api/operations/summary for frontend bootstrap. Broadcasts SSE events to frontend. |
| Frontend | Fetches initial state from /api/operations/summary. Updates incrementally via SSE. Shows Operations Summary (current state) and Operations tab (incident history). |
| PostgreSQL | System of record for heartbeats, healing events, and task state. |

### New container: watchdog

- Go service, ~64MB RAM, 0.25 CPU
- Depends on: postgres (healthy), api (healthy)
- Profile: core
- Health check: GET :9093/health
- Metrics: GET :9093/metrics (Prometheus)

---

## Data Model

### Migration: 000002_create_watchdog_tables

#### worker_heartbeats

```sql
CREATE TABLE worker_heartbeats (
    worker_id       TEXT PRIMARY KEY,
    last_seen_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    tasks_processed BIGINT NOT NULL DEFAULT 0,
    tasks_failed    BIGINT NOT NULL DEFAULT 0,
    current_task_id UUID,
    goroutines      INT NOT NULL DEFAULT 0,
    uptime_seconds  BIGINT NOT NULL DEFAULT 0
);
```

- One row per worker, updated via UPSERT every 10s.
- No history — this is a snapshot of current worker state.
- `current_task_id` is NULL when worker is idle.
- `goroutines` and `uptime_seconds` help detect restart loops and goroutine leaks.

#### healing_events

```sql
CREATE TABLE healing_events (
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

CREATE INDEX idx_healing_events_type_created
    ON healing_events (event_type, created_at DESC);
CREATE INDEX idx_healing_events_status_created
    ON healing_events (status, created_at DESC);
```

- Each row represents a detected operational condition, not a point-in-time event.
- Lifecycle: active -> resolved. `suppressed` deferred — will be added when suppression logic exists.
- `resolved_at` is separate from status for MTTR calculation (`resolved_at - created_at`). `healing_events` intentionally stores `created_at` and `resolved_at` to enable future MTTR calculations and operational reporting.
- No CHECK constraint on `event_type` — left open so new detectors can be added without migrations.
- `source` identifies who detected the condition (currently always "watchdog", extensible for future components).

---

## Detection Rules

### Conditions monitored

| Condition | Criterion | Severity | event_type |
|---|---|---|---|
| Worker stale | `NOW() - last_seen_at` > `WATCHDOG_HEARTBEAT_STALE_SECONDS` | warning | `worker.stale` |
| Worker down | HTTP GET `/health` fails `WATCHDOG_HEALTHCHECK_FAILURES` consecutive times | critical | `worker.down` |
| Queue lag high | `ApproximateNumberOfMessages` > `WATCHDOG_QUEUE_LAG_THRESHOLD` | warning | `queue.lag.high` |
| Task stuck | Task status=processing AND `NOW() - processing_started_at` > `WATCHDOG_TASK_STUCK_SECONDS` | warning | `task.stuck` |
| DLQ nonempty | DLQ `ApproximateNumberOfMessages` > 0 (fixed, not configurable) | info | `dlq.nonempty` |
| DLQ growing | DLQ depth delta > `WATCHDOG_DLQ_DELTA_THRESHOLD` since previous poll | warning | `dlq.growing` |

### Important distinctions

- **worker.stale vs worker.down**: Different failure modes. A worker can be stale (heartbeat stopped, possible deadlock) but still respond to /health. Or the reverse: /health fails but last heartbeat was 5s ago (process crashing). These are operationally distinct and must remain separate event types.
- **task.stuck** is a **warning**, not a failure verdict. The threshold (300s) is provisional and will be calibrated in Phase 8.5 with real p95/p99 inference measurements. It signals "merits attention", not "is definitely broken".
- **dlq.nonempty vs dlq.growing**: Different semantics. A DLQ with 6 old messages is not growing. Only delta-positive changes between polls trigger `dlq.growing`.

### Deduplication

The watchdog maintains an in-memory map of active conditions, keyed by `event_type + worker_id` (or `event_type + task_id` for task.stuck).

- Condition detected AND not in map -> INSERT healing_event (status=active), add to map, publish SSE
- Condition detected AND already in map -> skip (no duplicate)
- Condition resolved AND in map -> UPDATE healing_event (status=resolved, resolved_at=NOW()), remove from map, publish SSE with status=resolved
- On watchdog startup -> `SELECT * FROM healing_events WHERE status = 'active'` to reconstruct the map

PostgreSQL is the source of truth. The in-memory map is a performance optimization, not the authoritative state.

---

## Configuration

All thresholds are exposed as environment variables with prefix `WATCHDOG_*`. All values are **Phase 8 provisional** and will be recalibrated in Phase 8.5 with real operational measurements.

### Worker environment variables

| Variable | Default | Description |
|---|---|---|
| `WATCHDOG_HEARTBEAT_INTERVAL_SECONDS` | 10 | How often worker UPSERTs heartbeat to PostgreSQL |

### Watchdog environment variables

| Variable | Default | Description |
|---|---|---|
| `WATCHDOG_POLL_INTERVAL_SECONDS` | 15 | How often watchdog checks all conditions |
| `WATCHDOG_HEARTBEAT_STALE_SECONDS` | 60 | Seconds without heartbeat before worker.stale |
| `WATCHDOG_HEALTHCHECK_INTERVAL_SECONDS` | 15 | How often watchdog pings worker /health |
| `WATCHDOG_HEALTHCHECK_FAILURES` | 3 | Consecutive /health failures before worker.down |
| `WATCHDOG_QUEUE_LAG_THRESHOLD` | 50 | Queue depth above which queue.lag.high fires |
| `WATCHDOG_TASK_STUCK_SECONDS` | 300 | Seconds in processing before task.stuck fires |
| `WATCHDOG_DLQ_DELTA_THRESHOLD` | 1 | Minimum DLQ depth increase between polls to trigger dlq.growing |

Heartbeat interval and stale threshold are decoupled: heartbeat every 10s, stale after 60s. The detection logic is not implicitly dependent on the publication frequency.

`dlq.nonempty` uses a fixed threshold of `depth > 0` — it is not configurable because the condition is binary (DLQ has messages or it doesn't). `dlq.growing` uses `WATCHDOG_DLQ_DELTA_THRESHOLD` to detect growth between polls.

---

## SSE Events

### Event types published by watchdog

The watchdog publishes via `POST /internal/events` (same endpoint used by worker). No new event types for resolution — the same event_type is published with `status: "resolved"`.

```
worker.stale     — heartbeat gap exceeded threshold
worker.down      — health check failing consecutively
queue.lag.high   — queue depth above threshold
task.stuck       — task in processing beyond threshold
dlq.nonempty     — DLQ has messages
dlq.growing      — DLQ depth increased since last poll
```

### SSE event schema (operational)

```json
{
  "event_id": "uuid",
  "event_type": "worker.stale",
  "trace_id": "",
  "task_id": "",
  "timestamp": "2026-06-16T12:34:56Z",
  "source": "watchdog",
  "severity": "warning",
  "status": "active",
  "healing_event_id": "uuid",
  "details": {
    "worker_id": "worker-1",
    "last_seen_seconds_ago": 95,
    "threshold": 60
  }
}
```

- `trace_id` and `task_id` are empty unless the event is about a specific task (task.stuck includes both).
- `status` is "active" when condition is detected, "resolved" when condition disappears.
- `healing_event_id` links to the PostgreSQL record for audit.
- `details` contains context specific to the event type — threshold that was exceeded, current value, worker_id, etc.

### Resolution events

When a condition resolves, the watchdog publishes the **same event_type** with `status: "resolved"`:

```json
{
  "event_type": "worker.stale",
  "status": "resolved",
  "healing_event_id": "uuid-of-original-event",
  "details": {
    "worker_id": "worker-1",
    "duration_seconds": 136
  }
}
```

No separate `worker.recovered` or `queue.lag.normal` types. Fewer types, same information.

---

## API Endpoint

### GET /api/operations/summary

Returns current operational state. Called by frontend on page load and on SSE reconnection.

```json
{
  "workers": [
    {
      "worker_id": "worker-1",
      "status": "healthy",
      "last_seen_at": "2026-06-16T12:34:56Z",
      "last_seen_seconds_ago": 4,
      "tasks_processed": 142,
      "tasks_failed": 3,
      "current_task_id": null,
      "goroutines": 12,
      "uptime_seconds": 3600
    }
  ],
  "queue": {
    "depth": 12,
    "inflight": 1,
    "dlq_depth": 0
  },
  "active_incidents": [
    {
      "id": "uuid",
      "event_type": "queue.lag.high",
      "severity": "warning",
      "source": "watchdog",
      "worker_id": null,
      "details": {"queue_depth": 87, "threshold": 50},
      "created_at": "2026-06-16T12:33:41Z"
    }
  ],
  "summary": {
    "total_workers": 1,
    "healthy_workers": 1,
    "active_incidents_count": 0
  }
}
```

Data sources:
- `workers` -> worker_heartbeats table + watchdog health check state
- `queue` -> SQS GetQueueAttributes (cached by watchdog, refreshed every poll)
- `active_incidents` -> healing_events WHERE status = 'active'
- `summary` -> aggregation of above

Worker status derivation:
- `healthy`: last_seen_at within stale threshold AND no active worker.down event
- `stale`: last_seen_at exceeds stale threshold AND health check responding
- `down`: active worker.down healing_event exists

---

## Frontend

### Operations Summary (state card)

```
+--------------------------------------------+
|  Operations                                |
|                                            |
|  Workers    * healthy (1/1)     4s ago     |
|  Queue Lag  12 messages                    |
|  DLQ        0 messages                     |
|  Incidents  0 active                       |
+--------------------------------------------+
```

- Compact card above the EventFeed
- Worker indicator: green (healthy), yellow (stale), red (down)
- Queue lag and DLQ: numeric values
- Incidents: count of active healing_events
- "4s ago": time since last heartbeat
- Represents **current system state**, not history

### Data flow

1. Page load: `fetch GET /api/operations/summary` -> populate OpsState
2. SSE connection established -> incremental updates
3. SSE reconnect: re-fetch `/api/operations/summary` to resync

```typescript
interface OpsState {
  workers: Map<string, {
    status: 'healthy' | 'stale' | 'down';
    lastSeenAt: string;
    lastSeenSecondsAgo: number;
    tasksProcessed: number;
    tasksFailed: number;
    currentTaskId: string | null;
  }>;
  queueDepth: number;
  queueInflight: number;
  dlqDepth: number;
  activeIncidents: HealingEvent[];
}
```

### EventFeed tabs

```
[ Tasks | Operations | All ]
```

- **Tasks**: events where event_type starts with `task.` EXCEPT `task.stuck`
- **Operations**: events with source=watchdog (worker.*, queue.*, dlq.*, task.stuck)
- **All**: chronological, unfiltered

Operational events in the feed show:
- Severity icon (warning, critical, resolved)
- Event type and worker_id (if applicable)
- Timestamp
- Key details from JSONB (threshold exceeded, current value)
- Duration on resolution ("was stale for 2m16s")

### Responsibilities separation

- **Operations Summary** = current system state (derived from /api/operations/summary + SSE updates)
- **EventFeed Operations tab** = incident history (all healing_events, chronological)

These are different responsibilities and do not depend exclusively on the same data source.

---

## Prometheus Metrics (Watchdog)

```
traceruntime_watchdog_workers_total          gauge    Total registered workers
traceruntime_watchdog_workers_healthy        gauge    Workers with status healthy
traceruntime_watchdog_workers_stale          gauge    Workers with status stale
traceruntime_watchdog_workers_down           gauge    Workers with status down
traceruntime_watchdog_healing_events_total   counter  Total healing events created (by type, severity)
traceruntime_watchdog_active_incidents       gauge    Currently active incidents
traceruntime_watchdog_queue_lag              gauge    Current main queue depth
traceruntime_watchdog_dlq_depth              gauge    Current DLQ depth
traceruntime_watchdog_poll_duration_seconds  histogram  Time taken per watchdog poll cycle
```

Labels:
- `healing_events_total`: labels `event_type`, `severity`
- `poll_duration_seconds`: standard histogram buckets (10ms - 5s)

---

## Docker Compose

```yaml
watchdog:
  build:
    context: ./services/watchdog
    dockerfile: Dockerfile
  container_name: traceruntime-watchdog
  profiles: ["core", "no-ai", "full"]
  ports:
    - "9093:9093"
  environment:
    DATABASE_URL: postgres://traceruntime:traceruntime@postgres:5432/traceruntime?sslmode=disable
    WORKER_HEALTH_URL: http://worker:9091/health
    API_EVENTS_URL: http://api:8082/internal/events
    INTERNAL_TOKEN: ${INTERNAL_TOKEN:-}
    AWS_ENDPOINT_URL: http://localstack:4566
    AWS_REGION: us-east-1
    AWS_ACCESS_KEY_ID: test
    AWS_SECRET_ACCESS_KEY: test
    SQS_QUEUE_URL: http://localstack:4566/000000000000/traceruntime-tasks
    SQS_DLQ_URL: http://localstack:4566/000000000000/traceruntime-tasks-dlq
    WATCHDOG_POLL_INTERVAL_SECONDS: 15
    WATCHDOG_HEARTBEAT_STALE_SECONDS: 60
    WATCHDOG_HEALTHCHECK_INTERVAL_SECONDS: 15
    WATCHDOG_HEALTHCHECK_FAILURES: 3
    WATCHDOG_QUEUE_LAG_THRESHOLD: 50
    WATCHDOG_TASK_STUCK_SECONDS: 300
    WATCHDOG_DLQ_GROWTH_THRESHOLD: 0
    OTEL_EXPORTER_OTLP_ENDPOINT: http://otel-collector:4317
    OTEL_SERVICE_NAME: watchdog
  deploy:
    resources:
      limits:
        memory: 64M
        cpus: "0.25"
  healthcheck:
    test: ["CMD", "curl", "-f", "http://localhost:9093/health"]
    interval: 30s
    timeout: 5s
    retries: 3
  depends_on:
    migrate:
      condition: service_completed_successfully
    api:
      condition: service_healthy
  restart: unless-stopped
```

---

## Worker Changes

The existing worker requires two additions:

### 1. Heartbeat goroutine

A new goroutine started alongside the existing `pollMessages` and `pollQueueDepth`:

```go
go w.publishHeartbeat(ctx)  // UPSERT to PostgreSQL every WATCHDOG_HEARTBEAT_INTERVAL_SECONDS
```

The heartbeat includes:
- `worker_id`: from hostname or `WORKER_ID` env var
- `tasks_processed`: from existing counter
- `tasks_failed`: from existing counter
- `current_task_id`: set when processing starts, cleared when done
- `goroutines`: `runtime.NumGoroutine()`
- `uptime_seconds`: `time.Since(startTime).Seconds()`

### 2. Database function

```go
func (r *TaskRepo) UpsertHeartbeat(ctx context.Context, hb Heartbeat) error
```

Uses PostgreSQL `INSERT ... ON CONFLICT (worker_id) DO UPDATE SET ...`.

No other worker changes. The existing /health endpoint, metrics, and processing loop remain unchanged.

---

## Service Structure

```
services/watchdog/
  cmd/watchdog/main.go        -- entry point, config, startup
  internal/
    config/config.go          -- env var parsing, threshold struct
    detector/detector.go      -- detection loop, condition checking
    detector/health.go        -- HTTP health check logic
    detector/queue.go         -- SQS queue depth queries
    detector/tasks.go         -- stuck task detection
    detector/heartbeat.go     -- heartbeat staleness detection
    detector/dlq.go           -- DLQ monitoring
    db/healing.go             -- healing_events CRUD
    db/heartbeats.go          -- worker_heartbeats queries
    db/db.go                  -- pgx pool setup
    metrics/metrics.go        -- Prometheus metrics
    publisher/sse.go          -- POST /internal/events client
  go.mod
  go.sum
  Dockerfile
```

---

## Observability

### Traces

The watchdog exports spans for:
- Each poll cycle: `watchdog.poll`
- Each detection check: `watchdog.check.heartbeat`, `watchdog.check.health`, `watchdog.check.queue`, `watchdog.check.tasks`, `watchdog.check.dlq`
- Each SSE publish: `watchdog.publish`

### Logs

Structured JSON via slog. Key log events:
- `"healing event opened"` with event_type, severity, worker_id
- `"healing event resolved"` with event_type, duration, worker_id
- `"heartbeat stale"` with worker_id, last_seen_seconds_ago
- `"health check failed"` with worker_id, consecutive_failures
- `"watchdog started"` with config values
- `"active conditions loaded"` with count (on startup reconstruction)

---

## Validation Criteria

Phase 8 is complete when:

1. Watchdog container starts and passes health check
2. Worker publishes heartbeats to PostgreSQL every 10s
3. Stopping the worker eventually triggers both worker.stale and worker.down (order depends on timing)
4. Restarting the worker resolves both conditions
5. Queue lag above threshold triggers queue.lag.high; processing below threshold resolves it
6. DLQ messages trigger dlq.nonempty; growth between polls triggers dlq.growing
7. All detections appear in healing_events table with correct lifecycle (active -> resolved)
8. All detections appear as SSE events in the frontend Operations tab
9. Operations Summary shows current state accurately
10. GET /api/operations/summary returns correct data
11. Prometheus metrics are exposed and scrapeable
12. Traces visible in Tempo
13. No duplicate healing_events for sustained conditions
14. Watchdog restart reconstructs active conditions from PostgreSQL

---

## Phase Boundaries

- **Phase 8** (this spec): Detect + Classify + Make Visible. Zero automated repair.
- **Phase 8.5**: Measure real p95/p99 inference latency, calibrate thresholds, replace provisional values.
- **Phase 9**: Validate recovery under controlled failures. Automated repair actions (requeue, restart) become possible once thresholds are derived from real measurement.
