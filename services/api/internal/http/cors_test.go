package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/runtime-platform/services/api/internal/event"
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
	ctx, cancel := context.WithTimeout(req.Context(), 100*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code == http.StatusForbidden {
		t.Error("should allow requests with no Origin header")
	}
}
