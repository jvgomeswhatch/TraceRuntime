# Phase 9 Remaining — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the 4 remaining Phase 9 sub-phases: Security Hardening (9C.1), Integration Testing (9C.2), Chaos Testing (9B), and CI Hardening (9C.3).

**Architecture:** Each phase is a self-contained group of tasks. Phases execute sequentially: 9C.1 → 9C.2 → 9B → 9C.3. Each phase produces working, testable software that can be validated independently before proceeding.

**Tech Stack:** Go 1.22+ (API, Worker, Chaos Runner), Python 3.11+ (AI Runtime), Next.js/TypeScript (Frontend), Docker SDK for Go, PostgreSQL (pgx), AWS SDK (SQS/S3), GitHub Actions CI.

**Specs:** All design specs at `docs/superpowers/specs/2026-06-22-phase9*.md`

---

## Phase 9C.1 — Security Hardening

### Task 1: Configurable CORS + SSE Origin Validation

**Files:**
- Modify: `services/api/internal/http/router.go`
- Modify: `services/api/internal/http/sse.go`
- Modify: `services/api/cmd/server/main.go`
- Test: `services/api/internal/http/cors_test.go` (create)

- [ ] **Step 1: Write tests for configurable CORS**

Create `services/api/internal/http/cors_test.go`:

```go
package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORS_AllowedOriginGetsHeader(t *testing.T) {
	handler := corsMiddleware([]string{"http://localhost:3001"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/health", nil)
	req.Header.Set("Origin", "http://localhost:3001")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3001" {
		t.Errorf("expected origin http://localhost:3001, got %q", got)
	}
}

func TestCORS_DisallowedOriginNoHeader(t *testing.T) {
	handler := corsMiddleware([]string{"http://localhost:3001"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/health", nil)
	req.Header.Set("Origin", "http://evil.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no CORS header for disallowed origin, got %q", got)
	}
}

func TestCORS_NoOriginNoHeader(t *testing.T) {
	handler := corsMiddleware([]string{"http://localhost:3001"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("expected no CORS header when no Origin sent, got %q", got)
	}
}

func TestCORS_SecurityHeaders(t *testing.T) {
	handler := corsMiddleware([]string{"http://localhost:3001"})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))

	req := httptest.NewRequest("GET", "/health", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	checks := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":        "DENY",
		"Referrer-Policy":        "no-referrer",
		"Content-Security-Policy": "default-src 'none'; frame-ancestors 'none'",
	}
	for header, expected := range checks {
		if got := rr.Header().Get(header); got != expected {
			t.Errorf("header %s: expected %q, got %q", header, expected, got)
		}
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd services/api && go test -run TestCORS ./internal/http/ -v`
Expected: FAIL — `corsMiddleware` currently takes no arguments.

- [ ] **Step 3: Implement configurable CORS middleware**

Replace `corsMiddleware` in `services/api/internal/http/router.go`:

```go
func corsMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
	originSet := make(map[string]bool, len(allowedOrigins))
	for _, o := range allowedOrigins {
		originSet[o] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if originSet[origin] {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-Internal-Token")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "no-referrer")
			w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
			next.ServeHTTP(w, r)
		})
	}
}

func isAllowedOrigin(origin string, allowedOrigins []string) bool {
	for _, o := range allowedOrigins {
		if o == origin {
			return true
		}
	}
	return false
}
```

Update `NewRouter` signature to accept `allowedOrigins []string`:

```go
func NewRouter(broker *event.Broker, publisher Publisher, q *queue.Queue, database *db.DB, resultsDir string, allowedOrigins []string) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware(allowedOrigins))
	r.Use(otelMiddleware)
	r.Use(RateLimitMiddleware())
	// ... routes unchanged
```

Update `services/api/cmd/server/main.go` to parse `ALLOWED_ORIGINS`:

```go
originsStr := envString("ALLOWED_ORIGINS", "http://localhost:3001")
var allowedOrigins []string
for _, o := range strings.Split(originsStr, ",") {
	if trimmed := strings.TrimSpace(o); trimmed != "" {
		allowedOrigins = append(allowedOrigins, trimmed)
	}
}

router := apihttp.NewRouter(broker, publisher, q, database, resultsDir, allowedOrigins)
```

Add `"strings"` to imports in `main.go`.

- [ ] **Step 4: Add SSE Origin validation**

Update `services/api/internal/http/sse.go` to accept and validate origins:

```go
type SSEHandler struct {
	broker         *event.Broker
	allowedOrigins []string
}

func NewSSEHandler(broker *event.Broker, allowedOrigins []string) *SSEHandler {
	return &SSEHandler{broker: broker, allowedOrigins: allowedOrigins}
}

func (h *SSEHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	origin := r.Header.Get("Origin")
	if origin != "" && !isAllowedOrigin(origin, h.allowedOrigins) {
		http.Error(w, "origin not allowed", http.StatusForbidden)
		return
	}

	_, span := sseTracer.Start(r.Context(), "sse.stream")
	defer span.End()
	// ... rest unchanged
```

Update `NewRouter` to pass `allowedOrigins` to `NewSSEHandler`:

```go
r.Get("/events", NewSSEHandler(broker, allowedOrigins).ServeHTTP)
```

- [ ] **Step 5: Write SSE origin validation test**

Add to `services/api/internal/http/cors_test.go`:

```go
func TestSSE_RejectsDisallowedOrigin(t *testing.T) {
	broker := event.NewBroker()
	handler := NewSSEHandler(broker, []string{"http://localhost:3001"})

	req := httptest.NewRequest("GET", "/events", nil)
	req.Header.Set("Origin", "http://evil.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 for disallowed origin, got %d", rr.Code)
	}
}

func TestSSE_AllowsNoOrigin(t *testing.T) {
	broker := event.NewBroker()
	handler := NewSSEHandler(broker, []string{"http://localhost:3001"})

	req := httptest.NewRequest("GET", "/events", nil)
	// No Origin header — non-browser clients (curl, etc.)
	rr := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(req.Context(), 100*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	handler.ServeHTTP(rr, req)

	// Should not be 403 — connection accepted (will close on context cancel)
	if rr.Code == http.StatusForbidden {
		t.Error("should allow requests with no Origin header")
	}
}
```

Add `"context"` and `"time"` to test imports. Add `"github.com/runtime-platform/services/api/internal/event"` to test imports.

- [ ] **Step 6: Run all tests to verify they pass**

Run: `cd services/api && go test -run "TestCORS|TestSSE" ./internal/http/ -v`
Expected: All PASS.

- [ ] **Step 7: Commit**

```bash
git add services/api/internal/http/router.go services/api/internal/http/sse.go services/api/internal/http/cors_test.go services/api/cmd/server/main.go
git commit -m "feat(9c1): configurable CORS origins, SSE origin validation, CSP header"
```

---

### Task 2: Internal Token Middleware + Rate Limit GET Endpoints

**Files:**
- Modify: `services/api/internal/http/router.go`
- Modify: `services/api/internal/http/ratelimit.go`
- Modify: `services/api/internal/http/events.go`
- Test: `services/api/internal/http/token_test.go` (create)

- [ ] **Step 1: Write tests for internal token middleware**

Create `services/api/internal/http/token_test.go`:

