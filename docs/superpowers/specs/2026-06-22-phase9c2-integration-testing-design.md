# Phase 9C.2 — Integration Testing Design

## Objective

Create end-to-end integration tests that validate the full task lifecycle: API → SQS → Worker → AI Runtime (mock) → PostgreSQL → S3. Tests run against real local services (LocalStack, PostgreSQL), not mocks.

This fills the gap between unit tests (which mock dependencies) and chaos tests (which validate resilience). Integration tests validate that the happy path and common failure paths work correctly when all services are wired together.

## Constraints

- 14 GB RAM, no GPU, Docker Compose only
- Tests run against real LocalStack + PostgreSQL (no mocks for infrastructure)
- AI Runtime mocked at HTTP level (no Ollama in tests — too slow, too much RAM)
- Dedicated `go.mod` in `tests/integration/` to avoid polluting service modules
- Tests must be runnable locally via `make test-integration` and in CI (Phase 9C.3)

---

## Architecture

### Directory Structure

```
tests/
└── integration/
    ├── go.mod                      ← Dedicated module (imports api/worker as needed)
    ├── go.sum
    ├── main_test.go                 ← TestMain: setup/teardown, service readiness checks
    ├── helpers.go                   ← Shared utilities: HTTP client, DB queries, SQS helpers, assertions
    ├── task_lifecycle_test.go        ← Happy path: create → process → complete → S3 artifact
    ├── task_failure_test.go          ← Failure path: AI error → task failed → error recorded
    ├── queue_behavior_test.go        ← SQS: visibility timeout, traceparent propagation, message attributes
    ├── sse_events_test.go            ← SSE: event stream + reconnect behavior
    ├── idempotency_test.go           ← Duplicate message: no double processing, no duplicate artifacts
    ├── healing_events_test.go        ← Watchdog: healing events created and resolved (operational)
    └── cmd/
        └── mock-ai-runtime/
            └── main.go              ← Standalone mock AI Runtime binary (shared with CI)
```

### What Each Test Validates

#### 1. Task Lifecycle (Happy Path)

```
POST /tasks {"input": "test prompt"}
  → task created in PostgreSQL (status: pending)
  → message enqueued in SQS (with traceparent attribute)
  → worker picks up message
  → worker calls AI Runtime (mock returns canned response)
  → worker writes artifact to S3
  → worker updates task in PostgreSQL (status: completed, artifact_key set)
  → SSE event emitted for each state transition
```

**Assertions:**
- Task transitions: `pending → processing → completed`
- `artifact_key` format matches `{trace_id}/{task_id}.json`
- S3 object exists and content matches DB record:
  - `artifact.output` matches mock response
  - `artifact.task_id` matches `task.id`
  - `artifact.trace_id` matches `task.trace_id`
- `trace_id` is consistent across all records (DB, S3 key, SQS message)
- Token metrics populated (`prompt_tokens`, `completion_tokens`, `tokens_per_second`)
- Timestamps are ordered: `created_at < processing_started_at < completed_at`

#### 2. Task Failure Path

```
POST /tasks {"input": "trigger-failure"}
  → AI Runtime mock returns 503
  → worker marks task as failed
  → error_message recorded in PostgreSQL
  → message redelivered by SQS (maxReceiveCount=3)
  → after 3 failures, message routed to DLQ
```

**Assertions:**
- Task status: `failed`
- `error_message` is non-empty and descriptive
- DLQ contains the failed message (after retries exhaust)
- Worker did not crash (health endpoint still 200)

#### 3. Queue Behavior

Focused on SQS-specific behavior. DLQ routing is validated in Task Failure (test 2) — not duplicated here.

- **Message attributes:** `traceparent` propagated correctly as SQS MessageAttribute
- **Receive count tracking:** `ApproximateReceiveCount` increments correctly on redelivery
- **Visibility timeout:** Message is not redelivered while worker is processing (within VT window)
- **Queue depth metrics:** Worker reports correct queue depth via Prometheus endpoint

**Assertions:**
- `traceparent` attribute is valid W3C format (00-{trace_id}-{span_id}-{flags})
- `traceparent` trace_id matches the task's `trace_id` in PostgreSQL
- Queue depth metric matches actual SQS `ApproximateNumberOfMessages`

