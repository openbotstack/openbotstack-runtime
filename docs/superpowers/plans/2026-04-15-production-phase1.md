# Production Phase 1: Dead Code Cleanup + Rate Limiting + CORS

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove dead code, wire rate limiting, add CORS — making the system production-safe and deployment-ready.

**Architecture:** Three independent tasks: (1) Delete V1-era dead code that confuses maintainers and bloats the binary, (2) Wire the existing SQLiteRateLimiter into HTTP middleware chain, (3) Add CORS headers for web UI compatibility. Each task is independently testable.

**Tech Stack:** Go 1.26, SQLite (modernc.org/sqlite), net/http middleware chain

**Mandatory Rules:** TDD for all new code (RED → GREEN → REFACTOR). Mandatory independent audit cycle after each task — must repeat audit until zero >= 80% confidence issues. `go build ./...` and `go test ./...` must pass after every task.

---

## File Structure

### Delete
- `runtime/assistant_runtime.go` — V1 legacy, calls non-existent ModelProvider.Stream()
- `runtime/api/chat.go` — V1 legacy ChatServer, depends on runtime.AssistantRuntime
- `runtime/llm/openai_provider.go` — V1 legacy provider
- `runtime/memory/vector_store.go` — V1 legacy vector store
- `api/handler.go` — Dead ChatHandler, not wired to Router
- `api/handler_test.go` — Tests for dead ChatHandler
- `memory/milvus_store.go` — Deprecated, replaced by PgVectorStore
- `memory/embedder.go` — Deprecated, replaced by EmbeddingService
- `memory/memory_manager.go` — Deprecated, replaced by MarkdownMemoryBridge

### Create
- `api/middleware/ratelimit.go` — Rate limiting HTTP middleware
- `api/middleware/ratelimit_test.go` — Tests for rate limit middleware
- `api/middleware/cors.go` — CORS middleware
- `api/middleware/cors_test.go` — Tests for CORS middleware

### Modify
- `cmd/openbotstack/main.go` — Wire rate limiter + CORS into middleware chain
- `docs/ROADMAP.md` (in openbotstack-docs) — Mark Phase 2-3 complete

---

## Task 1: Delete Dead Code — V1 runtime/ Package

**Files:**
- Delete: `runtime/assistant_runtime.go`
- Delete: `runtime/api/chat.go`
- Delete: `runtime/llm/openai_provider.go`
- Delete: `runtime/memory/vector_store.go`

**Context:** The `runtime/` package (under `openbotstack-runtime/runtime/`) is V1-era code that is never imported by `cmd/openbotstack/main.go`. It defines its own `AssistantRuntime`, `ModelProvider`, `ChatServer` types that are superseded by the real implementations in the top-level packages. The only cross-reference is `runtime/api/chat.go` importing `runtime/llm` and `runtime/memory` — all internal to this dead package.

- [ ] **Step 1: Verify no external imports of runtime/ package**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && grep -r '"github.com/openbotstack/openbotstack-runtime/runtime"' --include='*.go' | grep -v 'runtime/' | grep -v '_test.go'`

Expected: Zero matches outside the runtime/ package itself.

- [ ] **Step 2: Delete the runtime/ package files**

Run: `rm -rf runtime/runtime/`

Note: This removes `runtime/assistant_runtime.go`, `runtime/api/`, `runtime/llm/`, `runtime/memory/`. The `runtime/workflow/` directory should be checked — if it exists and is also unused, delete it too.

- [ ] **Step 3: Verify build passes**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && go build ./...`

Expected: Success, no compilation errors.

- [ ] **Step 4: Verify all tests pass**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && go test ./...`

Expected: All tests PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime
git add -A
git commit -m "chore: remove V1-era runtime/ package (dead code)

The runtime/ sub-package was V1-era code superseded by top-level
implementations. No external imports existed outside the package itself.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 2: Delete Dead Code — api/handler.go ChatHandler

**Files:**
- Delete: `api/handler.go`
- Delete: `api/handler_test.go`

**Context:** `ChatHandler` in `api/handler.go` has its own in-memory `sessions` map and a `Process()` method, but it is never wired into the Router. The Router's `/v1/chat` route delegates directly to `agent.HandleMessage()` in `router.go:194`. The ChatHandler's TODO at line 57 ("Integrate with agent execution and model providers") is stale — the real integration is in `router.go`.

- [ ] **Step 1: Verify ChatHandler is not used in router.go**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && grep -n 'ChatHandler\|NewChatHandler' api/router.go`