```go
package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTokenMiddleware_BlocksWithoutToken(t *testing.T) {
	handler := internalTokenMiddleware("secret-token-32-chars-min-length!!")(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		}),
	)

	req := httptest.NewRequest("GET", "/api/operations/summary", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 without token, got %d", rr.Code)
	}
}

func TestTokenMiddleware_AllowsWithCorrectToken(t *testing.T) {
	handler := internalTokenMiddleware("secret-token-32-chars-min-length!!")(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		}),
	)

	req := httptest.NewRequest("GET", "/api/operations/summary", nil)
	req.Header.Set("X-Internal-Token", "secret-token-32-chars-min-length!!")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 with correct token, got %d", rr.Code)
	}
}

func TestTokenMiddleware_AllowsWhenTokenEmpty(t *testing.T) {
	handler := internalTokenMiddleware("")(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		}),
	)

	req := httptest.NewRequest("GET", "/api/operations/summary", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200 with empty token config (dev mode), got %d", rr.Code)
	}
}

func TestTokenMiddleware_BlocksWithWrongToken(t *testing.T) {
	handler := internalTokenMiddleware("correct-token-min-32-characters!!")(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(200)
		}),
	)

	req := httptest.NewRequest("GET", "/api/operations/summary", nil)
	req.Header.Set("X-Internal-Token", "wrong-token")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Errorf("expected 403 with wrong token, got %d", rr.Code)
	}
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `cd services/api && go test -run TestTokenMiddleware ./internal/http/ -v`
Expected: FAIL — `internalTokenMiddleware` not defined.

- [ ] **Step 3: Implement internal token middleware**

Add to `services/api/internal/http/router.go`:

```go
func internalTokenMiddleware(token string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if token != "" && r.Header.Get("X-Internal-Token") != token {
				jsonError(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

Update `NewRouter` to accept `internalToken string` and apply middleware to protected routes:

```go
func NewRouter(broker *event.Broker, publisher Publisher, q *queue.Queue, database *db.DB, resultsDir string, allowedOrigins []string, internalToken string) http.Handler {
	r := chi.NewRouter()

	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware(allowedOrigins))
	r.Use(otelMiddleware)
	r.Use(RateLimitMiddleware())

	r.Get("/health", Health)
	r.Get("/metrics", NewMetricsHandler(broker, q).ServeHTTP)
	r.Get("/events", NewSSEHandler(broker, allowedOrigins).ServeHTTP)
	r.Post("/tasks", NewTaskHandler(broker, publisher, database).ServeHTTP)
	r.Options("/tasks", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	tokenMw := internalTokenMiddleware(internalToken)
	r.Post("/internal/events", tokenMw(NewEventsHandler(broker)).ServeHTTP)
	r.Get("/api/events/recent", tokenMw(NewRecentEventsHandler(database)).ServeHTTP)
	r.Get("/api/operations/summary", tokenMw(NewOperationsSummaryHandler(database)).ServeHTTP)
	r.Get("/api/capacity/latest", tokenMw(NewCapacityHandler(resultsDir)).ServeHTTP)

	return r
}
```

- [ ] **Step 4: Refactor events.go — remove inline token check**

Update `services/api/internal/http/events.go` — remove the manual token check since it's now handled by middleware:

```go
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

	if !json.Valid(body) {
		slog.Warn("invalid json in /internal/events body")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	h.broker.Publish(body)
	w.WriteHeader(http.StatusAccepted)
}
```

Remove `"os"` import from events.go.

- [ ] **Step 5: Update main.go to pass internalToken**

In `services/api/cmd/server/main.go`:

```go
internalToken := envString("INTERNAL_TOKEN", "")
if internalToken == "" {
	slog.Warn("INTERNAL_TOKEN not set — operational endpoints accessible without auth (dev mode)")
}

router := apihttp.NewRouter(broker, publisher, q, database, resultsDir, allowedOrigins, internalToken)
```

- [ ] **Step 6: Extend rate limiting to GET endpoints**

Update `services/api/internal/http/ratelimit.go`:

```go
func RateLimitMiddleware() func(http.Handler) http.Handler {
	rps := envInt("RATE_LIMIT_RPS", 10)
	burst := envInt("RATE_LIMIT_BURST", 20)
	rl := newRateLimiter(rps, burst)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			path := r.URL.Path
			if path == "/health" || path == "/metrics" {
				next.ServeHTTP(w, r)
				return
			}

			ip := r.RemoteAddr
			if !rl.allow(ip) {
				w.Header().Set("Retry-After", "1")
				jsonError(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
```

- [ ] **Step 7: Run all tests**

Run: `cd services/api && go test ./... -v`
Expected: All PASS. Existing tests may need `allowedOrigins` and `internalToken` params added to `NewRouter` calls in test files.

- [ ] **Step 8: Fix any broken tests**

If existing tests call `NewRouter` without the new params, update them:

In `services/api/internal/http/health_test.go`, `task_test.go`, etc., update `NewRouter` calls to include `[]string{"http://localhost:3001"}` and `""` (empty token for test compat).

- [ ] **Step 9: Commit**

```bash
git add services/api/internal/http/router.go services/api/internal/http/events.go services/api/internal/http/ratelimit.go services/api/internal/http/token_test.go services/api/cmd/server/main.go
git commit -m "feat(9c1): internal token middleware, rate limit GET endpoints, refactor events auth"
```

---

### Task 3: Docker Compose + Frontend Token Integration

**Files:**
- Modify: `docker-compose.yml`
- Modify: `frontend/components/providers/sse-provider.tsx`
- Modify: `frontend/components/capacity-report.tsx`
- Modify: `frontend/components/kpi-cards.tsx`

- [ ] **Step 1: Update docker-compose.yml**

Update `INTERNAL_TOKEN` defaults and add `ALLOWED_ORIGINS` to api service:

```yaml
# In api service environment:
- ALLOWED_ORIGINS=${ALLOWED_ORIGINS:-http://localhost:3001}
- INTERNAL_TOKEN=${INTERNAL_TOKEN:-}
```

```yaml
# In worker service environment:
- INTERNAL_TOKEN=${INTERNAL_TOKEN:-}
```

```yaml
# In watchdog service environment:
- INTERNAL_TOKEN=${INTERNAL_TOKEN:-}
```

```yaml
# In frontend service environment, add:
- NEXT_PUBLIC_INTERNAL_TOKEN=${INTERNAL_TOKEN:-}
```

- [ ] **Step 2: Update frontend API calls to include token header**

Create a helper function or update each fetch call. In `frontend/components/providers/sse-provider.tsx`:

```typescript
const headers: HeadersInit = {};
const token = process.env.NEXT_PUBLIC_INTERNAL_TOKEN;
if (token) {
  headers["X-Internal-Token"] = token;
}
```

Apply to all `fetch` calls for `/api/operations/summary`, `/api/events/recent`, `/api/capacity/latest`.

In `frontend/components/capacity-report.tsx` and `frontend/components/kpi-cards.tsx`, add the token header to the fetch options:

```typescript
const token = process.env.NEXT_PUBLIC_INTERNAL_TOKEN;
const headers: HeadersInit = token ? { "X-Internal-Token": token } : {};
const res = await fetch("http://localhost:8082/api/capacity/latest", { headers });
```

- [ ] **Step 3: Add .chaos.token to .gitignore**

Add to `.gitignore`:

```
# Chaos testing
.chaos.token
```

- [ ] **Step 4: Verify locally**

Run: `make up`
Verify:
- Frontend loads without errors (no INTERNAL_TOKEN = dev mode, no auth required)
- API responds to all endpoints
- SSE stream connects from browser

Run: `INTERNAL_TOKEN=test-token-at-least-32-characters-long!! make up`
Verify:
- Frontend includes token in API calls
- `/api/operations/summary` returns 403 without token via curl
- `/api/operations/summary` returns 200 with correct token header via curl

- [ ] **Step 5: Commit**

```bash
git add docker-compose.yml frontend/components/providers/sse-provider.tsx frontend/components/capacity-report.tsx frontend/components/kpi-cards.tsx .gitignore
git commit -m "feat(9c1): docker-compose token defaults, frontend token integration, gitignore chaos"
```

---

**STOP — Phase 9C.1 complete. Wait for user validation before proceeding to 9C.2.**

---

## Phase 9C.2 — Integration Testing

### Task 4: Mock AI Runtime Binary + Test Module Setup

**Files:**
- Create: `tests/integration/go.mod`
- Create: `tests/integration/go.sum`
- Create: `tests/integration/cmd/mock-ai-runtime/main.go`
- Create: `tests/integration/main_test.go`
- Create: `tests/integration/helpers.go`

- [ ] **Step 1: Create Go module**

```bash
mkdir -p tests/integration/cmd/mock-ai-runtime
cd tests/integration && go mod init github.com/runtime-platform/tests/integration
```

Add dependencies:

```bash
cd tests/integration && go get github.com/jackc/pgx/v5
cd tests/integration && go get github.com/aws/aws-sdk-go-v2/config
cd tests/integration && go get github.com/aws/aws-sdk-go-v2/service/sqs
cd tests/integration && go get github.com/aws/aws-sdk-go-v2/service/s3
```

- [ ] **Step 2: Create mock AI Runtime binary**

Create `tests/integration/cmd/mock-ai-runtime/main.go`:

```go
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
)

type inferRequest struct {
	TaskID        string `json:"task_id"`
	Input         string `json:"input"`
	DeadlineUnixMs int64 `json:"deadline_unix_ms"`
}

type inferResponse struct {
	Output             string         `json:"output"`
	ExecutionStatus    string         `json:"execution_status"`
	InferenceDurationMs int           `json:"inference_duration_ms"`
	PromptTokens       int            `json:"prompt_tokens"`
	CompletionTokens   int            `json:"completion_tokens"`
	TokensPerSecond    float64        `json:"tokens_per_second"`
	ExecutionProfile   map[string]any `json:"execution_profile"`
}

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	mux.HandleFunc("/ready", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{"status": "ok", "ollama": "mocked"})
	})

	mux.HandleFunc("/infer", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		var req inferRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": "invalid request"})
			return
		}

		if strings.Contains(req.Input, "trigger-failure") {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(inferResponse{
				Output:          "",
				ExecutionStatus: "failed",
			})
			return
		}

		time.Sleep(50 * time.Millisecond)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(inferResponse{
			Output:              fmt.Sprintf("mock response for: %s", req.Input),
			ExecutionStatus:     "completed",
			InferenceDurationMs: 50,
			PromptTokens:        10,
			CompletionTokens:    20,
			TokensPerSecond:     400.0,
			ExecutionProfile:    map[string]any{"model": "mock"},
		})
	})

	log.Println("mock-ai-runtime listening on :8000")
	log.Fatal(http.ListenAndServe(":8000", mux))
}
```

- [ ] **Step 3: Verify mock binary builds and runs**

```bash
cd tests/integration && go build ./cmd/mock-ai-runtime/
./mock-ai-runtime &
curl http://localhost:8000/health
# Expected: {"status":"ok"}
curl -X POST http://localhost:8000/infer -d '{"task_id":"test","input":"hello"}'
# Expected: {"output":"mock response for: hello","execution_status":"completed",...}
kill %1
```

- [ ] **Step 4: Create helpers.go**

Create `tests/integration/helpers.go`:

```go
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	apiURL     string
	dbPool     *pgxpool.Pool
	sqsClient  *sqs.Client
	s3Client   *s3.Client
	queueURL   string
	dlqURL     string
)

func createTask(t *testing.T, input string) (taskID, traceID string) {
	t.Helper()
	body := fmt.Sprintf(`{"input":%q}`, input)
	resp, err := http.Post(apiURL+"/tasks", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("failed to create task: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("unexpected status %d: %s", resp.StatusCode, string(b))
	}
	var result struct {
		TaskID  string `json:"task_id"`
		TraceID string `json:"trace_id"`
	}
	json.NewDecoder(resp.Body).Decode(&result)
	return result.TaskID, result.TraceID
}

func waitForTaskStatus(t *testing.T, taskID, expected string, timeout time.Duration) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	for {
		var status string
		err := dbPool.QueryRow(ctx, "SELECT status FROM tasks WHERE id = $1", taskID).Scan(&status)
		if err == nil && status == expected {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("task %s did not reach status %q within %v (last: %q)", taskID, expected, timeout, status)
		case <-time.After(500 * time.Millisecond):
		}
	}
}

type taskRecord struct {
	ID                  string
	TraceID             string
	Status              string
	ArtifactKey         *string
	ErrorMessage        string
	PromptTokens        *int
	CompletionTokens    *int
	TokensPerSecond     *float64
	CreatedAt           time.Time
	ProcessingStartedAt *time.Time
	CompletedAt         *time.Time
}

func getTask(t *testing.T, taskID string) taskRecord {
	t.Helper()
	var task taskRecord
	err := dbPool.QueryRow(context.Background(),
		`SELECT id, trace_id, status, artifact_key, COALESCE(error_message,''), prompt_tokens, completion_tokens, tokens_per_second, created_at, processing_started_at, completed_at
		 FROM tasks WHERE id = $1`, taskID).Scan(
		&task.ID, &task.TraceID, &task.Status, &task.ArtifactKey, &task.ErrorMessage,
		&task.PromptTokens, &task.CompletionTokens, &task.TokensPerSecond,
		&task.CreatedAt, &task.ProcessingStartedAt, &task.CompletedAt,
	)
	if err != nil {
		t.Fatalf("failed to get task %s: %v", taskID, err)
	}
	return task
}

func cleanDB(t *testing.T) {
	t.Helper()
	_, err := dbPool.Exec(context.Background(),
		"TRUNCATE TABLE healing_events, worker_heartbeats, tasks RESTART IDENTITY CASCADE")
	if err != nil {
		t.Fatalf("failed to clean database: %v", err)
	}
}

func queueDepth(t *testing.T) int {
	t.Helper()
	out, err := sqsClient.GetQueueAttributes(context.Background(), &sqs.GetQueueAttributesInput{
		QueueUrl:       &queueURL,
		AttributeNames: []sqstypes.QueueAttributeName{"ApproximateNumberOfMessages"},
	})
	if err != nil {
		t.Fatalf("failed to get queue depth: %v", err)
	}
	var n int
	fmt.Sscanf(out.Attributes["ApproximateNumberOfMessages"], "%d", &n)
	return n
}

func dlqDepth(t *testing.T) int {
	t.Helper()
	out, err := sqsClient.GetQueueAttributes(context.Background(), &sqs.GetQueueAttributesInput{
		QueueUrl:       &dlqURL,
		AttributeNames: []sqstypes.QueueAttributeName{"ApproximateNumberOfMessages"},
	})
	if err != nil {
		t.Fatalf("failed to get DLQ depth: %v", err)
	}
	var n int
	fmt.Sscanf(out.Attributes["ApproximateNumberOfMessages"], "%d", &n)
	return n
}

func purgeQueue(t *testing.T, url string) {
	t.Helper()
	_, err := sqsClient.PurgeQueue(context.Background(), &sqs.PurgeQueueInput{QueueUrl: &url})
	if err != nil {
		t.Logf("purge queue warning: %v", err)
	}
}

func getArtifact(t *testing.T, key string) []byte {
	t.Helper()
	out, err := s3Client.GetObject(context.Background(), &s3.GetObjectInput{
		Bucket: aws.String("traceruntime-outputs"),
		Key:    aws.String(key),
	})
	if err != nil {
		t.Fatalf("failed to get S3 artifact %s: %v", key, err)
	}
	defer out.Body.Close()
	data, err := io.ReadAll(out.Body)
	if err != nil {
		t.Fatalf("failed to read S3 artifact: %v", err)
	}
	return data
}

func getArtifactJSON(t *testing.T, key string) map[string]any {
	t.Helper()
	data := getArtifact(t, key)
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("failed to parse artifact JSON: %v", err)
	}
	return result
}
```

- [ ] **Step 5: Create main_test.go with TestMain**

Create `tests/integration/main_test.go`:

```go
package integration

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"testing"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jackc/pgx/v5/pgxpool"
)

var mockCmd *exec.Cmd

func TestMain(m *testing.M) {
	apiURL = envOr("API_URL", "http://localhost:8082")
	queueURL = envOr("SQS_QUEUE_URL", "http://localhost:4566/000000000000/traceruntime-tasks")
	dlqURL = envOr("SQS_DLQ_URL", "http://localhost:4566/000000000000/traceruntime-tasks-dlq")
	sqsEndpoint := envOr("SQS_ENDPOINT", "http://localhost:4566")
	s3Endpoint := envOr("S3_ENDPOINT", "http://localhost:4566")
	dbURL := envOr("DATABASE_URL", "postgres://traceruntime:traceruntime@localhost:5432/traceruntime?sslmode=disable")

	ctx := context.Background()

	var err error
	dbPool, err = pgxpool.New(ctx, dbURL)
	if err != nil {
		log.Fatalf("cannot connect to database: %v", err)
	}
	defer dbPool.Close()

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	if err != nil {
		log.Fatalf("cannot load AWS config: %v", err)
	}

	sqsClient = sqs.NewFromConfig(awsCfg, func(o *sqs.Options) {
		o.BaseEndpoint = &sqsEndpoint
	})

	s3Client = s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = &s3Endpoint
		o.UsePathStyle = true
	})

	if err := waitForService(apiURL+"/health", 30*time.Second); err != nil {
		log.Fatalf("API not ready: %v", err)
	}

	os.Exit(m.Run())
}

func waitForService(url string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := http.Get(url)
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("service at %s not ready after %v", url, timeout)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
```

- [ ] **Step 6: Add Makefile target**

Add to `Makefile`:

```makefile
test-integration:
	@docker compose ps --format '{{.Service}}' | head -1 > /dev/null 2>&1 || \
		(echo "ERROR: services not running. Run 'make up' first." && exit 1)
	cd tests/integration && go test -v -count=1 -timeout=5m ./...

test-all: test test-integration
```

- [ ] **Step 7: Commit**

```bash
git add tests/integration/ Makefile
git commit -m "feat(9c2): integration test module, mock AI runtime binary, test helpers"
```

---

### Task 5: Task Lifecycle Integration Test

**Files:**
- Create: `tests/integration/task_lifecycle_test.go`

- [ ] **Step 1: Write task lifecycle test**

Create `tests/integration/task_lifecycle_test.go`:

```go
package integration

import (
	"strings"
	"testing"
	"time"
)

func TestTaskLifecycle_HappyPath(t *testing.T) {
	cleanDB(t)
	purgeQueue(t, queueURL)
	purgeQueue(t, dlqURL)

	taskID, traceID := createTask(t, "integration test prompt")

	waitForTaskStatus(t, taskID, "completed", 30*time.Second)

	task := getTask(t, taskID)

	if task.TraceID != traceID {
		t.Errorf("trace_id mismatch: API returned %q, DB has %q", traceID, task.TraceID)
	}

	if task.ArtifactKey == nil || *task.ArtifactKey == "" {
		t.Fatal("artifact_key not set on completed task")
	}

	expectedKeyPrefix := traceID + "/" + taskID
	if !strings.HasPrefix(*task.ArtifactKey, expectedKeyPrefix) {
		t.Errorf("artifact_key %q does not match expected prefix %q", *task.ArtifactKey, expectedKeyPrefix)
	}

	artifact := getArtifactJSON(t, *task.ArtifactKey)
	if output, ok := artifact["output"].(string); !ok || output == "" {
		t.Error("artifact output is empty")
	}

	if task.PromptTokens == nil || *task.PromptTokens <= 0 {
		t.Error("prompt_tokens not populated")
	}
	if task.CompletionTokens == nil || *task.CompletionTokens <= 0 {
		t.Error("completion_tokens not populated")
	}
	if task.TokensPerSecond == nil || *task.TokensPerSecond <= 0 {
		t.Error("tokens_per_second not populated")
	}

	if task.ProcessingStartedAt == nil {
		t.Fatal("processing_started_at not set")
	}
	if task.CompletedAt == nil {
		t.Fatal("completed_at not set")
	}

	if !task.CreatedAt.Before(*task.ProcessingStartedAt) {
		t.Error("created_at should be before processing_started_at")
	}
	if !task.ProcessingStartedAt.Before(*task.CompletedAt) {
		t.Error("processing_started_at should be before completed_at")
	}
}
```

- [ ] **Step 2: Run test locally**

Start services with mock AI Runtime:

```bash
make up
# In another terminal: cd tests/integration && go run ./cmd/mock-ai-runtime/ &
# Ensure worker's AI_RUNTIME_URL points to mock
cd tests/integration && go test -run TestTaskLifecycle -v -count=1 -timeout=30s
```

Expected: PASS (if worker can reach the mock AI Runtime).

- [ ] **Step 3: Commit**

```bash
git add tests/integration/task_lifecycle_test.go
git commit -m "feat(9c2): task lifecycle integration test — happy path"
```

---

### Task 6: Task Failure + Queue Behavior + SSE + Idempotency Tests

**Files:**
- Create: `tests/integration/task_failure_test.go`
- Create: `tests/integration/queue_behavior_test.go`
- Create: `tests/integration/sse_events_test.go`
- Create: `tests/integration/idempotency_test.go`

- [ ] **Step 1: Write task failure test**

Create `tests/integration/task_failure_test.go`:

```go
package integration

import (
	"net/http"
	"testing"
	"time"
)

func TestTaskFailure_AIRuntimeError(t *testing.T) {
	cleanDB(t)
	purgeQueue(t, queueURL)
	purgeQueue(t, dlqURL)

	taskID, _ := createTask(t, "trigger-failure")

	waitForTaskStatus(t, taskID, "failed", 60*time.Second)

	task := getTask(t, taskID)

	if task.ErrorMessage == "" {
		t.Error("error_message should be non-empty for failed task")
	}

	resp, err := http.Get(apiURL + "/health")
	if err != nil {
		t.Fatalf("worker health check failed: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("worker should still be healthy after task failure, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Write queue behavior test**

Create `tests/integration/queue_behavior_test.go`:

```go
package integration

import (
	"context"
	"testing"
	"time"

	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	sqssdk "github.com/aws/aws-sdk-go-v2/service/sqs"
)

func TestQueueBehavior_TraceparentPropagated(t *testing.T) {
	cleanDB(t)
	purgeQueue(t, queueURL)

	taskID, traceID := createTask(t, "traceparent test")

	waitForTaskStatus(t, taskID, "completed", 30*time.Second)

	task := getTask(t, taskID)
	if task.TraceID != traceID {
		t.Errorf("trace_id not consistent: expected %s, got %s", traceID, task.TraceID)
	}
}
```

- [ ] **Step 3: Write SSE events test**

Create `tests/integration/sse_events_test.go`:

```go
package integration

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
	"context"
)

type sseEvent struct {
	EventType string `json:"event_type"`
	TaskID    string `json:"task_id"`
	TraceID   string `json:"trace_id"`
}

func TestSSE_ReceivesTaskEvents(t *testing.T) {
	cleanDB(t)
	purgeQueue(t, queueURL)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, "GET", apiURL+"/events", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("failed to connect SSE: %v", err)
	}
	defer resp.Body.Close()

	events := make(chan sseEvent, 10)
	go func() {
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			var ev sseEvent
			if json.Unmarshal([]byte(data), &ev) == nil && ev.EventType != "" {
				events <- ev
			}
		}
	}()

	time.Sleep(500 * time.Millisecond)
	taskID, _ := createTask(t, "sse test prompt")

	expectedTypes := []string{"task.created", "task.processing", "task.completed"}
	received := map[string]bool{}

	timeout := time.After(25 * time.Second)
	for len(received) < len(expectedTypes) {
		select {
		case ev := <-events:
			if ev.TaskID == taskID {
				received[ev.EventType] = true
			}
		case <-timeout:
			t.Fatalf("timed out waiting for SSE events. received: %v", received)
		}
	}

	for _, et := range expectedTypes {
		if !received[et] {
			t.Errorf("missing SSE event: %s", et)
		}
	}
}