#### 4. SSE Events

**Test 4a — Event delivery:**

```
Connect to GET /events (SSE stream)
  → POST /tasks
  → receive task.created event
  → receive task.processing event
  → receive task.completed event
```

**Test 4b — Reconnect behavior:**

```
Connect to GET /events
  → receive events normally
  → close connection (client disconnect)
  → reconnect to GET /events
  → POST /tasks (new task)
  → receive events for new task on new connection
```

SSE reconnect is the most common SSE bug. This test validates that the broker correctly handles subscribe/unsubscribe/re-subscribe without leaking channels or dropping events.

**Assertions:**
- Events arrive in order: `task.created → task.processing → task.completed`
- Each event contains `task_id` and `trace_id`
- Event payload is valid JSON matching the event envelope contract
- After reconnect, new events are delivered on the new connection
- No leaked goroutines from first connection (observable via worker health metrics)

#### 5. Healing Events (Operational Integration)

**Note:** This test is logically separate from the core integration tests. It validates watchdog behavior end-to-end and depends on the watchdog poll interval (~15s), making it slower and potentially more fragile. Separated as "operational integration" vs "core integration."

```
Stop worker heartbeat emitter (docker pause worker briefly, or kill and don't restart)
  → watchdog detects stale heartbeat (heartbeat gap > threshold)
  → healing event created (worker.stale)
  → restart worker (docker unpause or start)
  → heartbeat resumes
  → healing event resolved
```

**Why not insert directly in PostgreSQL?** Writing stale heartbeat rows directly into the DB tests the watchdog query, not the full detection pipeline. By actually stopping the worker's heartbeat emission, we validate the complete flow: worker silence → watchdog detection → event creation → worker recovery → event resolution.

**Assertions:**
- Healing event created with correct `event_type` and `severity`
- Healing event status transitions: `active → resolved`
- `resolved_at` timestamp is set on resolution
- SSE stream receives healing event notifications

#### 6. Duplicate Message Processing (Idempotency)

```
POST /tasks {"input": "idempotency test"}
  → task created, message enqueued
  → worker processes task → completed
  → manually re-enqueue the same message body to SQS (simulating redelivery)
  → worker receives duplicate
  → worker detects already-completed task
```

**This is arguably the most valuable integration test in the project.** It directly validates the SQS redelivery scenario investigated in Phase 9A.

**Assertions:**
- Only one S3 artifact exists for the task
- Task status remains `completed` (not re-processed)
- `completion_tokens` not doubled (no duplicate inference)
- No duplicate SSE events emitted
- Worker did not crash or error on the duplicate

---

## AI Runtime Mock

**Important:** The mock cannot be an `httptest.Server` inside the test process. The worker runs as a Docker container and cannot reach `127.0.0.1:random-port` inside the test binary. The mock must be a standalone process accessible on the Docker network.

**Implementation:** Standalone Go binary at `tests/integration/cmd/mock-ai-runtime/main.go`.

```
tests/integration/cmd/mock-ai-runtime/main.go
```

This binary:
- Listens on `:8000` (same port as real AI Runtime)
- Implements: `GET /health`, `GET /ready`, `POST /infer`
- Returns canned successful responses for normal inputs
- Returns 503 for inputs containing "trigger-failure"
- Returns token metrics in every response
- Responds in < 10ms (no real inference)

Shared between local integration tests and CI (9C.3). Single source of truth for mock behavior.

**TestMain starts the mock as a subprocess:**

```go
func TestMain(m *testing.M) {
    // 1. Build and start mock-ai-runtime binary
    cmd := exec.Command("go", "run", "./cmd/mock-ai-runtime")
    cmd.Start()
    defer cmd.Process.Kill()
    // 2. Wait for mock healthy (GET /health)
    // 3. Check prerequisites: PostgreSQL, LocalStack, API, Worker reachable
    // ...
}
```

For local development, the worker's `AI_RUNTIME_URL` must point to the mock (e.g., `http://host.docker.internal:8000` on Docker Desktop, or the host IP on Linux).

---

## TestMain — Setup and Teardown

