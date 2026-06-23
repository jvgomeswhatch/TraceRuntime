# Phase 9C.1 — Security Hardening Design

## Objective

Harden the TraceRuntime API surface by addressing the two security items deferred from earlier phases, plus closing gaps identified during the Phase 9 review. All changes are server-side — no client-side tokens, no BFF proxy (overengineered for a local-first project).

## Constraints

- 14 GB RAM, no GPU, Docker Compose only
- Local-first project — no public internet exposure
- No BFF/reverse proxy added (unnecessary complexity for local Docker Compose)
- Security hardening must not break existing functionality
- All changes are backward-compatible with current frontend

---

## Current Security State

### What's Already Good

| Feature | Status | Details |
|---|---|---|
| Non-root containers | ✅ | `USER app` in API, Worker, AI Runtime Dockerfiles |
| `no-new-privileges` | ✅ | All service containers in docker-compose.yml |
| Resource limits | ✅ | `mem_limit` and `cpus` on all containers |
| Rate limiting | ✅ | Token bucket, per-IP, 10 RPS / burst 20 on POST |
| Input validation | ✅ | 64KB body limit, 10K char input limit on `/tasks` |
| Basic security headers | ✅ | `X-Content-Type-Options`, `X-Frame-Options`, `Referrer-Policy` |
| INTERNAL_TOKEN | ✅ | Used on `/internal/events` endpoint |
| Structured error responses | ✅ | `jsonError()` helper, no stack traces in responses |

### Gaps to Close

| Gap | Severity | Roadmap Item |
|---|---|---|
| SSE `/events` has no Origin validation | Medium | Yes (9C) |
| `/api/operations/summary` has no auth | Low | Yes (9C) |
| `/api/events/recent` has no auth | Low | No (same pattern) |
| `/api/capacity/latest` has no auth | Low | No (same pattern) |
| CORS origin hardcoded in code | Low | No |
| Rate limiting only on POST, not GET | Low | No |
| `INTERNAL_TOKEN` default is insecure (`local-dev-token-change-in-prod`) | Low | No |
| No `Content-Security-Policy` header | Low | No |
| Watchdog Dockerfile missing `USER` directive | Low | No |

---

## Changes

### 1. SSE Origin Validation

**File:** `services/api/internal/http/sse.go`

Add server-side Origin header validation on the SSE `/events` endpoint. Only connections from allowed origins are accepted.

```go
func (h *SSEHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
    origin := r.Header.Get("Origin")
    if origin != "" && !isAllowedOrigin(origin) {
        http.Error(w, "origin not allowed", http.StatusForbidden)
        return
    }
    // ... existing SSE logic
}
```

**Allowed origins:**
- Configurable via `ALLOWED_ORIGINS` env var (comma-separated)
- Default: `http://localhost:3001` (frontend dev server)
- If `ALLOWED_ORIGINS` is empty or unset, default is applied

**Why not reject empty Origin?** Browsers always send Origin on cross-origin requests. Same-origin requests and non-browser clients (curl, Grafana) may omit it. Rejecting empty Origin would break legitimate tooling.

### 2. CORS Origin Configuration

**File:** `services/api/internal/http/router.go`

Make CORS origin configurable instead of hardcoded.

```go
func corsMiddleware(allowedOrigins []string) func(http.Handler) http.Handler {
    originSet := make(map[string]bool)
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
            w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
            // ... security headers
            next.ServeHTTP(w, r)
        })
    }
}
```

**Changes:**
- CORS origin read from `ALLOWED_ORIGINS` env var
- Only matching origins get `Access-Control-Allow-Origin` response header
- Non-matching origins get no CORS header (browser blocks the request)
- Shares the same `ALLOWED_ORIGINS` config as SSE validation

### 3. Read-Only API Endpoints — Rate Limiting

**File:** `services/api/internal/http/ratelimit.go`

Extend rate limiting to cover GET endpoints that query the database.

**Current:** Rate limiting only on POST requests.

**Change:** Apply rate limiting to all non-health, non-metrics endpoints.

```go
func (rl *RateLimiter) shouldLimit(r *http.Request) bool {
    path := r.URL.Path
    if path == "/health" || path == "/metrics" {
        return false
    }
    return true
}
```

Rate limits:
- POST endpoints: 10 RPS / burst 20 (unchanged)
- GET endpoints: 30 RPS / burst 60 (higher limit, read-only)

### 4. INTERNAL_TOKEN Default Hardening

**File:** `docker-compose.yml`

Change the default `INTERNAL_TOKEN` from an insecure string to a value that fails fast.

```yaml
# Current:
INTERNAL_TOKEN: ${INTERNAL_TOKEN:-local-dev-token-change-in-prod}

# New:
INTERNAL_TOKEN: ${INTERNAL_TOKEN:-}
```