func TestSSE_Reconnect(t *testing.T) {
	cleanDB(t)
	purgeQueue(t, queueURL)

	ctx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)
	req1, _ := http.NewRequestWithContext(ctx1, "GET", apiURL+"/events", nil)
	resp1, err := http.DefaultClient.Do(req1)
	if err != nil {
		t.Fatalf("first SSE connection failed: %v", err)
	}
	cancel1()
	resp1.Body.Close()

	time.Sleep(500 * time.Millisecond)

	ctx2, cancel2 := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel2()

	req2, _ := http.NewRequestWithContext(ctx2, "GET", apiURL+"/events", nil)
	resp2, err := http.DefaultClient.Do(req2)
	if err != nil {
		t.Fatalf("reconnect SSE failed: %v", err)
	}
	defer resp2.Body.Close()

	events := make(chan sseEvent, 10)
	go func() {
		scanner := bufio.NewScanner(resp2.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				var ev sseEvent
				json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev)
				if ev.EventType != "" {
					events <- ev
				}
			}
		}
	}()

	time.Sleep(500 * time.Millisecond)
	taskID, _ := createTask(t, "reconnect test")

	timeout := time.After(15 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.TaskID == taskID && ev.EventType == "task.created" {
				return
			}
		case <-timeout:
			t.Fatal("no events received after SSE reconnect")
		}
	}
}
```

- [ ] **Step 4: Write idempotency test**

Create `tests/integration/idempotency_test.go`:

```go
package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	sqssdk "github.com/aws/aws-sdk-go-v2/service/sqs"
)

