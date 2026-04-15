package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/access/auth"
	ratelimit "github.com/openbotstack/openbotstack-core/access/ratelimit"
)

// mockRateLimiter implements ratelimit.RateLimiter for testing.
type mockRateLimiter struct {
	allowResult *ratelimit.RateLimitResult
	allowErr    error
	remaining   int64
	remainingErr error
	consumed     bool
	consumeErr   error
	resetCalled  bool
	resetErr     error
}

func (m *mockRateLimiter) Allow(ctx context.Context, key ratelimit.RateLimitKey) (*ratelimit.RateLimitResult, error) {
	return m.allowResult, m.allowErr
}

func (m *mockRateLimiter) Consume(ctx context.Context, key ratelimit.RateLimitKey, tokens int64) error {
	m.consumed = true
	return m.consumeErr
}

func (m *mockRateLimiter) Remaining(ctx context.Context, key ratelimit.RateLimitKey) (int64, error) {
	return m.remaining, m.remainingErr
}

func (m *mockRateLimiter) Reset(ctx context.Context, key ratelimit.RateLimitKey) error {
	m.resetCalled = true
	return m.resetErr
}

// helper to build a request with a user injected into context.
func requestWithUser(t *testing.T, path string) *http.Request {
	t.Helper()
	req := httptest.NewRequest("GET", path, nil)
	ctx := WithUser(req.Context(), &auth.User{ID: "user1", TenantID: "tenant1", Name: "Test"})
	return req.WithContext(ctx)
}

func TestRateLimitMiddleware_Allowed(t *testing.T) {
	limiter := &mockRateLimiter{
		allowResult: &ratelimit.RateLimitResult{
			Allowed:   true,
			Remaining: 42,
			ResetAt:   time.Now().Add(time.Minute),
		},
	}

	called := false
	handler := RateLimitMiddleware(limiter)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithUser(t, "/v1/chat"))

	if !called {
		t.Fatal("next handler should have been called")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	got := rec.Header().Get("X-RateLimit-Remaining")
	if got != "42" {
		t.Errorf("X-RateLimit-Remaining = %q, want %q", got, "42")
	}
}

func TestRateLimitMiddleware_Blocked(t *testing.T) {
	limiter := &mockRateLimiter{
		allowResult: &ratelimit.RateLimitResult{
			Allowed:    false,
			Remaining:  0,
			RetryAfter: 30 * time.Second,
			ResetAt:    time.Now().Add(30 * time.Second),
		},
	}

	called := false
	handler := RateLimitMiddleware(limiter)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithUser(t, "/v1/chat"))

	if called {
		t.Error("next handler should NOT have been called when blocked")
	}
	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	gotRetry := rec.Header().Get("Retry-After")
	if gotRetry != "30" {
		t.Errorf("Retry-After = %q, want %q", gotRetry, "30")
	}
}

func TestRateLimitMiddleware_NoUser_Passthrough(t *testing.T) {
	limiter := &mockRateLimiter{
		allowResult: &ratelimit.RateLimitResult{
			Allowed:   true,
			Remaining: 99,
		},
	}

	called := false
	handler := RateLimitMiddleware(limiter)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)

	// No user in context
	req := httptest.NewRequest("GET", "/v1/chat", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("next handler should have been called (passthrough) when no user")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRateLimitMiddleware_QuotaNotFound_Passthrough(t *testing.T) {
	limiter := &mockRateLimiter{
		allowErr: ratelimit.ErrQuotaNotFound,
	}

	called := false
	handler := RateLimitMiddleware(limiter)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithUser(t, "/v1/chat"))

	if !called {
		t.Fatal("next handler should have been called (passthrough) when quota not found")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRateLimitMiddleware_SkipHealthEndpoints(t *testing.T) {
	limiter := &mockRateLimiter{
		allowResult: &ratelimit.RateLimitResult{
			Allowed:    false,
			Remaining:  0,
			RetryAfter: 10 * time.Second,
		},
	}

	paths := []string{"/health", "/healthz", "/readyz", "/metrics"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			called := false
			handler := RateLimitMiddleware(limiter)(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true
					w.WriteHeader(http.StatusOK)
				}),
			)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, requestWithUser(t, path))

			if !called {
				t.Errorf("next handler should have been called for health path %q", path)
			}
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want %d for path %q", rec.Code, http.StatusOK, path)
			}
		})
	}
}

func TestRateLimitMiddleware_OtherError_Passthrough(t *testing.T) {
	limiter := &mockRateLimiter{
		allowErr: errors.New("internal rate limiter failure"),
	}

	called := false
	handler := RateLimitMiddleware(limiter)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithUser(t, "/v1/chat"))

	if !called {
		t.Fatal("next handler should have been called (passthrough) on rate limiter error")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestRateLimitMiddleware_RetryAfterSeconds(t *testing.T) {
	// Verify Retry-After is rounded to integer seconds
	limiter := &mockRateLimiter{
		allowResult: &ratelimit.RateLimitResult{
			Allowed:    false,
			Remaining:  0,
			RetryAfter: 4500 * time.Millisecond,
		},
	}

	handler := RateLimitMiddleware(limiter)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, requestWithUser(t, "/v1/chat"))

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusTooManyRequests)
	}
	gotRetry := rec.Header().Get("Retry-After")
	wantRetry := strconv.Itoa(int((4500 * time.Millisecond).Seconds()))
	if gotRetry != wantRetry {
		t.Errorf("Retry-After = %q, want %q", gotRetry, wantRetry)
	}
}