Expected: Zero matches.

- [ ] **Step 2: Verify ChatHandler is only referenced in its own test file**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && grep -rn 'ChatHandler\|NewChatHandler' --include='*.go' | grep -v handler_test.go | grep -v handler.go`

Expected: Zero matches (excluding the definition and test files).

- [ ] **Step 3: Delete the files**

Run: `rm api/handler.go api/handler_test.go`

- [ ] **Step 4: Verify build and tests pass**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && go build ./... && go test ./api/...`

Expected: Build success, all API tests PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime
git add -A
git commit -m "chore: remove dead ChatHandler (superseded by Router+Agent)

api/handler.go was never wired into the Router. The real /v1/chat
handler delegates to agent.HandleMessage() in router.go.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 3: Delete Dead Code — Deprecated Memory Files

**Files:**
- Delete: `memory/milvus_store.go` (119 lines, deprecated → PgVectorStore)
- Delete: `memory/embedder.go` (112 lines, deprecated → EmbeddingService)
- Delete: `memory/memory_manager.go` (162 lines, deprecated → MarkdownMemoryBridge)

**Context:** All three files are marked with `Deprecated:` comments and have been replaced by new implementations. `milvus_store.go` → `vector_store.go` (PgVectorStore), `embedder.go` → `embedding_service.go` (EmbeddingService), `memory_manager.go` → `manager_bridge.go` (MarkdownMemoryBridge).

- [ ] **Step 1: Verify no production imports of deprecated types**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && grep -rn 'MilvusStore\|LocalEmbedder\|NewMemoryManager\|LLMSummarizer' --include='*.go' | grep -v '_test.go' | grep -v 'milvus_store\|embedder\|memory_manager' | grep -v 'Deprecated'`

Expected: Zero matches in production code (excluding the deprecated files themselves).

- [ ] **Step 2: Check if any test files import deprecated types**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && grep -rn 'MilvusStore\|LocalEmbedder\|NewMemoryManager' --include='*_test.go'`

Expected: Zero matches. If matches exist, those tests must be updated first.

- [ ] **Step 3: Delete the deprecated files**

Run: `rm memory/milvus_store.go memory/embedder.go memory/memory_manager.go`

- [ ] **Step 4: Verify build and tests pass**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && go build ./... && go test ./memory/...`

Expected: Build success, all memory tests PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime
git add -A
git commit -m "chore: remove deprecated memory stubs (milvus, embedder, memory_manager)

Replaced by PgVectorStore, EmbeddingService, and MarkdownMemoryBridge.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 4: Rate Limiting Middleware

**Files:**
- Create: `api/middleware/ratelimit.go`
- Create: `api/middleware/ratelimit_test.go`

**Context:** `SQLiteRateLimiter` and `SQLiteQuotaStore` exist in `ratelimit/` but are not wired into HTTP. The middleware extracts tenant/user ID from context (set by auth middleware) and checks the rate limiter before passing to the next handler.

**Interface dependencies:**
- `core/access/ratelimit.RateLimiter` — `Allow(ctx, RateLimitKey) (*RateLimitResult, error)`
- `core/access/ratelimit.RateLimitKey` — `{TenantID, UserID, SkillID}`
- `core/access/ratelimit.ErrQuotaNotFound` — no quota configured → allow all
- `api/middleware.UserFromContext(ctx)` — returns `(*auth.User, bool)`

- [ ] **Step 1: Write the failing test**

Create `api/middleware/ratelimit_test.go`:

```go
package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openbotstack/openbotstack-core/access/auth"
	ratelimit "github.com/openbotstack/openbotstack-core/access/ratelimit"
)

// mockRateLimiter implements ratelimit.RateLimiter for testing.
type mockRateLimiter struct {
	allowed   bool
	remaining int64
	err       error
}

func (m *mockRateLimiter) Allow(_ context.Context, _ ratelimit.RateLimitKey) (*ratelimit.RateLimitResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &ratelimit.RateLimitResult{
		Allowed:   m.allowed,
		Remaining: m.remaining,
	}, nil
}

func (m *mockRateLimiter) Consume(_ context.Context, _ ratelimit.RateLimitKey, _ int64) error {
	return nil
}
func (m *mockRateLimiter) Remaining(_ context.Context, _ ratelimit.RateLimitKey) (int64, error) {
	return m.remaining, nil
}
func (m *mockRateLimiter) Reset(_ context.Context, _ ratelimit.RateLimitKey) error { return nil }