func TestIdempotency_DuplicateMessageNotReprocessed(t *testing.T) {
	cleanDB(t)
	purgeQueue(t, queueURL)
	purgeQueue(t, dlqURL)

	taskID, traceID := createTask(t, "idempotency test")
	waitForTaskStatus(t, taskID, "completed", 30*time.Second)

	taskBefore := getTask(t, taskID)

	duplicateBody, _ := json.Marshal(map[string]string{
		"task_id":  taskID,
		"trace_id": traceID,
		"input":    "idempotency test",
	})
	_, err := sqsClient.SendMessage(context.Background(), &sqssdk.SendMessageInput{
		QueueUrl:    &queueURL,
		MessageBody: aws.String(string(duplicateBody)),
	})
	if err != nil {
		t.Fatalf("failed to send duplicate message: %v", err)
	}

	time.Sleep(5 * time.Second)

	taskAfter := getTask(t, taskID)

	if taskAfter.Status != "completed" {
		t.Errorf("task status changed after duplicate: %s", taskAfter.Status)
	}

	if taskBefore.CompletionTokens != nil && taskAfter.CompletionTokens != nil {
		if *taskAfter.CompletionTokens != *taskBefore.CompletionTokens {
			t.Errorf("completion_tokens changed: before=%d, after=%d",
				*taskBefore.CompletionTokens, *taskAfter.CompletionTokens)
		}
	}

	if taskBefore.ArtifactKey != nil && taskAfter.ArtifactKey != nil {
		if *taskAfter.ArtifactKey != *taskBefore.ArtifactKey {
			t.Errorf("artifact_key changed: before=%s, after=%s",
				*taskBefore.ArtifactKey, *taskAfter.ArtifactKey)
		}
	}

	depth := dlqDepth(t)
	_ = depth

	fmt.Printf("  idempotency: task remained %s, tokens unchanged, artifact unchanged\n", taskAfter.Status)
}
```

- [ ] **Step 5: Run all integration tests**

```bash
cd tests/integration && go test -v -count=1 -timeout=5m ./...
```

Expected: All PASS.

- [ ] **Step 6: Commit**

```bash
git add tests/integration/task_failure_test.go tests/integration/queue_behavior_test.go tests/integration/sse_events_test.go tests/integration/idempotency_test.go
git commit -m "feat(9c2): task failure, queue behavior, SSE, idempotency integration tests"
```

---

**STOP — Phase 9C.2 complete. Wait for user validation before proceeding to 9B.**

---

## Phase 9B — Chaos Testing

### Task 7: Docker Controller + Checker Foundation

**Files:**
- Create: `internal/chaos/docker/controller.go`
- Create: `internal/chaos/checker/checker.go`
- Create: `internal/chaos/scenario.go`
- Create: `internal/chaos/config.go`

- [ ] **Step 1: Add Docker SDK dependency**

```bash
go get github.com/docker/docker@v27.4.1
go get github.com/docker/go-connections
```

- [ ] **Step 2: Create Docker controller**

Create `internal/chaos/docker/controller.go`:

```go
package docker