**API startup validation:** If `INTERNAL_TOKEN` is empty, the API logs a warning but continues (development mode). In production-like setups, the operator sets the token explicitly.

**Why not fail-fast?** This is a local-first project. Failing on startup without a token would break `make up` for new developers. The warning is sufficient.

### 5. Security Headers — Content-Security-Policy

**File:** `services/api/internal/http/router.go`

Add `Content-Security-Policy` header to all responses.

```go
w.Header().Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
```

This is a JSON API — no HTML rendering. `default-src 'none'` blocks everything. `frame-ancestors 'none'` prevents framing (defense-in-depth alongside `X-Frame-Options: DENY`).

### 6. Watchdog Dockerfile — Non-Root User

**File:** `services/watchdog/Dockerfile`

Add `USER` directive (same pattern as API and Worker).

```dockerfile
RUN addgroup -S app && adduser -S app -G app
USER app
```

### 7. Operations Endpoints — Internal Token

**Files:** `services/api/internal/http/operations.go`, `recent_events.go`, `capacity.go`

Add optional `X-Internal-Token` validation to operational endpoints.

**Behavior:**
- If `INTERNAL_TOKEN` is set (non-empty): require `X-Internal-Token` header matching the token. Return 403 if missing or invalid.
- If `INTERNAL_TOKEN` is empty: allow without token (development mode).

This approach avoids breaking the frontend during local development while enabling access control when a token is configured.

**Implementation:** Extract token validation into a reusable middleware:

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

Applied to routes:
- `GET /api/operations/summary`
- `GET /api/events/recent`
- `GET /api/capacity/latest`
- `POST /internal/events` (already has this, refactored to use shared middleware)

**Frontend change:** The frontend must send `X-Internal-Token` when calling these endpoints. Token injected via `NEXT_PUBLIC_INTERNAL_TOKEN` env var (or read from the same `INTERNAL_TOKEN` env var exposed to the frontend container).

---

## Environment Variables

### New

| Variable | Default | Description |
|---|---|---|
| `ALLOWED_ORIGINS` | `http://localhost:3001` | Comma-separated allowed origins for CORS and SSE |

### Modified

| Variable | Change | Description |
|---|---|---|
| `INTERNAL_TOKEN` | Default changed from `local-dev-token-change-in-prod` to empty | Empty = dev mode (no auth), set = enforce auth |

---

## Docker Compose Changes

```yaml
api:
  environment:
    - ALLOWED_ORIGINS=${ALLOWED_ORIGINS:-http://localhost:3001}
    - INTERNAL_TOKEN=${INTERNAL_TOKEN:-}
```

Frontend (if token auth enabled):
```yaml
frontend:
  environment:
    - NEXT_PUBLIC_INTERNAL_TOKEN=${INTERNAL_TOKEN:-}
```

---

## Files Modified

| File | Change |
|---|---|
| `services/api/internal/http/router.go` | Configurable CORS, CSP header, token middleware |
| `services/api/internal/http/sse.go` | Origin validation |
| `services/api/internal/http/ratelimit.go` | Rate limit GET endpoints |
| `services/api/internal/http/operations.go` | Token auth (via middleware) |
| `services/api/internal/http/recent_events.go` | Token auth (via middleware) |
| `services/api/internal/http/capacity.go` | Token auth (via middleware) |
| `services/api/internal/http/events.go` | Refactor to shared token middleware |
| `services/api/cmd/server/main.go` | Parse ALLOWED_ORIGINS config |
| `services/watchdog/Dockerfile` | Add USER directive |
| `docker-compose.yml` | Add ALLOWED_ORIGINS, update INTERNAL_TOKEN default |
| `frontend/` | Pass INTERNAL_TOKEN header on API calls (if configured) |

---

## What This Phase Does NOT Include

- Authentication system (login, sessions, JWT) — overengineered for local-first
- BFF/reverse proxy — unnecessary additional container
- TLS/HTTPS — local Docker network, no external exposure
- Network policies — Docker Compose doesn't support them
- Secret management (Vault, SOPS) — local development project
- RBAC/authorization — single operator, no multi-tenancy

---

## Security Checklist (Post-Implementation)

After implementation, verify:

- [ ] SSE `/events` rejects requests with non-allowed Origin header
- [ ] SSE `/events` accepts requests with no Origin header (non-browser clients)
- [ ] SSE `/events` accepts requests from `http://localhost:3001`
- [ ] CORS header only set for matching origins
- [ ] `/api/operations/summary` returns 403 without token (when INTERNAL_TOKEN is set)
- [ ] `/api/operations/summary` works without token (when INTERNAL_TOKEN is empty)
- [ ] Rate limiting applies to GET `/api/*` endpoints
- [ ] Watchdog container runs as non-root user
- [ ] `Content-Security-Policy` header present in all API responses
- [ ] Frontend continues working with all changes
- [ ] `make up` works without setting INTERNAL_TOKEN (dev mode)
