# Phase 9B — Chaos Testing Design

## Objective

Validate the operational resilience of TraceRuntime by injecting controlled failures and verifying that the auto-healing system (watchdog) detects, reports, and recovers from each scenario within defined SLOs.

This is NOT automated testing. It is an operational validation tool — a CLI chaos runner that injects failures, observes system behavior in real time (Grafana, frontend, logs), and produces structured JSON reports.

## Constraints

- 14 GB RAM, no GPU, Docker Compose only
- Chaos runner runs outside containers (host machine), interacts via Docker API + HTTP
- No new containers added to docker-compose.yml
- Sequential scenario execution — no parallel chaos
- Each scenario: setup → inject → observe → validate → cleanup

---

## Architecture

### Components

```
cmd/chaos/main.go              ← CLI entry point (flags, scenario selection, runner invocation)

internal/chaos/
├── runner.go                   ← Orchestrator with explicit state machine (Stage enum)
├── report.go                   ← Report generation (individual + suite, JSON output)
├── scenario.go                 ← Scenario interface + Context struct
├── config.go                   ← CLI config parsing, SLO thresholds, global timeout
├── health.go                   ← Pre-flight health checks
│
├── docker/
│   └── controller.go           ← DockerController via Docker SDK: Kill, Stop, Start, Pause, Unpause, Health, WaitForHealthy, RestartCount
│
├── checker/
│   └── checker.go              ← Stateless validators: DB, Docker, HTTP, SQS queries
│
└── scenarios/
    ├── worker_crash.go          ← Chaos 1: docker kill worker
    ├── ai_failure.go            ← Chaos 2: docker stop ai-runtime
    ├── queue_flood.go           ← Chaos 3: submit 20-30 tasks rapidly
    ├── slow_inference.go        ← Chaos 4A: POST /internal/chaos/config delay
    ├── runtime_hang.go          ← Chaos 4B: docker pause ai-runtime
    └── postgres_failure.go      ← Chaos 5: docker stop postgres
```

### AI Runtime — Chaos Config Endpoint

```
ai-runtime/
├── chaos_config.py             ← ChaosConfig dataclass + thread-safe state + middleware
└── main.py                     ← Registers chaos_router only if CHAOS_ENABLED=true
```

### Docker SDK

Interaction with containers uses `github.com/docker/docker/client` — no shell execution, no `exec.Command("docker")`.

```
Chaos Runner → Docker Engine API → Container
```

No intermediate shell layer.

---

## Runner — State Machine

Each scenario transitions through explicit stages:

```go
type Stage string

const (
    StageSetup    Stage = "setup"
    StageInject   Stage = "inject"
    StageObserve  Stage = "observe"
    StageValidate Stage = "validate"
    StageCleanup  Stage = "cleanup"
)
```

This enables structured logging, tracing, stage-level timing in reports, and potential future partial re-execution.

### Cleanup Contract

Every scenario's `Cleanup` must follow this sequence — `docker.Start` alone is not recovery:

1. Restart stopped/killed containers
2. `WaitForHealthy()` — container health check passes
3. Smoke test — verify the service actually works (API responds, worker heartbeat resumes, task can be processed)
4. Confirm queue depth = 0 and no active healing events

Container healthy ≠ system functional. The smoke test after restart is mandatory.

---

## Scenario Interface

```go
type ScenarioContext struct {
    RunID         string
    ScenarioName  string
    StartTime     time.Time
    Config        Config
    Timeout       time.Duration
    Checker       *checker.Checker
    Docker        *docker.Controller
}

type Scenario interface {
    Name() string
    Setup(ctx context.Context, sc *ScenarioContext) error
    Inject(ctx context.Context, sc *ScenarioContext) error
    Observe(ctx context.Context, sc *ScenarioContext) (*ObserveResult, error)
    Validate(ctx context.Context, sc *ScenarioContext, observed *ObserveResult) (*ScenarioResult, error)
    Cleanup(ctx context.Context, sc *ScenarioContext) error
}
```

---

## Docker Controller

