---
name: security:rate-limiter
description: Implementa sliding window rate limiter em Go sem dependências externas (sem Redis). Thread-safe, com cleanup automático de entradas antigas. Pronto para uso como middleware HTTP.
---

# Skill: security:rate-limiter

## Input necessário
1. Onde aplicar: middleware global, por rota, ou por serviço?
2. Limite (ex: 100 requests por minuto por IP)
3. Comportamento ao exceder: 429 imediato ou enfileirar?

## O que gerar

### `internal/security/ratelimit.go`
```go
package security

import (
    "net"
    "net/http"
    "sync"
    "time"
)

type RateLimiter struct {
    mu       sync.Mutex
    clients  map[string]*client
    limit    int
    window   time.Duration
    stopCleanup chan struct{}
}

type client struct {
    requests []time.Time
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
    rl := &RateLimiter{
        clients:     make(map[string]*client),
        limit:       limit,
        window:      window,
        stopCleanup: make(chan struct{}),
    }
    go rl.cleanupLoop()
    return rl
}

func (rl *RateLimiter) Allow(ip string) bool {
    rl.mu.Lock()
    defer rl.mu.Unlock()

    c, ok := rl.clients[ip]
    if !ok {
        c = &client{}
        rl.clients[ip] = c
    }

    now := time.Now()
    cutoff := now.Add(-rl.window)

    // Remove requests fora da janela
    valid := c.requests[:0]
    for _, t := range c.requests {
        if t.After(cutoff) {
            valid = append(valid, t)
        }
    }
    c.requests = valid

    if len(c.requests) >= rl.limit {
        return false
    }

    c.requests = append(c.requests, now)
    return true
}

// Middleware HTTP
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        ip, _, err := net.SplitHostPort(r.RemoteAddr)
        if err != nil {
            ip = r.RemoteAddr
        }

        if !rl.Allow(ip) {
            w.Header().Set("Retry-After", "60")
            http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
            return
        }

        next.ServeHTTP(w, r)
    })
}

// Cleanup periódico — evita crescimento ilimitado do map
func (rl *RateLimiter) cleanupLoop() {
    ticker := time.NewTicker(5 * time.Minute)
    defer ticker.Stop()
    for {
        select {
        case <-ticker.C:
            rl.cleanup()
        case <-rl.stopCleanup:
            return
        }
    }
}

func (rl *RateLimiter) cleanup() {
    rl.mu.Lock()
    defer rl.mu.Unlock()
    cutoff := time.Now().Add(-rl.window)
    for ip, c := range rl.clients {
        allOld := true
        for _, t := range c.requests {
            if t.After(cutoff) {
                allOld = false
                break
            }
        }
        if allOld {
            delete(rl.clients, ip)
        }
    }
}

func (rl *RateLimiter) Stop() {
    close(rl.stopCleanup)
}
```

### Uso no main.go
```go
limiter := security.NewRateLimiter(100, time.Minute) // 100 req/min por IP
defer limiter.Stop()

mux := http.NewServeMux()
mux.Handle("/", limiter.Middleware(router))
```

## Checklist pós-geração
- [ ] `cleanupLoop` iniciado para evitar memory leak
- [ ] `Stop()` chamado no graceful shutdown
- [ ] Header `Retry-After` presente na resposta 429
- [ ] Limites configuráveis via env (não hardcoded)
- [ ] Teste unitário com race detector: `go test -race ./internal/security/...`