func TestRateLimitMiddleware_Allowed(t *testing.T) {
	limiter := &mockRateLimiter{allowed: true, remaining: 99}
	called := false
	handler := RateLimitMiddleware(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	// Set user in context (simulating auth middleware)
	user := &auth.User{ID: "u1", TenantID: "t1", Name: "test"}
	ctx := WithUser(context.Background(), user)
	req := httptest.NewRequest("POST", "/v1/chat", nil).WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("expected next handler to be called")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
	// Check rate limit headers
	if rr.Header().Get("X-RateLimit-Remaining") != "99" {
		t.Errorf("expected X-RateLimit-Remaining 99, got %q", rr.Header().Get("X-RateLimit-Remaining"))
	}
}

func TestRateLimitMiddleware_Blocked(t *testing.T) {
	limiter := &mockRateLimiter{allowed: false, remaining: 0}
	called := false
	handler := RateLimitMiddleware(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))

	user := &auth.User{ID: "u1", TenantID: "t1", Name: "test"}
	ctx := WithUser(context.Background(), user)
	req := httptest.NewRequest("POST", "/v1/chat", nil).WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if called {
		t.Error("expected next handler NOT to be called when rate limited")
	}
	if rr.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rr.Code)
	}
}

func TestRateLimitMiddleware_NoUser_Passthrough(t *testing.T) {
	// Without user in context, rate limiter should pass through
	limiter := &mockRateLimiter{allowed: true, remaining: 50}
	called := false
	handler := RateLimitMiddleware(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/v1/chat", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("expected passthrough when no user in context")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}

func TestRateLimitMiddleware_QuotaNotFound_Passthrough(t *testing.T) {
	// When no quota is configured for tenant, allow through
	limiter := &mockRateLimiter{err: ratelimit.ErrQuotaNotFound}
	called := false
	handler := RateLimitMiddleware(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	user := &auth.User{ID: "u1", TenantID: "t1", Name: "test"}
	ctx := WithUser(context.Background(), user)
	req := httptest.NewRequest("POST", "/v1/chat", nil).WithContext(ctx)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("expected passthrough when quota not found")
	}
}

func TestRateLimitMiddleware_SkipHealthEndpoints(t *testing.T) {
	limiter := &mockRateLimiter{allowed: false, remaining: 0}
	called := false
	handler := RateLimitMiddleware(limiter)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	for _, path := range []string{"/health", "/healthz", "/readyz", "/metrics"} {
		called = false
		req := httptest.NewRequest("GET", path, nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if !called {
			t.Errorf("expected %s to bypass rate limiting", path)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && go test -v -run TestRateLimitMiddleware ./api/middleware/`

Expected: FAIL — `RateLimitMiddleware` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `api/middleware/ratelimit.go`:

```go
package middleware

import (
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/openbotstack/openbotstack-core/access/auth"
	ratelimit "github.com/openbotstack/openbotstack-core/access/ratelimit"
)

// RateLimitMiddleware creates middleware that enforces rate limits based on
// tenant and user identity from the request context.
// Health and metrics endpoints are always allowed through.
// If no user is in context or no quota is configured, the request passes through.
func RateLimitMiddleware(limiter ratelimit.RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip rate limiting for infrastructure endpoints
			path := r.URL.Path
			if path == "/health" || path == "/healthz" || path == "/readyz" || path == "/metrics" {
				next.ServeHTTP(w, r)
				return
			}

			// Extract user from context (set by auth middleware)
			user, ok := UserFromContext(r.Context())
			if !ok || user.TenantID == "" {
				// No authenticated user — pass through (auth middleware handles rejection)
				next.ServeHTTP(w, r)
				return
			}

			key := ratelimit.RateLimitKey{
				TenantID: user.TenantID,
				UserID:   user.ID,
			}

			result, err := limiter.Allow(r.Context(), key)
			if err != nil {
				if err == ratelimit.ErrQuotaNotFound {
					// No quota configured for this tenant — allow all
					next.ServeHTTP(w, r)
					return
				}
				slog.ErrorContext(r.Context(), "rate limit check failed",
					"error", err, "path", path)
				next.ServeHTTP(w, r)
				return
			}

			// Set rate limit headers
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(result.Remaining, 10))

			if !result.Allowed {
				slog.WarnContext(r.Context(), "rate limit exceeded",
					"tenant_id", user.TenantID, "user_id", user.ID, "path", path)
				w.Header().Set("Retry-After", strconv.Itoa(int(result.RetryAfter.Seconds())))
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && go test -v -run TestRateLimitMiddleware ./api/middleware/`

Expected: All 5 tests PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime
git add api/middleware/ratelimit.go api/middleware/ratelimit_test.go
git commit -m "feat: add rate limiting HTTP middleware

Checks SQLite-backed rate limiter per tenant/user. Passes through
for health endpoints, unauthenticated requests, and tenants without
quota config. Returns 429 with Retry-After header when limited.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 5: CORS Middleware

**Files:**
- Create: `api/middleware/cors.go`
- Create: `api/middleware/cors_test.go`

**Context:** The Web UI is served from `/ui/` but API calls go to `/v1/`. In production with different origins, CORS headers are required. The middleware should be configurable via config or environment variable.

- [ ] **Step 1: Write the failing test**

Create `api/middleware/cors_test.go`:

```go
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSMiddleware_SetsHeaders(t *testing.T) {
	handler := CORSMiddleware(CORSConfig{
		AllowedOrigins:   []string{"https://example.com"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "X-API-Key", "Authorization"},
		AllowCredentials: true,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("POST", "/v1/chat", nil)
	req.Header.Set("Origin", "https://example.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "https://example.com" {
		t.Errorf("expected ACAO 'https://example.com', got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
	if rr.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Error("expected Allow-Credentials true")
	}
}

func TestCORSMiddleware_PreflightOptions(t *testing.T) {
	handler := CORSMiddleware(CORSConfig{
		AllowedOrigins:   []string{"https://example.com"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Content-Type", "X-API-Key"},
		AllowCredentials: true,
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("next handler should not be called for OPTIONS")
	}))

	req := httptest.NewRequest("OPTIONS", "/v1/chat", nil)
	req.Header.Set("Origin", "https://example.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Errorf("expected 204 for preflight, got %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Methods") != "GET, POST, OPTIONS" {
		t.Errorf("unexpected Allow-Methods: %q", rr.Header().Get("Access-Control-Allow-Methods"))
	}
}

func TestCORSMiddleware_WildcardOrigin(t *testing.T) {
	handler := CORSMiddleware(CORSConfig{
		AllowedOrigins: []string{"*"},
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/v1/skills", nil)
	req.Header.Set("Origin", "https://any-site.com")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Header().Get("Access-Control-Allow-Origin") != "*" {
		t.Errorf("expected wildcard origin, got %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}
}

func TestCORSMiddleware_NoOrigin(t *testing.T) {
	called := false
	handler := CORSMiddleware(CORSConfig{
		AllowedOrigins: []string{"https://example.com"},
	})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	// Request without Origin header (non-browser client)
	req := httptest.NewRequest("POST", "/v1/chat", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if !called {
		t.Error("should still call next handler without Origin")
	}
	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rr.Code)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && go test -v -run TestCORSMiddleware ./api/middleware/`

Expected: FAIL — `CORSMiddleware` undefined.

- [ ] **Step 3: Write minimal implementation**

Create `api/middleware/cors.go`:

```go
package middleware

import (
	"net/http"
	"strings"
)

// CORSConfig controls Cross-Origin Resource Sharing behavior.
type CORSConfig struct {
	AllowedOrigins   []string
	AllowedMethods   []string
	AllowedHeaders   []string
	AllowCredentials bool
}

// CORSMiddleware adds CORS headers to responses.
// For preflight OPTIONS requests, responds with 204 No Content.
// For requests without an Origin header, passes through without CORS headers.
func CORSMiddleware(config CORSConfig) func(http.Handler) http.Handler {
	methods := strings.Join(config.AllowedMethods, ", ")
	headers := strings.Join(config.AllowedHeaders, ", ")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")
			if origin == "" {
				next.ServeHTTP(w, r)
				return
			}

			// Set CORS headers
			if len(config.AllowedOrigins) == 1 && config.AllowedOrigins[0] == "*" {
				w.Header().Set("Access-Control-Allow-Origin", "*")
			} else {
				for _, allowed := range config.AllowedOrigins {
					if origin == allowed {
						w.Header().Set("Access-Control-Allow-Origin", origin)
						break
					}
				}
			}

			if config.AllowCredentials {
				w.Header().Set("Access-Control-Allow-Credentials", "true")
			}
			if methods != "" {
				w.Header().Set("Access-Control-Allow-Methods", methods)
			}
			if headers != "" {
				w.Header().Set("Access-Control-Allow-Headers", headers)
			}

			// Handle preflight
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && go test -v -run TestCORSMiddleware ./api/middleware/`

Expected: All 4 tests PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime
git add api/middleware/cors.go api/middleware/cors_test.go
git commit -m "feat: add CORS middleware for web UI compatibility

Supports wildcard and specific origins, preflight OPTIONS handling,
and configurable methods/headers. Passes through for non-browser clients.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 6: Wire Rate Limiting + CORS into main.go

**Files:**
- Modify: `cmd/openbotstack/main.go`

**Context:** The middleware chain in main.go is currently:
```
otelhttp → MetricsMiddleware → CorrelationMiddleware → mux → auth → handlers
```

After this task, it becomes:
```
otelhttp → MetricsMiddleware → CorrelationMiddleware → CORS → mux → auth → RateLimit → handlers
```

CORS is outermost (before mux) so preflight requests are handled before routing.
Rate limit is between auth and handlers so it has access to tenant/user identity.

The `SQLiteRateLimiter` already exists in `ratelimit/sqlite_limiter.go`. The `SQLiteQuotaStore` is already created in main.go (line 227) but ignored (`_ =`). This task wires them together.

- [ ] **Step 1: Write the failing test**

There is no direct way to TDD main.go wiring. Instead, verify the integration works via the existing full system test.

- [ ] **Step 2: Modify main.go to wire rate limiter and CORS**

In `cmd/openbotstack/main.go`, make these changes:

**a) Replace the ignored quota store with a real rate limiter:**

Find: `_ = ratelimit.NewSQLiteQuotaStore(pdb.DB) // wired when rate limiting middleware is added`

Replace with:
```go
quotaStore := ratelimit.NewSQLiteQuotaStore(pdb.DB)
rateLimiter := ratelimit.NewSQLiteRateLimiter(pdb.DB, quotaStore)
```

**b) Add CORS middleware before mux:**

Find the section where `correlationHandler` and `metricsHandler` are defined (around line 378-382). After `metricsHandler` and before the `otelhttp.NewHandler`, insert CORS:

```go
corsHandler := middleware.CORSMiddleware(middleware.CORSConfig{
    AllowedOrigins:   []string{"*"}, // Restrict in production via config
    AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
    AllowedHeaders:   []string{"Content-Type", "X-API-Key", "Authorization"},
    AllowCredentials: true,
})(metricsHandler)
```

Then use `corsHandler` instead of `metricsHandler` in the otelhttp chain.

**c) Add rate limiting after auth for v1 routes:**

Find: `mux.Handle("/v1/", apiRouter)`

Replace with:
```go
rateLimitMW := middleware.RateLimitMiddleware(rateLimiter)
mux.Handle("/v1/", rateLimitMW(apiRouter))
```

**d) Add rate limit import:**

Ensure `middleware` package is imported (it already is for auth).

- [ ] **Step 3: Verify build and all tests pass**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && go build ./... && go test ./...`

Expected: Build success, all tests PASS.

- [ ] **Step 4: Commit**

```bash
cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime
git add cmd/openbotstack/main.go
git commit -m "feat: wire rate limiting and CORS into HTTP middleware chain

Rate limiting uses SQLiteRateLimiter with tenant/user scope from auth
context. CORS allows all origins for development (restrict via config
for production). Health/metrics endpoints bypass rate limiting.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Self-Review Checklist

**1. Spec coverage:**
- Dead code cleanup: Tasks 1-3 cover all identified dead code (runtime/, handler.go, deprecated memory files)
- Rate limiting: Task 4 (middleware) + Task 6 (wiring) cover the full integration
- CORS: Task 5 (middleware) + Task 6 (wiring) cover the full integration

**2. Placeholder scan:** No TBD, TODO, "implement later", or vague descriptions found in plan steps.

**3. Type consistency:**
- `mockRateLimiter` implements all 4 methods of `ratelimit.RateLimiter` interface (Allow, Consume, Remaining, Reset)
- `RateLimitKey{TenantID, UserID}` matches the struct defined in `core/access/ratelimit/limiter.go`
- `UserFromContext` returns `(*auth.User, bool)` matching `jwt.go:21`
- `CORSConfig` struct is defined in `cors.go` and used in both test and main.go

**4. Missing considerations:**
- CORS config should be externalized via config.yaml in a follow-up (hardcoded `*` for now)
- Rate limit consume (post-execution token accounting) is deferred to a follow-up
- Default quota seeding for the default tenant is handled by the existing `SeedDefaults()` flow
