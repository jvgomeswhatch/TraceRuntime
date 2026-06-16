package http

import (
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"
)

type client struct {
	tokens   float64
	last     time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	clients map[string]*client
	rate    float64
	burst   float64
}

func newRateLimiter(rps, burst int) *rateLimiter {
	rl := &rateLimiter{
		clients: make(map[string]*client),
		rate:    float64(rps),
		burst:   float64(burst),
	}
	go rl.cleanup()
	return rl
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	c, exists := rl.clients[ip]
	if !exists {
		rl.clients[ip] = &client{tokens: rl.burst - 1, last: now}
		return true
	}

	elapsed := now.Sub(c.last).Seconds()
	c.tokens += elapsed * rl.rate
	if c.tokens > rl.burst {
		c.tokens = rl.burst
	}
	c.last = now

	if c.tokens < 1 {
		return false
	}
	c.tokens--
	return true
}

func (rl *rateLimiter) cleanup() {
	for {
		time.Sleep(5 * time.Minute)
		rl.mu.Lock()
		cutoff := time.Now().Add(-10 * time.Minute)
		for ip, c := range rl.clients {
			if c.last.Before(cutoff) {
				delete(rl.clients, ip)
			}
		}
		rl.mu.Unlock()
	}
}

func RateLimitMiddleware() func(http.Handler) http.Handler {
	rps := envInt("RATE_LIMIT_RPS", 10)
	burst := envInt("RATE_LIMIT_BURST", 20)
	rl := newRateLimiter(rps, burst)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
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

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