```go
type Controller struct {
    client *client.Client
    retry  RetryPolicy
}

type RetryPolicy struct {
    MaxAttempts int
    Backoff     time.Duration
}

type HealthStatus string

const (
    HealthHealthy   HealthStatus = "healthy"
    HealthUnhealthy HealthStatus = "unhealthy"
    HealthStarting  HealthStatus = "starting"
    HealthNone      HealthStatus = "none"
    HealthExited    HealthStatus = "exited"
)
```

Methods: `Kill`, `Stop`, `Start`, `Pause`, `Unpause`, `Health`, `WaitForHealthy`, `RestartCount`.

All operations include retry policy for Docker API transient failures (container already stopped, race conditions).

---

## Checker — Stateless Validators

Sources of truth by priority:

| Priority | Source | Validates | Latency |
|---|---|---|---|
| 1 | PostgreSQL | Task status, healing events, heartbeats | Immediate |
| 2 | Docker API | Container state, health, restart count | Immediate |
| 3 | HTTP APIs | Endpoint availability, response codes | Immediate |
| 4 | SQS | Queue depth, inflight, DLQ depth | ~1s |
| 5 | Prometheus | Counters/gauges (complementary only) | Eventual (~15s) |

Prometheus is never used for PASS/FAIL decisions.

```go
type Checker struct {
    db     *pgx.Pool
    docker *Controller
    sqs    *sqs.Client
    apiURL string
    workerURL string
    aiRuntimeURL string
}
```

### Key Methods

```go
// PostgreSQL
func (c *Checker) ActiveHealingEvents(ctx context.Context) ([]HealingEvent, error)
func (c *Checker) HealingEventsSince(ctx context.Context, since time.Time) ([]HealingEvent, error)
func (c *Checker) TaskStatus(ctx context.Context, taskID string) (string, error)
func (c *Checker) TasksInStatus(ctx context.Context, status string) (int, error)
func (c *Checker) LatestHeartbeat(ctx context.Context, workerID string) (*Heartbeat, error)

// Docker
func (c *Checker) ContainerHealth(ctx context.Context, name string) (HealthStatus, error)
func (c *Checker) ContainerRestartCount(ctx context.Context, name string) (int, error)
func (c *Checker) AllContainersHealthy(ctx context.Context, names []string) (bool, error)

// SQS
func (c *Checker) QueueDepth(ctx context.Context) (int, error)
func (c *Checker) DLQDepth(ctx context.Context) (int, error)

// HTTP
func (c *Checker) APIHealth(ctx context.Context) (int, error)
func (c *Checker) WorkerHealth(ctx context.Context) (int, error)
func (c *Checker) AIRuntimeHealth(ctx context.Context) (int, error)
```

### Polling with Timeout

```go
type WaitCondition struct {
    Name           string
    Check          func(ctx context.Context) (bool, error)
    Timeout        time.Duration
    Interval       time.Duration
    SuccessMessage string
    FailureMessage string
}

type WaitResult struct {
    Elapsed  time.Duration
    Attempts int
}

func (c *Checker) WaitFor(ctx context.Context, cond WaitCondition) (*WaitResult, error)
```

---

## Pre-flight Health Check

Before any scenario runs:

1. All required containers are `healthy` (api, worker, watchdog, postgres, localstack; ai-runtime if AI scenarios included)
2. No active healing events in database
3. Queue depth = 0 (prevents contamination between scenarios)
4. DLQ depth = 0

If preflight fails, the suite does not run. Chaos on unstable state is invalid.

---

## Scenarios

### Suite Order (least to most destructive)

1. queue-flood
2. slow-inference
3. ai-failure
4. runtime-hang
5. worker-crash
6. postgres-failure

### Per-Scenario Timeouts

Each scenario has its own timeout. If a scenario exceeds its timeout (e.g., `Observe` hangs), the runner aborts that scenario with FAIL and proceeds to the next one. This prevents a single stuck scenario from consuming the entire global timeout.

