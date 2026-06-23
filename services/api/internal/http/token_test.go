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