```go
func TestMain(m *testing.M) {
    // 1. Build and start mock-ai-runtime binary
    // 2. Check prerequisites: PostgreSQL, LocalStack, API, Worker reachable
    // 3. Clean database state (see cleanDB below)
    // 4. Purge SQS queues (main + DLQ)
    // 5. Run tests
    // 6. Cleanup (kill mock, clean state)
    os.Exit(m.Run())
}
```

**Database cleanup — TRUNCATE CASCADE:**

```go
func cleanDB(t *testing.T) {
    _, err := db.Exec(ctx, `
        TRUNCATE TABLE healing_events, worker_heartbeats, tasks
        RESTART IDENTITY CASCADE
    `)
    require.NoError(t, err)
}
```

Centralized in `helpers.go`. Handles FK dependencies correctly. Each test calls `cleanDB` in setup — tests are independent, no ordering dependency.

---

## Helpers

```go
// HTTP
func createTask(t *testing.T, input string) (taskID, traceID string)
func waitForTaskStatus(t *testing.T, taskID, expectedStatus string, timeout time.Duration)

// Database
func cleanDB(t *testing.T)  // TRUNCATE ... RESTART IDENTITY CASCADE
func getTask(t *testing.T, taskID string) Task
func getHealingEvents(t *testing.T, since time.Time) []HealingEvent

// SQS
func queueDepth(t *testing.T) int
func dlqDepth(t *testing.T) int
func purgeQueue(t *testing.T, queueURL string)
func sendRawMessage(t *testing.T, queueURL string, body string)  // for idempotency test

// S3
func getArtifact(t *testing.T, key string) []byte
func getArtifactJSON(t *testing.T, key string) map[string]any  // parsed content

// SSE
func connectSSE(t *testing.T) (*SSEClient, <-chan SSEEvent)  // returns client for disconnect/reconnect
func (c *SSEClient) Close()
func (c *SSEClient) Reconnect(t *testing.T) <-chan SSEEvent
```

---

## Environment Variables

Tests read from environment (with defaults for local development):

| Variable | Default | Description |
|---|---|---|
| `API_URL` | `http://localhost:8082` | API endpoint |
| `DATABASE_URL` | `postgres://traceruntime:traceruntime@localhost:5432/traceruntime?sslmode=disable` | PostgreSQL |
| `SQS_ENDPOINT` | `http://localhost:4566` | LocalStack |
| `SQS_QUEUE_URL` | `http://localhost:4566/000000000000/traceruntime-tasks` | Main queue |
| `SQS_DLQ_URL` | `http://localhost:4566/000000000000/traceruntime-tasks-dlq` | DLQ |
| `S3_ENDPOINT` | `http://localhost:4566` | S3 (LocalStack) |

---

## Makefile

```makefile
test-integration:
	@docker compose ps --format '{{.Service}}' | head -1 > /dev/null 2>&1 || \
		(echo "ERROR: services not running. Run 'make up' first." && exit 1)
	cd tests/integration && go test -v -count=1 -timeout=5m ./...
```

---

## Prerequisites

Tests require services running locally:
- `make up` (or `make chaos-up` if testing with chaos support)
- API, Worker, Watchdog, PostgreSQL, LocalStack must be healthy
- AI Runtime is NOT required (mock is used)

---

## What This Phase Does NOT Include

- Performance benchmarks (covered by loadtest in 9A)
- Chaos/resilience tests (covered by chaos runner in 9B)
- CI pipeline changes (covered by 9C.3)
- Contract validation tests (JSON schema enforcement — deferred)
- Multi-worker concurrency tests (deferred)

---

## Dependencies

### New Go module

```
tests/integration/go.mod
```

Dependencies: `pgx/v5`, `aws-sdk-go-v2`, standard library. No new external dependencies beyond what the project already uses.

---

## Test Execution Time

Target: < 2 minutes. Hard limit: 5 minutes.

The 60s target was unrealistic given that healing events depend on watchdog poll interval (~15s) and task failure requires SQS redelivery cycles.

Individual test timeouts:
- Task lifecycle: 30s
- Task failure + DLQ: 60s (waits for 3 redelivery cycles)
- Queue behavior: 20s
- SSE events + reconnect: 30s
- Idempotency: 45s
- Healing events: 60s (depends on watchdog poll interval ~15s + detection threshold)