| Scenario | Timeout | Rationale |
|---|---|---|
| queue-flood | 2m | Burst submit + detection polling |
| slow-inference | 10m | 120s delay × 2 tasks + inference time + margin |
| ai-failure | 3m | Stop/start + smoke test |
| runtime-hang | 3m | Stuck detection with reduced threshold |
| worker-crash | 3m | Kill + detection + restart + recovery |
| postgres-failure | 3m | Stop + degradation check + restart + reconnect |

Global `CHAOS_TIMEOUT` (default 15m) is a hard ceiling for the entire suite. Per-scenario timeouts abort individual scenarios independently.

### Scenario 1: Queue Flood

**Hypothesis:** System remains stable under elevated backlog. Watchdog detects queue lag. No crashes or message loss.

| Phase | Action |
|---|---|
| Setup | Confirm system healthy, queue empty, 0 active healing events |
| Inject | Submit 20-30 tasks via HTTP POST /tasks (burst rate) |
| Observe | Poll queue depth + watchdog healing events (timeout: 30s for detection) |
| Validate | Assertions below |
| Cleanup | Purge queue; confirm system healthy |

**Functional SLOs:**
- Watchdog emits `queue.lag` healing event
- No container crashes (all healthy throughout)
- Worker continues processing (tasks_processed counter increments)
- Queue depth returns to 0 after cleanup purge (system can recover)

**Timing SLOs:**
- `queue.lag` detection < 30s after depth exceeds threshold

**Metrics captured:** `peak_queue_depth`, `detection_time_s`, `healing_events[]`

### Scenario 2: Slow Inference

**Hypothesis:** With 120s artificial delay, Visibility Timeout (360s) holds the message. No duplication. No spurious requeue.

| Phase | Action |
|---|---|
| Setup | Confirm system healthy; confirm chaos config reset |
| Inject | `POST /internal/chaos/config {"delay_seconds": 120}` + submit 2 tasks |
| Observe | Poll task status in DB + queue inflight metrics (timeout: 360s — delay + inference + margin) |
| Validate | Assertions below |
| Cleanup | `POST /internal/chaos/reset`; wait tasks complete or fail; confirm healthy |

**Functional SLOs:**
- Tasks complete with status `completed`
- Zero messages in DLQ
- SQS `ApproximateReceiveCount` <= 1 per message (observational — LocalStack may differ from real SQS)
- Worker does not report timeout error

**Timing SLOs:**
- Processing duration > 120s (delay was applied)

**Metrics captured:** `processing_duration_s`, `receive_count`, `dlq_depth`

### Scenario 3: AI Runtime Failure

**Hypothesis:** With ai-runtime stopped, worker does not crash. Tasks are reverted to `pending` for SQS retry. After ai-runtime restart, retried tasks complete normally — validating end-to-end resilience via the SQS retry mechanism.

| Phase | Action |
|---|---|
| Setup | Confirm system healthy; submit 1 task and wait for `completed` (smoke test) |
| Inject | `docker.Stop("ai-runtime")` + submit 2 tasks |
| Observe | Wait 15s, verify worker healthy, check tasks reverted to `pending` |
| Validate | Restart ai-runtime; wait for injected tasks to complete via SQS retry; submit 1 new task and confirm it completes |
| Cleanup | Ensure ai-runtime running and healthy; drain queue; resolve healing events |

**Functional SLOs:**
- Worker remains healthy (heartbeat active, health endpoint 200)
- Watchdog does NOT emit `worker.stale` or `worker.down` (worker is alive, only ai-runtime is down)
- Injected tasks complete via SQS retry after ai-runtime recovery (not permanently failed on first attempt)
- New post-recovery task completes normally

**Timing SLOs:**
- Recovery (ai-runtime healthy after restart) < 60s

**Metrics captured:** `pending_count`, `worker_healthy_during`, `recovery_time_s`, `inject_tasks_retried`

### Scenario 4: Runtime Hang (Container Pause)

**Hypothesis:** With ai-runtime paused (frozen process), worker detects timeout. Task marked as stuck by watchdog.