import (
	"context"
	"fmt"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

type HealthStatus string

const (
	HealthHealthy   HealthStatus = "healthy"
	HealthUnhealthy HealthStatus = "unhealthy"
	HealthStarting  HealthStatus = "starting"
	HealthNone      HealthStatus = "none"
	HealthExited    HealthStatus = "exited"
)

type RetryPolicy struct {
	MaxAttempts int
	Backoff     time.Duration
}

type Controller struct {
	client *client.Client
	retry  RetryPolicy
	prefix string
}

func NewController(prefix string) (*Controller, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, fmt.Errorf("docker client: %w", err)
	}
	return &Controller{
		client: cli,
		retry: RetryPolicy{
			MaxAttempts: 3,
			Backoff:     1 * time.Second,
		},
		prefix: prefix,
	}, nil
}

func (c *Controller) containerName(service string) string {
	return c.prefix + "-" + service + "-1"
}

func (c *Controller) Kill(ctx context.Context, service, signal string) error {
	name := c.containerName(service)
	return c.withRetry(ctx, func() error {
		return c.client.ContainerKill(ctx, name, signal)
	})
}

func (c *Controller) Stop(ctx context.Context, service string) error {
	name := c.containerName(service)
	timeout := 10
	return c.withRetry(ctx, func() error {
		return c.client.ContainerStop(ctx, name, container.StopOptions{Timeout: &timeout})
	})
}

func (c *Controller) Start(ctx context.Context, service string) error {
	name := c.containerName(service)
	return c.withRetry(ctx, func() error {
		return c.client.ContainerStart(ctx, name, container.StartOptions{})
	})
}

func (c *Controller) Pause(ctx context.Context, service string) error {
	name := c.containerName(service)
	return c.client.ContainerPause(ctx, name)
}

func (c *Controller) Unpause(ctx context.Context, service string) error {
	name := c.containerName(service)
	return c.client.ContainerUnpause(ctx, name)
}