| Phase | Action |
|---|---|
| Setup | Confirm system healthy |
| Inject | Submit 1 task; wait for status `processing`; `docker.Pause("ai-runtime")` |
| Observe | Poll task status + healing events (timeout: 90s with reduced stuck threshold) |
| Validate | Assertions below |
| Cleanup | `docker.Unpause("ai-runtime")`; wait healthy; confirm system stable |

**Note:** `WATCHDOG_TASK_STUCK_SECONDS` is temporarily reduced (300s → 60s) during this scenario to avoid 5+ minute test duration. The chaos runner restarts the watchdog container with `WATCHDOG_TASK_STUCK_SECONDS=60` before inject, and restores the original value (300s) during cleanup. This is the simplest mechanism — no API needed on the watchdog, just an env var override + container restart.

**Functional SLOs:**
- Watchdog emits `task.stuck` healing event
- Worker does not crash during the hang

**Timing SLOs:**
- `task.stuck` detection within stuck threshold + poll interval margin

**Metrics captured (observational):** `stuck_detection_time_s`, `message_redelivered` (observational only — LocalStack `ApproximateReceiveCount` may not match real SQS behavior; not used for PASS/FAIL)

### Scenario 5: Worker Crash

**Hypothesis:** With worker killed, watchdog detects via stale heartbeat and health check failure. After restart, worker recovers.

| Phase | Action |
|---|---|
| Setup | Confirm system healthy; submit 1 task and wait for `processing` |
| Inject | `docker.Kill("worker", SIGKILL)` |
| Observe | Poll healing events + container health (timeout: 120s) |
| Validate | Assertions below |
| Cleanup | `docker.Start("worker")`; wait healthy; confirm heartbeat resumed |

**Functional SLOs:**
- Watchdog emits at least one critical healing event related to the worker (e.g. `worker.stale`, `worker.down`)
- After restart, worker resumes heartbeat and processes tasks
- No other containers crash during the scenario (restart_count == 0 for api, watchdog)

**Timing SLOs:**
- First critical event detection < 60s
- Recovery (worker healthy + heartbeat active) < 30s after restart

**Observed metrics (not PASS/FAIL):** Specific event types emitted (`worker.stale`, `worker.down`), detection timing per event type

**Metrics captured:** `detection_time_s`, `recovery_time_s`, `restart_count`, `healing_events[]`

### Scenario 6: PostgreSQL Failure

**Hypothesis:** With PostgreSQL stopped, API and worker degrade gracefully. No crashes. After restart, system recovers with automatic pool reconnection.

| Phase | Action |
|---|---|
| Setup | Confirm system healthy |
| Inject | `docker.Stop("postgres")` |
| Observe | Poll container health + attempt task creation via API + check worker/watchdog alive (timeout: 60s) |
| Validate | Assertions below |
| Cleanup | `docker.Start("postgres")`; wait healthy; confirm API and worker reconnect |

**Functional SLOs:**
- API returns structured error on task creation (not 500 panic, not connection reset)
- Worker process remains alive (container running, not exited)
- Watchdog process remains alive
- No container enters crash-loop: `restart_count(api) == 0`, `restart_count(worker) == 0`, `restart_count(watchdog) == 0` during postgres downtime
- `GET /health` returns 200 or structured 503 (never connection reset or crash)
- After postgres restart, next task creation and processing succeeds (pgx pool reconnects)

**Timing SLOs:**
- Pool reconnection < 30s after postgres is healthy

**Metrics captured:** `api_health_during_failure`, `worker_alive_during`, `reconnect_time_s`, `reconnection_attempts`, `restart_counts`

---

## SLO Result Model

```go
type SLOStatus string

const (
    SLOPass SLOStatus = "PASS"
    SLOWarn SLOStatus = "WARN"
    SLOFail SLOStatus = "FAIL"
)

type SLOType string

const (
    SLOFunctional SLOType = "functional"
    SLOTiming     SLOType = "timing"
)

type SLOResult struct {
    Name     string    `json:"name"`
    Type     SLOType   `json:"type"`
    Expected any       `json:"expected"`
    Actual   any       `json:"actual"`
    Status   SLOStatus `json:"status"`
}
```

### Scenario Status Logic

```
All functional PASS + all timing PASS → PASS
All functional PASS + any timing FAIL → WARN
Any functional FAIL                   → FAIL
```

---

## Report Format

### Individual Scenario

```json
{
  "name": "worker-crash",
  "status": "PASS",
  "duration_seconds": 73,
  "stages": {
    "setup":    {"duration_ms": 1200, "status": "ok"},
    "inject":   {"duration_ms": 500,  "status": "ok"},
    "observe":  {"duration_ms": 58000, "status": "ok"},
    "validate": {"duration_ms": 200,  "status": "ok"},
    "cleanup":  {"duration_ms": 12000, "status": "ok"}
  },
  "metrics": {
    "detection_time_seconds": 37,
    "recovery_time_seconds": 12,
    "restart_count": 1,
    "healing_events": ["worker.stale", "worker.down"]
  },
  "slo_results": [
    {"name": "worker.stale emitted", "type": "functional", "expected": true, "actual": true, "status": "PASS"},
    {"name": "worker.down emitted", "type": "functional", "expected": true, "actual": true, "status": "PASS"},
    {"name": "detection < 60s", "type": "timing", "expected": 60, "actual": 37, "status": "PASS"},
    {"name": "recovery < 30s", "type": "timing", "expected": 30, "actual": 12, "status": "PASS"}
  ],
  "warnings": []
}
```

### Suite Report

```json
{
  "version": "1.0",
  "run_id": "chaos-20260622-190000",
  "git_commit": "f49f312",
  "environment": "local-docker",
  "docker_compose_project": "traceruntime",
  "docker_images": {
    "api": "traceruntime-api:latest",
    "worker": "traceruntime-worker:latest",
    "watchdog": "traceruntime-watchdog:latest",
    "ai-runtime": "traceruntime-ai-runtime:latest"
  },
  "timestamp": "2026-06-22T19:00:00Z",
  "duration_seconds": 421,
  "summary": {
    "total": 6,
    "passed": 5,
    "warned": 1,
    "failed": 0
  },
  "scenarios": [...]
}
```

---

## AI Runtime — Chaos Config

### Endpoints (only if CHAOS_ENABLED=true)

Routes registered via `app.include_router(chaos_router)` — if `CHAOS_ENABLED` is not `true`, routes do not exist (404, not 403).

**POST /internal/chaos/config**
```json
{"delay_seconds": 240, "failure_rate": 0.0, "timeout_rate": 0.0}
```

**GET /internal/chaos/config**
Returns current chaos configuration.

**POST /internal/chaos/reset**
Resets all chaos parameters to zero.

### Protections

1. `CHAOS_ENABLED` env var — if not `true`, chaos router is not registered (404)
2. `INTERNAL_TOKEN` env var — validated via `X-Internal-Token` header; `403` if missing/invalid
3. Fail-fast on startup: if `CHAOS_ENABLED=true` and `INTERNAL_TOKEN` is missing or < 32 characters, the service refuses to start
4. Thread-safe state via `threading.Lock`

### Middleware

Applied only to chaos endpoints:

```python
async def chaos_middleware(request, call_next):
    token = request.headers.get("X-Internal-Token")
    if token != settings.internal_token:
        return JSONResponse(status_code=403, content={"error": "forbidden"})
    return await call_next(request)
```

---

## Docker Compose Changes

Minimal. Only `ai-runtime` service:

```yaml
ai-runtime:
  environment:
    - CHAOS_ENABLED=${CHAOS_ENABLED:-false}
    - INTERNAL_TOKEN=${INTERNAL_TOKEN:-disabled}
```

Fail-fast validation on ai-runtime startup when `CHAOS_ENABLED=true`.

No other docker-compose.yml changes.

---

## Environment Variables

### Chaos Runner (cmd/chaos)