func (c *Controller) Health(ctx context.Context, service string) (HealthStatus, error) {
	name := c.containerName(service)
	info, err := c.client.ContainerInspect(ctx, name)
	if err != nil {
		return HealthExited, err
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

func (c *Controller) RestartCount(ctx context.Context, service string) (int, error) {
	name := c.containerName(service)
	info, err := c.client.ContainerInspect(ctx, name)
	if err != nil {
		return 0, err
	}
	return info.RestartCount, nil
}

func (c *Controller) WaitForHealthy(ctx context.Context, service string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		health, err := c.Health(ctx, service)
		if err == nil && health == HealthHealthy {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("container %s not healthy after %v", service, timeout)
}

func (c *Controller) ImageID(ctx context.Context, service string) (string, error) {
	name := c.containerName(service)
	info, err := c.client.ContainerInspect(ctx, name)
	if err != nil {
		return "", err
	}
	return info.Image, nil
}

func (c *Controller) Close() error {
	return c.client.Close()
}

func (c *Controller) withRetry(ctx context.Context, fn func() error) error {
	var lastErr error
	for i := 0; i < c.retry.MaxAttempts; i++ {
		if err := fn(); err != nil {
			lastErr = err
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(c.retry.Backoff):
			}
			continue
		}
		return nil
	}
	return fmt.Errorf("after %d attempts: %w", c.retry.MaxAttempts, lastErr)
}
```

- [ ] **Step 3: Create scenario types and config**

Create `internal/chaos/scenario.go`:

```go
package chaos

import (
	"context"
	"time"

	"github.com/runtime-platform/internal/chaos/checker"
	"github.com/runtime-platform/internal/chaos/docker"
)

type Stage string

const (
	StageSetup    Stage = "setup"
	StageInject   Stage = "inject"
	StageObserve  Stage = "observe"
	StageValidate Stage = "validate"
	StageCleanup  Stage = "cleanup"
)

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

type StageResult struct {
	DurationMs int64  `json:"duration_ms"`
	Status     string `json:"status"`
}

type ScenarioResult struct {
	Name       string                 `json:"name"`
	Status     SLOStatus              `json:"status"`
	DurationS  float64                `json:"duration_seconds"`
	Stages     map[Stage]StageResult  `json:"stages"`
	Metrics    map[string]any         `json:"metrics"`
	SLOResults []SLOResult            `json:"slo_results"`
	Warnings   []string               `json:"warnings"`
}

type ScenarioContext struct {
	RunID        string
	ScenarioName string
	StartTime    time.Time
	Timeout      time.Duration
	Config       Config
	Checker      *checker.Checker
	Docker       *docker.Controller
}

type ObserveResult struct {
	Data map[string]any
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

Create `internal/chaos/config.go`:

```go
package chaos

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	APIURL          string
	AIRuntimeURL    string
	WorkerURL       string
	DatabaseURL     string
	SQSEndpoint     string
	SQSQueueURL     string
	SQSDlqURL       string
	InternalToken   string
	OutputDir       string
	GlobalTimeout   time.Duration
	PollInterval    time.Duration
}

func LoadConfig() Config {
	return Config{
		APIURL:        envOr("API_URL", "http://localhost:8082"),
		AIRuntimeURL:  envOr("AI_RUNTIME_URL", "http://localhost:8001"),
		WorkerURL:     envOr("WORKER_URL", "http://localhost:9091"),
		DatabaseURL:   envOr("DATABASE_URL", "postgres://traceruntime:traceruntime@localhost:5432/traceruntime?sslmode=disable"),
		SQSEndpoint:   envOr("SQS_ENDPOINT", "http://localhost:4566"),
		SQSQueueURL:   envOr("SQS_QUEUE_URL", "http://localhost:4566/000000000000/traceruntime-tasks"),
		SQSDlqURL:     envOr("SQS_DLQ_URL", "http://localhost:4566/000000000000/traceruntime-tasks-dlq"),
		InternalToken: envOr("INTERNAL_TOKEN", ""),
		OutputDir:     envOr("CHAOS_OUTPUT_DIR", "results"),
		GlobalTimeout: parseDuration(envOr("CHAOS_TIMEOUT", "15m"), 15*time.Minute),
		PollInterval:  parseDuration(envOr("CHAOS_POLL_INTERVAL", "2s"), 2*time.Second),
	}
}

var scenarioTimeouts = map[string]time.Duration{
	"queue-flood":     2 * time.Minute,
	"slow-inference":  10 * time.Minute,
	"ai-failure":      3 * time.Minute,
	"runtime-hang":    3 * time.Minute,
	"worker-crash":    3 * time.Minute,
	"postgres-failure": 3 * time.Minute,
}

func ScenarioTimeout(name string) time.Duration {
	if t, ok := scenarioTimeouts[name]; ok {
		return t
	}
	return 5 * time.Minute
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseDuration(s string, def time.Duration) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return def
	}
	return d
}
```

- [ ] **Step 4: Create checker foundation**

Create `internal/chaos/checker/checker.go`:

```go
package checker

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sqs"
	sqstypes "github.com/aws/aws-sdk-go-v2/service/sqs/types"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/runtime-platform/internal/chaos/docker"
)

type HealingEvent struct {
	ID        string
	EventType string
	Severity  string
	Status    string
	CreatedAt time.Time
}

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

type Checker struct {
	DB           *pgxpool.Pool
	Docker       *docker.Controller
	SQS          *sqs.Client
	APIURL       string
	WorkerURL    string
	AIRuntimeURL string
	QueueURL     string
	DlqURL       string
}

func (c *Checker) WaitFor(ctx context.Context, cond WaitCondition) (*WaitResult, error) {
	start := time.Now()
	attempts := 0
	deadline := time.Now().Add(cond.Timeout)
	for time.Now().Before(deadline) {
		attempts++
		ok, err := cond.Check(ctx)
		if err == nil && ok {
			return &WaitResult{Elapsed: time.Since(start), Attempts: attempts}, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("%s: %v", cond.FailureMessage, ctx.Err())
		case <-time.After(cond.Interval):
		}
	}
	return nil, fmt.Errorf("%s: timed out after %v (%d attempts)", cond.FailureMessage, cond.Timeout, attempts)
}

func (c *Checker) ActiveHealingEvents(ctx context.Context) ([]HealingEvent, error) {
	rows, err := c.DB.Query(ctx, "SELECT id, event_type, severity, status, created_at FROM healing_events WHERE status = 'active'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []HealingEvent
	for rows.Next() {
		var e HealingEvent
		if err := rows.Scan(&e.ID, &e.EventType, &e.Severity, &e.Status, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, nil
}

func (c *Checker) HealingEventsSince(ctx context.Context, since time.Time) ([]HealingEvent, error) {
	rows, err := c.DB.Query(ctx, "SELECT id, event_type, severity, status, created_at FROM healing_events WHERE created_at >= $1 ORDER BY created_at", since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []HealingEvent
	for rows.Next() {
		var e HealingEvent
		if err := rows.Scan(&e.ID, &e.EventType, &e.Severity, &e.Status, &e.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, nil
}

func (c *Checker) QueueDepth(ctx context.Context) (int, error) {
	out, err := c.SQS.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       &c.QueueURL,
		AttributeNames: []sqstypes.QueueAttributeName{"ApproximateNumberOfMessages"},
	})
	if err != nil {
		return 0, err
	}
	var n int
	fmt.Sscanf(out.Attributes["ApproximateNumberOfMessages"], "%d", &n)
	return n, nil
}

func (c *Checker) DLQDepth(ctx context.Context) (int, error) {
	out, err := c.SQS.GetQueueAttributes(ctx, &sqs.GetQueueAttributesInput{
		QueueUrl:       &c.DlqURL,
		AttributeNames: []sqstypes.QueueAttributeName{"ApproximateNumberOfMessages"},
	})
	if err != nil {
		return 0, err
	}
	var n int
	fmt.Sscanf(out.Attributes["ApproximateNumberOfMessages"], "%d", &n)
	return n, nil
}

func (c *Checker) AllContainersHealthy(ctx context.Context, services []string) (bool, error) {
	for _, svc := range services {
		h, err := c.Docker.Health(ctx, svc)
		if err != nil || h != docker.HealthHealthy {
			return false, err
		}
	}
	return true, nil
}

func (c *Checker) HTTPHealth(ctx context.Context, url string) (int, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url+"/health", nil)
	if err != nil {
		return 0, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	return resp.StatusCode, nil
}

func (c *Checker) APIHealth(ctx context.Context) (int, error) {
	return c.HTTPHealth(ctx, c.APIURL)
}

func (c *Checker) WorkerHealth(ctx context.Context) (int, error) {
	return c.HTTPHealth(ctx, c.WorkerURL)
}

func (c *Checker) AIRuntimeHealth(ctx context.Context) (int, error) {
	return c.HTTPHealth(ctx, c.AIRuntimeURL)
}
```

- [ ] **Step 5: Commit**

```bash
git add internal/chaos/
git commit -m "feat(9b): chaos testing foundation — docker controller, checker, scenario types"
```

---

### Task 8: Runner + Report + Preflight + CLI

**Files:**
- Create: `internal/chaos/runner.go`
- Create: `internal/chaos/report.go`
- Create: `internal/chaos/health.go`
- Create: `cmd/chaos/main.go`

- [ ] **Step 1: Create runner**

Create `internal/chaos/runner.go`:

```go
package chaos

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

type Runner struct {
	scenarios []Scenario
	ctx       *ScenarioContext
}

func NewRunner(scenarios []Scenario, ctx *ScenarioContext) *Runner {
	return &Runner{scenarios: scenarios, ctx: ctx}
}

func (r *Runner) Run(ctx context.Context) ([]ScenarioResult, error) {
	var results []ScenarioResult

	for _, s := range r.scenarios {
		result := r.runScenario(ctx, s)
		results = append(results, result)
	}

	return results, nil
}

func (r *Runner) runScenario(ctx context.Context, s Scenario) ScenarioResult {
	timeout := ScenarioTimeout(s.Name())
	sctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	r.ctx.ScenarioName = s.Name()
	r.ctx.StartTime = time.Now()
	r.ctx.Timeout = timeout

	result := ScenarioResult{
		Name:    s.Name(),
		Stages:  make(map[Stage]StageResult),
		Metrics: make(map[string]any),
	}

	stages := []struct {
		stage Stage
		fn    func() error
	}{
		{StageSetup, func() error { return s.Setup(sctx, r.ctx) }},
		{StageInject, func() error { return s.Inject(sctx, r.ctx) }},
	}

	for _, st := range stages {
		start := time.Now()
		slog.Info("chaos", "scenario", s.Name(), "stage", st.stage)
		if err := st.fn(); err != nil {
			result.Stages[st.stage] = StageResult{DurationMs: time.Since(start).Milliseconds(), Status: "error"}
			result.Status = SLOFail
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s failed: %v", st.stage, err))
			r.runCleanup(sctx, s, &result)
			result.DurationS = time.Since(r.ctx.StartTime).Seconds()
			return result
		}
		result.Stages[st.stage] = StageResult{DurationMs: time.Since(start).Milliseconds(), Status: "ok"}
	}

	observeStart := time.Now()
	slog.Info("chaos", "scenario", s.Name(), "stage", StageObserve)
	observed, err := s.Observe(sctx, r.ctx)
	if err != nil {
		result.Stages[StageObserve] = StageResult{DurationMs: time.Since(observeStart).Milliseconds(), Status: "error"}
		result.Status = SLOFail
		result.Warnings = append(result.Warnings, fmt.Sprintf("observe failed: %v", err))
		r.runCleanup(sctx, s, &result)
		result.DurationS = time.Since(r.ctx.StartTime).Seconds()
		return result
	}
	result.Stages[StageObserve] = StageResult{DurationMs: time.Since(observeStart).Milliseconds(), Status: "ok"}

	validateStart := time.Now()
	slog.Info("chaos", "scenario", s.Name(), "stage", StageValidate)
	validated, err := s.Validate(sctx, r.ctx, observed)
	if err != nil {
		result.Stages[StageValidate] = StageResult{DurationMs: time.Since(validateStart).Milliseconds(), Status: "error"}
		result.Status = SLOFail
		result.Warnings = append(result.Warnings, fmt.Sprintf("validate failed: %v", err))
	} else {
		result.Stages[StageValidate] = StageResult{DurationMs: time.Since(validateStart).Milliseconds(), Status: "ok"}
		result.SLOResults = validated.SLOResults
		result.Metrics = validated.Metrics
		result.Status = validated.Status
		result.Warnings = validated.Warnings
	}

	r.runCleanup(sctx, s, &result)
	result.DurationS = time.Since(r.ctx.StartTime).Seconds()
	return result
}

func (r *Runner) runCleanup(ctx context.Context, s Scenario, result *ScenarioResult) {
	start := time.Now()
	slog.Info("chaos", "scenario", s.Name(), "stage", StageCleanup)
	if err := s.Cleanup(ctx, r.ctx); err != nil {
		result.Stages[StageCleanup] = StageResult{DurationMs: time.Since(start).Milliseconds(), Status: "error"}
		result.Warnings = append(result.Warnings, fmt.Sprintf("cleanup failed: %v", err))
	} else {
		result.Stages[StageCleanup] = StageResult{DurationMs: time.Since(start).Milliseconds(), Status: "ok"}
	}
}

func ComputeStatus(slos []SLOResult) SLOStatus {
	anyFunctionalFail := false
	anyTimingFail := false
	for _, s := range slos {
		if s.Status == SLOFail {
			if s.Type == SLOFunctional {
				anyFunctionalFail = true
			} else {
				anyTimingFail = true
			}
		}
	}
	if anyFunctionalFail {
		return SLOFail
	}
	if anyTimingFail {
		return SLOWarn
	}
	return SLOPass
}
```

- [ ] **Step 2: Create report**

Create `internal/chaos/report.go`:

```go
package chaos

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type SuiteReport struct {
	Version        string                    `json:"version"`
	RunID          string                    `json:"run_id"`
	GitCommit      string                    `json:"git_commit"`
	Environment    string                    `json:"environment"`
	DockerProject  string                    `json:"docker_compose_project"`
	DockerImages   map[string]string         `json:"docker_images"`
	Timestamp      string                    `json:"timestamp"`
	DurationS      float64                   `json:"duration_seconds"`
	Summary        SuiteSummary              `json:"summary"`
	Scenarios      []ScenarioResult          `json:"scenarios"`
}

type SuiteSummary struct {
	Total  int `json:"total"`
	Passed int `json:"passed"`
	Warned int `json:"warned"`
	Failed int `json:"failed"`
}

func gitCommit() string {
	out, err := exec.Command("git", "rev-parse", "--short", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func WriteSuiteReport(outputDir string, results []ScenarioResult) (string, error) {
	summary := SuiteSummary{Total: len(results)}
	totalDuration := 0.0
	for _, r := range results {
		totalDuration += r.DurationS
		switch r.Status {
		case SLOPass:
			summary.Passed++
		case SLOWarn:
			summary.Warned++
		case SLOFail:
			summary.Failed++
		}
	}

	report := SuiteReport{
		Version:       "1.0",
		RunID:         fmt.Sprintf("chaos-%s", time.Now().Format("20060102-150405")),
		GitCommit:     gitCommit(),
		Environment:   "local-docker",
		DockerProject: "traceruntime",
		DockerImages:  map[string]string{},
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		DurationS:     totalDuration,
		Summary:       summary,
		Scenarios:     results,
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", err
	}

	filename := fmt.Sprintf("chaos-suite-%s.json", time.Now().Format("20060102-150405"))
	path := filepath.Join(outputDir, filename)
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o644)
}

func WriteScenarioReport(outputDir string, result ScenarioResult) (string, error) {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return "", err
	}

	filename := fmt.Sprintf("chaos-%s-%s.json", result.Name, time.Now().Format("20060102-150405"))
	path := filepath.Join(outputDir, filename)
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o644)
}
```

- [ ] **Step 3: Create preflight health check**

Create `internal/chaos/health.go`:

```go
package chaos

import (
	"context"
	"fmt"

	"github.com/runtime-platform/internal/chaos/checker"
	"github.com/runtime-platform/internal/chaos/docker"
)

func Preflight(ctx context.Context, chk *checker.Checker, needsAIRuntime bool) error {
	services := []string{"api", "worker", "watchdog", "postgres", "localstack"}
	if needsAIRuntime {
		services = append(services, "ai-runtime")
	}

	for _, svc := range services {
		health, err := chk.Docker.Health(ctx, svc)
		if err != nil || health != docker.HealthHealthy {
			return fmt.Errorf("preflight: %s is %s (err: %v)", svc, health, err)
		}
	}

	events, err := chk.ActiveHealingEvents(ctx)
	if err != nil {
		return fmt.Errorf("preflight: cannot query healing events: %w", err)
	}
	if len(events) > 0 {
		return fmt.Errorf("preflight: %d active healing events — resolve before chaos", len(events))
	}

	depth, err := chk.QueueDepth(ctx)
	if err != nil {
		return fmt.Errorf("preflight: cannot query queue depth: %w", err)
	}
	if depth > 0 {
		return fmt.Errorf("preflight: queue depth = %d — purge before chaos", depth)
	}

	dlq, err := chk.DLQDepth(ctx)
	if err != nil {
		return fmt.Errorf("preflight: cannot query DLQ depth: %w", err)
	}
	if dlq > 0 {
		return fmt.Errorf("preflight: DLQ depth = %d — drain before chaos", dlq)
	}

	return nil
}
```

- [ ] **Step 4: Create CLI entry point**

Create `cmd/chaos/main.go`:

```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	sqssdk "github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/runtime-platform/internal/chaos"
	"github.com/runtime-platform/internal/chaos/checker"
	"github.com/runtime-platform/internal/chaos/docker"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	var scenarioFlag, listFlag, outputDir string
	flag.StringVar(&scenarioFlag, "scenario", "", "run a single scenario")
	flag.StringVar(&listFlag, "list", "", "comma-separated list of scenarios")
	flag.StringVar(&outputDir, "output-dir", "", "output directory for reports")
	flag.Parse()

	cfg := chaos.LoadConfig()
	if outputDir != "" {
		cfg.OutputDir = outputDir
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	dockerCtrl, err := docker.NewController("traceruntime")
	if err != nil {
		slog.Error("failed to create docker controller", "error", err)
		os.Exit(1)
	}
	defer dockerCtrl.Close()

	dbPool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer dbPool.Close()

	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	if err != nil {
		slog.Error("failed to load AWS config", "error", err)
		os.Exit(1)
	}
	sqsEndpoint := cfg.SQSEndpoint
	sqsClient := sqssdk.NewFromConfig(awsCfg, func(o *sqssdk.Options) {
		o.BaseEndpoint = &sqsEndpoint
	})

	chk := &checker.Checker{
		DB:           dbPool,
		Docker:       dockerCtrl,
		SQS:          sqsClient,
		APIURL:       cfg.APIURL,
		WorkerURL:    cfg.WorkerURL,
		AIRuntimeURL: cfg.AIRuntimeURL,
		QueueURL:     cfg.SQSQueueURL,
		DlqURL:       cfg.SQSDlqURL,
	}

	allScenarios := chaos.AllScenarios()

	var selected []chaos.Scenario
	if scenarioFlag != "" {
		for _, s := range allScenarios {
			if s.Name() == scenarioFlag {
				selected = append(selected, s)
				break
			}
		}
		if len(selected) == 0 {
			slog.Error("unknown scenario", "name", scenarioFlag)
			os.Exit(1)
		}
	} else if listFlag != "" {
		names := strings.Split(listFlag, ",")
		nameSet := make(map[string]bool)
		for _, n := range names {
			nameSet[strings.TrimSpace(n)] = true
		}
		for _, s := range allScenarios {
			if nameSet[s.Name()] {
				selected = append(selected, s)
			}
		}
	} else {
		selected = allScenarios
	}

	needsAI := false
	for _, s := range selected {
		n := s.Name()
		if n == "slow-inference" || n == "ai-failure" || n == "runtime-hang" {
			needsAI = true
			break
		}
	}

	slog.Info("chaos preflight starting", "scenarios", len(selected))
	if err := chaos.Preflight(ctx, chk, needsAI); err != nil {
		slog.Error("preflight failed", "error", err)
		os.Exit(1)
	}
	slog.Info("preflight passed")

	sctx := &chaos.ScenarioContext{
		RunID:   fmt.Sprintf("chaos-%s", strings.ReplaceAll(strings.Split(cfg.GlobalTimeout.String(), ".")[0], "m", "m")),
		Config:  cfg,
		Checker: chk,
		Docker:  dockerCtrl,
	}

	runner := chaos.NewRunner(selected, sctx)
	results, err := runner.Run(ctx)
	if err != nil {
		slog.Error("runner error", "error", err)
		os.Exit(1)
	}

	if len(selected) == 1 {
		path, err := chaos.WriteScenarioReport(cfg.OutputDir, results[0])
		if err != nil {
			slog.Error("failed to write report", "error", err)
		} else {
			slog.Info("report written", "path", path)
		}
	}

	path, err := chaos.WriteSuiteReport(cfg.OutputDir, results)
	if err != nil {
		slog.Error("failed to write suite report", "error", err)
	} else {
		slog.Info("suite report written", "path", path)
	}

	for _, r := range results {
		fmt.Printf("  %-20s %s  (%.1fs)\n", r.Name, r.Status, r.DurationS)
	}

	for _, r := range results {
		if r.Status == chaos.SLOFail {
			os.Exit(1)
		}
	}
}
```

- [ ] **Step 5: Create AllScenarios placeholder**

For now, return an empty slice. Scenarios will be added in the next task.

Add to `internal/chaos/scenario.go`:

```go
func AllScenarios() []Scenario {
	return []Scenario{}
}
```

- [ ] **Step 6: Verify compilation**

```bash
go build ./cmd/chaos/
go build ./internal/chaos/...
```

Expected: Builds successfully.

- [ ] **Step 7: Add chaos Makefile targets**

Add to `Makefile`:

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

- [ ] **Step 8: Commit**

```bash
git add internal/chaos/runner.go internal/chaos/report.go internal/chaos/health.go cmd/chaos/main.go Makefile
git commit -m "feat(9b): chaos runner, report generator, preflight checks, CLI entry point"
```

---

### Task 9: AI Runtime Chaos Config Endpoint

**Files:**
- Create: `ai-runtime/chaos_config.py`
- Modify: `ai-runtime/main.py`
- Modify: `docker-compose.yml`

- [ ] **Step 1: Create chaos config module**

Create `ai-runtime/chaos_config.py`:

```python
import asyncio
import os
import threading
from dataclasses import dataclass, field

from fastapi import APIRouter, Request
from fastapi.responses import JSONResponse


@dataclass
class ChaosState:
    delay_seconds: int = 0
    failure_rate: float = 0.0
    timeout_rate: float = 0.0
    _lock: threading.Lock = field(default_factory=threading.Lock, repr=False)

    def update(self, delay_seconds: int = 0, failure_rate: float = 0.0, timeout_rate: float = 0.0):
        with self._lock:
            self.delay_seconds = delay_seconds
            self.failure_rate = failure_rate
            self.timeout_rate = timeout_rate

    def reset(self):
        with self._lock:
            self.delay_seconds = 0
            self.failure_rate = 0.0
            self.timeout_rate = 0.0

    def snapshot(self):
        with self._lock:
            return {
                "delay_seconds": self.delay_seconds,
                "failure_rate": self.failure_rate,
                "timeout_rate": self.timeout_rate,
            }

    async def apply_delay(self):
        with self._lock:
            delay = self.delay_seconds
        if delay > 0:
            await asyncio.sleep(delay)


chaos_state = ChaosState()

chaos_router = APIRouter(prefix="/internal/chaos")


def _validate_token(request: Request) -> bool:
    expected = os.environ.get("INTERNAL_TOKEN", "")
    actual = request.headers.get("X-Internal-Token", "")
    return actual == expected


@chaos_router.get("/config")
async def get_config(request: Request):
    if not _validate_token(request):
        return JSONResponse(status_code=403, content={"error": "forbidden"})
    return JSONResponse(content=chaos_state.snapshot())


@chaos_router.post("/config")
async def set_config(request: Request):
    if not _validate_token(request):
        return JSONResponse(status_code=403, content={"error": "forbidden"})
    body = await request.json()
    chaos_state.update(
        delay_seconds=body.get("delay_seconds", 0),
        failure_rate=body.get("failure_rate", 0.0),
        timeout_rate=body.get("timeout_rate", 0.0),
    )
    return JSONResponse(content=chaos_state.snapshot())


@chaos_router.post("/reset")
async def reset_config(request: Request):
    if not _validate_token(request):
        return JSONResponse(status_code=403, content={"error": "forbidden"})
    chaos_state.reset()
    return JSONResponse(content=chaos_state.snapshot())
```

- [ ] **Step 2: Register chaos router in main.py (conditional)**

Add to `ai-runtime/main.py`, after the FastAPI app is created:

```python
import os

# After: app = FastAPI(...)
chaos_enabled = os.environ.get("CHAOS_ENABLED", "false").lower() == "true"
if chaos_enabled:
    internal_token = os.environ.get("INTERNAL_TOKEN", "disabled")
    if internal_token == "disabled" or len(internal_token) < 32:
        raise RuntimeError("CHAOS_ENABLED=true requires INTERNAL_TOKEN with >= 32 characters")
    from chaos_config import chaos_router, chaos_state
    app.include_router(chaos_router)

# In the /infer endpoint, before calling the graph:
# if chaos_enabled:
#     await chaos_state.apply_delay()
```

- [ ] **Step 3: Add CHAOS_ENABLED to docker-compose.yml**

In the `ai-runtime` service environment:

```yaml
- CHAOS_ENABLED=${CHAOS_ENABLED:-false}
- INTERNAL_TOKEN=${INTERNAL_TOKEN:-disabled}
```

- [ ] **Step 4: Test manually**

```bash
CHAOS_ENABLED=true INTERNAL_TOKEN=$(openssl rand -hex 32) docker compose --profile full up -d
# Test chaos config:
TOKEN=$(cat .chaos.token)
curl -X POST http://localhost:8001/internal/chaos/config -H "X-Internal-Token: $TOKEN" -d '{"delay_seconds":5}'
curl http://localhost:8001/internal/chaos/config -H "X-Internal-Token: $TOKEN"
curl -X POST http://localhost:8001/internal/chaos/reset -H "X-Internal-Token: $TOKEN"
# Without CHAOS_ENABLED — routes should 404
```

- [ ] **Step 5: Commit**

```bash
git add ai-runtime/chaos_config.py ai-runtime/main.py docker-compose.yml
git commit -m "feat(9b): AI runtime chaos config endpoint with token auth and fail-fast validation"
```

---

### Task 10: Implement Chaos Scenarios (Worker Crash + Queue Flood + Postgres Failure)

**Files:**
- Create: `internal/chaos/scenarios/worker_crash.go`
- Create: `internal/chaos/scenarios/queue_flood.go`
- Create: `internal/chaos/scenarios/postgres_failure.go`
- Modify: `internal/chaos/scenario.go` (wire AllScenarios)

These are the 3 scenarios that don't require the AI runtime. Each follows the same pattern: setup → inject → observe → validate → cleanup.

- [ ] **Step 1: Implement worker crash scenario**

Create `internal/chaos/scenarios/worker_crash.go` — implements the full Scenario interface following the spec: kill worker, wait for watchdog detection, validate healing events, restart worker, confirm recovery.

- [ ] **Step 2: Implement queue flood scenario**

Create `internal/chaos/scenarios/queue_flood.go` — burst-submits 25 tasks, waits for queue.lag detection, validates worker continues processing, purges queue in cleanup.

- [ ] **Step 3: Implement postgres failure scenario**

Create `internal/chaos/scenarios/postgres_failure.go` — stops postgres, validates API/worker/watchdog remain alive with no crash-loops, restarts postgres, confirms reconnection.

- [ ] **Step 4: Wire AllScenarios**

Update `internal/chaos/scenario.go`:

```go
import "github.com/runtime-platform/internal/chaos/scenarios"

func AllScenarios() []Scenario {
	return []Scenario{
		scenarios.NewQueueFlood(),
		scenarios.NewWorkerCrash(),
		scenarios.NewPostgresFailure(),
	}
}
```

- [ ] **Step 5: Test with make chaos**

```bash
make chaos-up
make chaos SCENARIO=worker-crash
cat results/chaos-worker-crash-*.json
make chaos-down
```

- [ ] **Step 6: Commit**

```bash
git add internal/chaos/scenarios/ internal/chaos/scenario.go
git commit -m "feat(9b): chaos scenarios — worker crash, queue flood, postgres failure"
```

---

### Task 11: Implement AI-Dependent Chaos Scenarios

**Files:**
- Create: `internal/chaos/scenarios/ai_failure.go`
- Create: `internal/chaos/scenarios/slow_inference.go`
- Create: `internal/chaos/scenarios/runtime_hang.go`
- Modify: `internal/chaos/scenario.go` (add to AllScenarios)

- [ ] **Step 1: Implement AI failure scenario**

Create `internal/chaos/scenarios/ai_failure.go` — stops ai-runtime, submits tasks, validates they fail gracefully, restarts ai-runtime, confirms recovery.

- [ ] **Step 2: Implement slow inference scenario**

Create `internal/chaos/scenarios/slow_inference.go` — sets delay via chaos config endpoint (120s), submits 2 tasks, validates VT holds, no DLQ, resets chaos config.

- [ ] **Step 3: Implement runtime hang scenario**

Create `internal/chaos/scenarios/runtime_hang.go` — pauses ai-runtime container, validates task.stuck detection by watchdog, unpauses, confirms recovery.

- [ ] **Step 4: Add to AllScenarios**

Update `AllScenarios()` to include all 6 scenarios in order.

- [ ] **Step 5: Run full chaos suite**

```bash
make chaos-up
make chaos
cat results/chaos-suite-*.json
make chaos-down
```

- [ ] **Step 6: Commit**

```bash
git add internal/chaos/scenarios/ internal/chaos/scenario.go
git commit -m "feat(9b): chaos scenarios — AI failure, slow inference, runtime hang"
```

---

**STOP — Phase 9B complete. Wait for user validation before proceeding to 9C.3.**

---

## Phase 9C.3 — CI Hardening

### Task 12: Fix Smoke Test + Expand Integration Job

**Files:**
- Modify: `scripts/smoke-test.sh`
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Fix stale visibility timeout assertion**

In `scripts/smoke-test.sh`, line 59:

```bash
# Change:
if [ "$VTIMEOUT" = "150" ]; then
# To:
if [ "$VTIMEOUT" = "360" ]; then
```

Also update the label on line 60:

```bash
  ok "visibility_timeout = 360"
else
  fail "visibility_timeout = 360" "got ${VTIMEOUT:-<empty>}"
```

- [ ] **Step 2: Add watchdog build + test to Quality Gate job**

In `.github/workflows/ci.yml`, after the worker lint step, add:

```yaml
      - name: Go build (watchdog)
        run: cd services/watchdog && go build ./...

      - name: Go test (watchdog)
        run: cd services/watchdog && go test -race -count=1 ./...

      - name: golangci-lint (watchdog)
        uses: golangci/golangci-lint-action@v6
        with:
          version: v2.2
          working-directory: services/watchdog
          args: --timeout=3m
```

- [ ] **Step 3: Commit**

```bash
git add scripts/smoke-test.sh .github/workflows/ci.yml
git commit -m "fix(9c3): smoke test VT 150→360, add watchdog build/test/lint to CI"
```

---

### Task 13: E2E Validation CI Job

**Files:**
- Modify: `.github/workflows/ci.yml`

- [ ] **Step 1: Add E2E job to CI workflow**

Add the full E2E job from the 9C.3 spec to `.github/workflows/ci.yml`. This includes:
- Service containers (LocalStack, PostgreSQL)
- Docker network creation
- Container builds (API, Worker, Watchdog)
- Mock AI Runtime binary build + start
- Service container startup with explicit network
- Health check polling
- Integration test execution
- Log collection as artifacts

Use the exact job definition from the spec at `docs/superpowers/specs/2026-06-22-phase9c3-ci-hardening-design.md`.

- [ ] **Step 2: Verify CI config is valid**

```bash
docker compose config --quiet
```

- [ ] **Step 3: Commit and push to test CI**

```bash
git add .github/workflows/ci.yml
git commit -m "feat(9c3): E2E validation CI job — builds containers, runs integration tests"
```

---

**STOP — Phase 9C.3 complete. Wait for user validation.**

---

## Summary

| Task | Phase | Description |
|---|---|---|
| 1 | 9C.1 | Configurable CORS + SSE Origin Validation + CSP |
| 2 | 9C.1 | Internal Token Middleware + Rate Limit GET |
| 3 | 9C.1 | Docker Compose + Frontend Token Integration |
| 4 | 9C.2 | Mock AI Runtime Binary + Test Module Setup |
| 5 | 9C.2 | Task Lifecycle Integration Test |
| 6 | 9C.2 | Task Failure + Queue + SSE + Idempotency Tests |
| 7 | 9B | Docker Controller + Checker Foundation |
| 8 | 9B | Runner + Report + Preflight + CLI |
| 9 | 9B | AI Runtime Chaos Config Endpoint |
| 10 | 9B | Chaos Scenarios (Worker Crash, Queue Flood, Postgres) |
| 11 | 9B | AI-Dependent Chaos Scenarios |
| 12 | 9C.3 | Fix Smoke Test + Expand Integration Job |
| 13 | 9C.3 | E2E Validation CI Job |