| Variable | Default | Description |
|---|---|---|
| `API_URL` | `http://localhost:8082` | API endpoint |
| `AI_RUNTIME_URL` | `http://localhost:8001` | AI Runtime endpoint |
| `WORKER_URL` | `http://localhost:9091` | Worker health/metrics |
| `DATABASE_URL` | `postgres://traceruntime:traceruntime@localhost:5432/traceruntime?sslmode=disable` | PostgreSQL |
| `SQS_ENDPOINT` | `http://localhost:4566` | LocalStack |
| `SQS_QUEUE_URL` | `http://localhost:4566/000000000000/traceruntime-tasks` | Main queue |
| `SQS_DLQ_URL` | `http://localhost:4566/000000000000/traceruntime-tasks-dlq` | DLQ |
| `INTERNAL_TOKEN` | (required for chaos config scenarios) | Token for `/internal/chaos/*` |
| `CHAOS_OUTPUT_DIR` | `results` | Report output directory |
| `CHAOS_TIMEOUT` | `15m` | Global suite timeout |
| `CHAOS_POLL_INTERVAL` | `2s` | Default polling interval |

### AI Runtime (new)

| Variable | Default | Description |
|---|---|---|
| `CHAOS_ENABLED` | `false` | Enables `/internal/chaos/*` endpoints |
| `INTERNAL_TOKEN` | `disabled` | Auth token (min 32 chars when CHAOS_ENABLED=true) |

---

## Makefile

```makefile
chaos-build:
	docker compose --profile full build

chaos-up:
	@openssl rand -hex 32 > .chaos.token
	CHAOS_ENABLED=true INTERNAL_TOKEN=$$(cat .chaos.token) \
	docker compose --profile full up -d
	@echo "Chaos token persisted to .chaos.token"

chaos-down:
	docker compose --profile full down
	@rm -f .chaos.token

chaos-reset:
	docker compose --profile full down -v
	@rm -f .chaos.token
	make bootstrap

chaos:
	@docker compose ps --format '{{.Service}}' | head -1 > /dev/null 2>&1 || \
		(echo "ERROR: services not running. Run 'make chaos-up' first." && exit 1)
	@test -f .chaos.token || (echo "ERROR: .chaos.token not found. Run 'make chaos-up' first." && exit 1)
	INTERNAL_TOKEN=$$(cat .chaos.token) go run ./cmd/chaos \
		--output-dir=$(or $(OUTPUT_DIR),results) \
		$(if $(SCENARIO),--scenario=$(SCENARIO),) \
		$(if $(LIST),--list=$(LIST),)
```

### Usage

```bash
make chaos-build                           # build images
make chaos-up                              # start with chaos support + ephemeral token
make chaos                                 # run all scenarios
make chaos SCENARIO=worker-crash           # single scenario
make chaos LIST=worker-crash,queue-flood   # subset
cat results/chaos-*.json                   # inspect results
make chaos-down                            # stop
make chaos-reset                           # full reset (volumes + bootstrap)
```

---

## Security Checklist

- `CHAOS_ENABLED=false` by default — chaos endpoints do not exist in normal operation
- `INTERNAL_TOKEN` required with minimum 32 characters — fail-fast on startup
- Ephemeral token generated per `chaos-up` session
- Chaos router registered only when enabled (`app.include_router`, not conditional 403)
- Chaos runner operates outside containers — no code injection into services
- Docker API accessed via local socket — no network exposure
- No chaos endpoints exposed in frontend
- `chaos-up` is a separate Makefile target from `up` — normal operation never has chaos enabled
- No `exec.Command` / shell execution — all Docker interaction via SDK
- `.chaos.token` gitignored — ephemeral secret never committed

---

## Dependencies

### New Go dependency

```
github.com/docker/docker v27.x
```

Added to project go.mod. No other new dependencies — reuses existing AWS SDK and pgx.

### Report Output

```
results/
├── loadtest-*.json                  # existing (Phase 9A)
├── chaos-suite-{timestamp}.json     # suite report
├── chaos-{scenario}-{timestamp}.json  # individual scenario
```

---

## What This Phase Does NOT Include

- Automated `go test` chaos tests (deferred to 9C.3)
- CI pipeline for chaos (deferred to 9C.3)
- Frontend chaos results panel (deferred to 9B.2)
- DLQ redelivery validation scenario (separate concern)
- Multi-worker concurrency chaos (deferred)
