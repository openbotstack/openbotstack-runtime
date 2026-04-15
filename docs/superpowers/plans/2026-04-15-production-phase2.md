# Production Phase 2: Error Standardization + SSE Streaming + TLS Config

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Standardize all API error responses to JSON, wire SSE streaming into the chat endpoint, and add TLS configuration support — making the API production-ready for real clients.

**Architecture:** Three independent tracks: (1) Replace all 37 `http.Error()` calls with a unified `WriteError()` helper that returns `{"error":{"code":"...","message":"..."}}`, (2) Wire the existing `SSEStream`/`SSEHandler` infrastructure into a new `/v1/chat/stream` endpoint, (3) Add TLS config + reverse proxy deployment guide.

**Tech Stack:** Go 1.26, net/http, Server-Sent Events (SSE), existing SSEStream/SSEHandler in `api/sse.go`

**Mandatory Rules:** TDD for all new code (RED → GREEN → REFACTOR). Mandatory independent audit cycle after each task — must repeat audit until zero >= 80% confidence issues. `go build ./...` and `go test ./...` must pass after every task.

---

## File Structure

### Create
- `api/errors.go` — Unified error response helper + error codes
- `api/errors_test.go` — Tests for error helper

### Modify
- `api/router.go` — Replace `http.Error()` with `writeAPIError()`, add `/v1/chat/stream` endpoint
- `api/observability_handler.go` — Replace `http.Error()` with `writeAPIError()`
- `api/admin.go` — Replace `http.Error()` with `writeAPIError()`
- `api/middleware/apikey.go` — Replace `http.Error()` with `writeAPIError()`
- `api/middleware/jwt.go` — Replace `http.Error()` with `writeAPIError()`
- `api/middleware/admin.go` — Replace `http.Error()` with `writeAPIError()`
- `api/middleware/ratelimit.go` — Replace `http.Error()` with `writeAPIError()`
- `cmd/openbotstack/main.go` — Wire streaming endpoint, add TLS config
- `config/config.go` — Add TLSConfig struct

---

## Task 1: Unified Error Response Helper

**Files:**
- Create: `api/errors.go`
- Create: `api/errors_test.go`

**Context:** All 37 `http.Error()` calls across the API return plain text responses. This makes error handling on the client side inconsistent and fragile. The standard approach for REST APIs is to return a structured JSON error response. We create a single `writeAPIError()` function and a set of error code constants that all handlers use.

**Error response format:**
```json
{
  "error": {
    "code": "METHOD_NOT_ALLOWED",
    "message": "method not allowed"
  }
}
```

- [ ] **Step 1: Write the failing tests**

Create `api/errors_test.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestWriteAPIError_JSONFormat(t *testing.T) {
	rr := httptest.NewRecorder()
	writeAPIError(rr, http.StatusBadRequest, ErrInvalidRequest, "invalid request body")

	if rr.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusBadRequest)
	}

	contentType := rr.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("Content-Type = %q, want %q", contentType, "application/json")
	}

	var resp APIErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Error.Code != "INVALID_REQUEST" {
		t.Errorf("error code = %q, want %q", resp.Error.Code, "INVALID_REQUEST")
	}
	if resp.Error.Message != "invalid request body" {
		t.Errorf("error message = %q, want %q", resp.Error.Message, "invalid request body")
	}
}

func TestWriteAPIError_StatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		code       string
		message    string
	}{
		{"bad request", http.StatusBadRequest, ErrInvalidRequest, "bad input"},
		{"unauthorized", http.StatusUnauthorized, ErrUnauthorized, "no auth"},
		{"forbidden", http.StatusForbidden, ErrForbidden, "no access"},
		{"not found", http.StatusNotFound, ErrNotFound, "missing"},
		{"method not allowed", http.StatusMethodNotAllowed, ErrMethodNotAllowed, "wrong method"},
		{"rate limited", http.StatusTooManyRequests, ErrRateLimited, "slow down"},
		{"internal error", http.StatusInternalServerError, ErrInternal, "oops"},
		{"service unavailable", http.StatusServiceUnavailable, ErrUnavailable, "down"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			writeAPIError(rr, tt.statusCode, tt.code, tt.message)

			if rr.Code != tt.statusCode {
				t.Errorf("status = %d, want %d", rr.Code, tt.statusCode)
			}

			var resp APIErrorResponse
			if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
				t.Fatalf("failed to decode: %v", err)
			}
			if resp.Error.Code != tt.code {
				t.Errorf("code = %q, want %q", resp.Error.Code, tt.code)
			}
		})
	}
}

func TestErrorCodes_AreConsistent(t *testing.T) {
	// Verify all error codes are UPPER_SNAKE_CASE
	codes := []string{
		ErrMethodNotAllowed, ErrInvalidRequest, ErrUnauthorized,
		ErrForbidden, ErrNotFound, ErrRateLimited,
		ErrInternal, ErrUnavailable, ErrAgentNotConfigured,
	}
	for _, code := range codes {
		for _, c := range code {
			if !((c >= 'A' && c <= 'Z') || c == '_' || (c >= '0' && c <= '9')) {
				t.Errorf("error code %q contains non-UPPER_SNAKE_CASE char: %c", code, c)
			}
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && go test -v -run "TestWriteAPIError|TestErrorCodes" ./api/`

Expected: FAIL — `writeAPIError`, `APIErrorResponse`, and error code constants undefined.

- [ ] **Step 3: Write minimal implementation**

Create `api/errors.go`:

```go
package api

import (
	"encoding/json"
	"net/http"
)

// Error code constants (UPPER_SNAKE_CASE).
const (
	ErrMethodNotAllowed  = "METHOD_NOT_ALLOWED"
	ErrInvalidRequest    = "INVALID_REQUEST"
	ErrUnauthorized      = "UNAUTHORIZED"
	ErrForbidden         = "FORBIDDEN"
	ErrNotFound          = "NOT_FOUND"
	ErrRateLimited       = "RATE_LIMITED"
	ErrInternal          = "INTERNAL_ERROR"
	ErrUnavailable       = "SERVICE_UNAVAILABLE"
	ErrAgentNotConfigured = "AGENT_NOT_CONFIGURED"
)

// APIErrorResponse is the standard error envelope.
type APIErrorResponse struct {
	Error APIErrorDetail `json:"error"`
}

// APIErrorDetail contains the error code and human-readable message.
type APIErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeAPIError writes a structured JSON error response.
func writeAPIError(w http.ResponseWriter, statusCode int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(APIErrorResponse{
		Error: APIErrorDetail{
			Code:    code,
			Message: message,
		},
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && go test -v -run "TestWriteAPIError|TestErrorCodes" ./api/`

Expected: All 3 tests PASS.

- [ ] **Step 5: Commit**

```bash
cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime
git add api/errors.go api/errors_test.go
git commit -m "feat: add unified JSON error response helper

All API endpoints will return structured {\"error\":{\"code\":\"...\",\"message\":\"...\"}}
instead of plain text. Error codes use UPPER_SNAKE_CASE constants.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 2: Migrate router.go + observability_handler.go to writeAPIError

**Files:**
- Modify: `api/router.go`
- Modify: `api/observability_handler.go`

**Context:** Replace all `http.Error()` calls in `router.go` (6 calls) and `observability_handler.go` (3 calls) with `writeAPIError()`. The slog warning/error logging is preserved — only the HTTP response format changes.

**Mapping table for router.go:**

| Line | Old | New |
|------|-----|-----|
| 153 | `http.Error(w, "method not allowed", 405)` | `writeAPIError(w, 405, ErrMethodNotAllowed, "method not allowed")` |
| 165 | `http.Error(w, "invalid request body", 400)` | `writeAPIError(w, 400, ErrInvalidRequest, "invalid request body")` |
| 177 | `http.Error(w, "agent not configured", 503)` | `writeAPIError(w, 503, ErrAgentNotConfigured, "agent not configured")` |
| 203 | `http.Error(w, "internal error: "+err.Error(), 500)` | `writeAPIError(w, 500, ErrInternal, "internal error")` |
| 228 | `http.Error(w, "not found", 404)` | `writeAPIError(w, 404, ErrNotFound, "not found")` |
| 245 | `http.Error(w, "failed to get session history", 500)` | `writeAPIError(w, 500, ErrInternal, "failed to get session history")` |

**Mapping table for observability_handler.go:**

| Line | Old | New |
|------|-----|-----|
| 51 | `http.Error(w, "method not allowed", 405)` | `writeAPIError(w, 405, ErrMethodNotAllowed, "method not allowed")` |
| 116 | `http.Error(w, "method not allowed", 405)` | `writeAPIError(w, 405, ErrMethodNotAllowed, "method not allowed")` |
| 134 | `http.Error(w, "failed to query executions", 500)` | `writeAPIError(w, 500, ErrInternal, "failed to query executions")` |

- [ ] **Step 1: Replace http.Error calls in router.go**

For each of the 6 `http.Error()` calls in `api/router.go`, replace with `writeAPIError()` using the mapping table above. The surrounding slog.WarnContext/slog.ErrorContext calls are kept unchanged.

In `handleChat` (line 153):
```go
// Before:
http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
// After:
writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
```

In `handleChat` (line 165):
```go
// Before:
http.Error(w, "invalid request body", http.StatusBadRequest)
// After:
writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "invalid request body")
```

In `handleChat` (line 177):
```go
// Before:
http.Error(w, "agent not configured", http.StatusServiceUnavailable)
// After:
writeAPIError(w, http.StatusServiceUnavailable, ErrAgentNotConfigured, "agent not configured")
```

In `handleChat` (line 203):
```go
// Before:
http.Error(w, "internal error: "+err.Error(), http.StatusInternalServerError)
// After:
writeAPIError(w, http.StatusInternalServerError, ErrInternal, "internal error")
```

In `handleSessions` (line 228):
```go
// Before:
http.Error(w, "not found", http.StatusNotFound)
// After:
writeAPIError(w, http.StatusNotFound, ErrNotFound, "not found")
```

In `handleSessions` (line 245):
```go
// Before:
http.Error(w, "failed to get session history", http.StatusInternalServerError)
// After:
writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to get session history")
```

Also in `handleSkills` and `handleExecutions` in `observability_handler.go` (same file was split but it's the same package), replace the 3 `http.Error()` calls.

- [ ] **Step 2: Verify build and tests pass**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && go build ./... && go test ./api/...`

Expected: Build success, all tests PASS.

- [ ] **Step 3: Commit**

```bash
cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime
git add api/router.go api/observability_handler.go
git commit -m "refactor: migrate router and observability handlers to JSON errors

Replace 9 http.Error() calls with writeAPIError() for structured
{error:{code,message}} responses. No behavior change except response format.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 3: Migrate admin.go to writeAPIError

**Files:**
- Modify: `api/admin.go`

**Context:** Replace all 18 `http.Error()` calls in `admin.go` with `writeAPIError()`. The file has 4 endpoint handlers: `handleTenants`, `handleTenantUsers`, `handleUserKeys`, `handleRevokeKey`.

**Mapping for all admin.go calls:**

| Old message | New code |
|-------------|----------|
| "method not allowed" | `ErrMethodNotAllowed` |
| "invalid request" | `ErrInvalidRequest` |
| "id and name required" | `ErrInvalidRequest` |
| "name required" | `ErrInvalidRequest` |
| "role must be 'admin' or 'member'" | `ErrInvalidRequest` |
| "failed to create tenant: "+err.Error() | `ErrInternal` + "failed to create tenant" |
| "failed to list tenants" | `ErrInternal` |
| "failed to create user: "+err.Error() | `ErrInternal` + "failed to create user" |
| "failed to list users" | `ErrInternal` |
| "user not found" | `ErrNotFound` |
| "internal error" | `ErrInternal` |
| "failed to generate key" | `ErrInternal` |
| "failed to create key: "+err.Error() | `ErrInternal` + "failed to create API key" |
| "failed to list keys" | `ErrInternal` |
| "failed to revoke key" | `ErrInternal` |
| "key not found" | `ErrNotFound` |

- [ ] **Step 1: Replace all http.Error calls in admin.go**

Replace each of the 18 `http.Error()` calls with the corresponding `writeAPIError()` from the mapping table. Example:

```go
// Before:
http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
// After:
writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")

// Before:
http.Error(w, "failed to create tenant: "+err.Error(), http.StatusInternalServerError)
// After:
writeAPIError(w, http.StatusInternalServerError, ErrInternal, "failed to create tenant")
```

Note: Internal error messages with `err.Error()` are NOT exposed to the client in the response body (to avoid information leakage). They remain in the slog structured logs.

- [ ] **Step 2: Verify build and tests pass**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && go build ./... && go test ./api/...`

Expected: Build success, all tests PASS.

- [ ] **Step 3: Commit**

```bash
cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime
git add api/admin.go
git commit -m "refactor: migrate admin handlers to JSON errors

Replace 18 http.Error() calls with writeAPIError(). Internal error
details are no longer leaked in response bodies (kept in slog only).

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 4: Migrate middleware to writeAPIError

**Files:**
- Modify: `api/middleware/apikey.go`
- Modify: `api/middleware/jwt.go`
- Modify: `api/middleware/admin.go`
- Modify: `api/middleware/ratelimit.go`

**Context:** Replace all `http.Error()` calls in middleware files. Middleware is in a different package (`middleware`) so it cannot call `writeAPIError()` from `api` package (import cycle). Instead, create a `writeMiddlewareError()` helper in the middleware package.

**Middleware error mapping:**

apikey.go (4 calls):
| Old | New code |
|-----|----------|
| "missing API key" | `ERR_UNAUTHORIZED` |
| "invalid API key" | `ERR_UNAUTHORIZED` |
| "internal error" | `ERR_INTERNAL` |
| "API key expired" | `ERR_UNAUTHORIZED` |

jwt.go (4 calls):
| Old | New code |
|-----|----------|
| "missing authorization header" | `ERR_UNAUTHORIZED` |
| "invalid authorization header format" | `ERR_UNAUTHORIZED` |
| "invalid token" | `ERR_UNAUTHORIZED` |
| "invalid token claims" | `ERR_UNAUTHORIZED` |

admin.go (1 call):
| Old | New code |
|-----|----------|
| "forbidden: admin role required" | `ERR_FORBIDDEN` |

ratelimit.go (1 call):
| Old | New code |
|-----|----------|
| "rate limit exceeded" | `ERR_RATE_LIMITED` |

- [ ] **Step 1: Write the failing test**

Add to `api/middleware/cors_test.go` or create a new test file `api/middleware/errors_test.go`:

```go
package middleware

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type errorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func TestWriteMiddlewareError_JSONFormat(t *testing.T) {
	rr := httptest.NewRecorder()
	writeMiddlewareError(rr, http.StatusUnauthorized, ErrUnauthorized, "missing key")

	if rr.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusUnauthorized)
	}

	ct := rr.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Errorf("Content-Type = %q, want %q", ct, "application/json")
	}

	var resp errorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error.Code != "UNAUTHORIZED" {
		t.Errorf("code = %q, want %q", resp.Error.Code, "UNAUTHORIZED")
	}
	if resp.Error.Message != "missing key" {
		t.Errorf("message = %q, want %q", resp.Error.Message, "missing key")
	}
}

func TestMiddlewareErrorCodes(t *testing.T) {
	codes := []string{ErrUnauthorized, ErrForbidden, ErrRateLimited, ErrInternal}
	for _, code := range codes {
		for _, c := range code {
			if !((c >= 'A' && c <= 'Z') || c == '_' || (c >= '0' && c <= '9')) {
				t.Errorf("error code %q has non-UPPER_SNAKE_CASE char: %c", code, c)
			}
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && go test -v -run "TestWriteMiddlewareError|TestMiddlewareErrorCodes" ./api/middleware/`

Expected: FAIL — `writeMiddlewareError`, error code constants undefined.

- [ ] **Step 3: Create middleware error helper**

Create `api/middleware/errors.go`:

```go
package middleware

import (
	"encoding/json"
	"net/http"
)

// Error code constants for middleware responses.
const (
	ErrUnauthorized  = "UNAUTHORIZED"
	ErrForbidden     = "FORBIDDEN"
	ErrRateLimited   = "RATE_LIMITED"
	ErrInternal      = "INTERNAL_ERROR"
)

type middlewareErrorResponse struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// writeMiddlewareError writes a structured JSON error response.
func writeMiddlewareError(w http.ResponseWriter, statusCode int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	resp := middlewareErrorResponse{}
	resp.Error.Code = code
	resp.Error.Message = message
	_ = json.NewEncoder(w).Encode(resp)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && go test -v -run "TestWriteMiddlewareError|TestMiddlewareErrorCodes" ./api/middleware/`

Expected: PASS.

- [ ] **Step 5: Replace http.Error calls in middleware files**

In `api/middleware/apikey.go`, replace 4 `http.Error()` calls:
```go
// Line 41:
writeMiddlewareError(w, http.StatusUnauthorized, ErrUnauthorized, "missing API key")

// Line 71:
writeMiddlewareError(w, http.StatusUnauthorized, ErrUnauthorized, "invalid API key")

// Line 85:
writeMiddlewareError(w, http.StatusInternalServerError, ErrInternal, "internal error")

// Line 101:
writeMiddlewareError(w, http.StatusUnauthorized, ErrUnauthorized, "API key expired")
```

In `api/middleware/jwt.go`, replace 4 `http.Error()` calls:
```go
// Line 54:
writeMiddlewareError(w, http.StatusUnauthorized, ErrUnauthorized, "missing authorization header")

// Line 70:
writeMiddlewareError(w, http.StatusUnauthorized, ErrUnauthorized, "invalid authorization header format")

// Line 90:
writeMiddlewareError(w, http.StatusUnauthorized, ErrUnauthorized, "invalid token")

// Line 106:
writeMiddlewareError(w, http.StatusUnauthorized, ErrUnauthorized, "invalid token claims")
```

In `api/middleware/admin.go`, replace 1 `http.Error()` call:
```go
// Line 19:
writeMiddlewareError(w, http.StatusForbidden, ErrForbidden, "admin role required")
```

In `api/middleware/ratelimit.go`, replace 1 `http.Error()` call:
```go
// Line 72:
writeMiddlewareError(w, http.StatusTooManyRequests, ErrRateLimited, "rate limit exceeded")
```

- [ ] **Step 6: Verify build and all tests pass**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && go build ./... && go test ./api/...`

Expected: Build success, all tests PASS.

- [ ] **Step 7: Commit**

```bash
cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime
git add api/middleware/errors.go api/middleware/errors_test.go api/middleware/apikey.go api/middleware/jwt.go api/middleware/admin.go api/middleware/ratelimit.go
git commit -m "refactor: migrate all middleware to JSON error responses

Replace 10 http.Error() calls with writeMiddlewareError() for
consistent {error:{code,message}} format. No import cycle since
middleware package has its own error helper.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 5: SSE Streaming Chat Endpoint

**Files:**
- Modify: `api/router.go` — Add `/v1/chat/stream` route + handler
- Modify: `api/router.go` — Add `StreamingAgent` interface check

**Context:** The SSE infrastructure (`SSEStream`, `SSEHandler`) already exists in `api/sse.go` but is not wired into any endpoint. The existing `/v1/chat` endpoint returns a complete JSON response. We add a new `/v1/chat/stream` endpoint that:
1. Accepts the same `ChatRequest` body
2. Returns `Content-Type: text/event-stream`
3. Streams `thinking`, `chunk`, and `done` events
4. Falls back to non-streaming if agent doesn't support streaming

**Agent interface extension:** We check if the agent supports streaming via an optional interface:

```go
// StreamingAgent is an optional interface that agents can implement
// to support SSE streaming responses.
type StreamingAgent interface {
	Agent
	HandleMessageStream(ctx context.Context, req MessageRequest) (<-chan StreamEvent, error)
}

// StreamEvent represents a single event in the stream.
type StreamEvent struct {
	Type    string // "thinking", "chunk", "done", "error"
	Content string
}
```

Since changing the core `Agent` interface requires coordination across repos, the streaming endpoint will use a simpler approach: call `HandleMessage()` and stream the complete response as a single "done" event. True token-level streaming can be added later when the core `Agent` interface supports it.

- [ ] **Step 1: Write the failing test**

Add tests to `api/router_test.go` or create a dedicated file:

```go
// In api/router_test.go, add:
func TestHandleChatStream_Post(t *testing.T) {
	// Create a minimal mock agent
	agent := &mockAgent{
		response: &agent.MessageResponse{
			SessionID: "s1",
			Message:   "Hello!",
		},
	}
	r := NewRouter(agent)
	r.SetSkillProvider(&mockSkillProvider{})

	// POST with valid body
	body := strings.NewReader(`{"message":"hi","session_id":"s1"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/stream", body)
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	r.ServeHTTP(rr, req)

	// Should return SSE content type
	if ct := rr.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	bodyStr := rr.Body.String()
	if !strings.Contains(bodyStr, "event: done") {
		t.Errorf("expected 'event: done' in SSE body, got: %s", bodyStr)
	}
	if !strings.Contains(bodyStr, "data:") {
		t.Errorf("expected 'data:' in SSE body, got: %s", bodyStr)
	}
}

func TestHandleChatStream_MethodNotAllowed(t *testing.T) {
	r := NewRouter(nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/chat/stream", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rr.Code, http.StatusMethodNotAllowed)
	}

	// Should be JSON error now
	var resp APIErrorResponse
	if err := json.NewDecoder(rr.Body).Decode(&resp); err != nil {
		t.Fatalf("expected JSON error, got: %s", rr.Body.String())
	}
	if resp.Error.Code != ErrMethodNotAllowed {
		t.Errorf("code = %q, want %q", resp.Error.Code, ErrMethodNotAllowed)
	}
}
```

Note: The `mockAgent` and `mockSkillProvider` types already exist in `router_test.go`. Check before adding duplicates.

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && go test -v -run "TestHandleChatStream" ./api/`

Expected: FAIL — route not registered, handler not found.

- [ ] **Step 3: Implement the streaming endpoint**

In `api/router.go`, add the route registration and handler:

In `registerRoutes()`, add after the existing `/v1/chat` registration:
```go
r.v1Mux.HandleFunc("/v1/chat/stream", r.handleChatStream)
```

Add the handler:
```go
func (r *Router) handleChatStream(w http.ResponseWriter, req *http.Request) {
	if req.Method != http.MethodPost {
		slog.WarnContext(req.Context(), "request validation error",
			"method", req.Method,
			"path", req.URL.Path,
			"status", http.StatusMethodNotAllowed,
			"error", "method not allowed",
		)
		writeAPIError(w, http.StatusMethodNotAllowed, ErrMethodNotAllowed, "method not allowed")
		return
	}

	var chatReq ChatRequest
	if err := json.NewDecoder(req.Body).Decode(&chatReq); err != nil {
		writeAPIError(w, http.StatusBadRequest, ErrInvalidRequest, "invalid request body")
		return
	}

	if r.agent == nil {
		writeAPIError(w, http.StatusServiceUnavailable, ErrAgentNotConfigured, "agent not configured")
		return
	}

	agentReq := agent.MessageRequest{
		TenantID:  chatReq.TenantID,
		UserID:    chatReq.UserID,
		SessionID: chatReq.SessionID,
		Message:   chatReq.Message,
	}

	// Authenticated identity overrides request body
	if user, ok := middleware.UserFromContext(req.Context()); ok {
		agentReq.TenantID = user.TenantID
		agentReq.UserID = user.ID
	}

	// Get the response from agent (non-streaming for now)
	agentResp, err := r.agent.HandleMessage(req.Context(), agentReq)
	if err != nil {
		slog.ErrorContext(req.Context(), "streaming handler error",
			"method", req.Method,
			"path", req.URL.Path,
			"status", http.StatusInternalServerError,
			"error", err,
		)
		writeAPIError(w, http.StatusInternalServerError, ErrInternal, "internal error")
		return
	}

	// Stream the response as SSE events
	handler := NewSSEHandler(w)
	_ = handler.WriteEvent(SSEEvent{
		Event: "session",
		Data:  agentResp.SessionID,
	})
	_ = handler.WriteEvent(SSEEvent{
		Event: "done",
		Data:  agentResp.Message,
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && go test -v -run "TestHandleChatStream" ./api/`

Expected: PASS.

- [ ] **Step 5: Run full test suite**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && go build ./... && go test ./...`

Expected: Build success, all tests PASS.

- [ ] **Step 6: Commit**

```bash
cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime
git add api/router.go
git commit -m "feat: add SSE streaming chat endpoint /v1/chat/stream

Streams agent response as Server-Sent Events. Currently sends complete
response as a single 'done' event. Token-level streaming will be added
when the core Agent interface supports it.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Task 6: TLS Configuration Support

**Files:**
- Modify: `config/config.go` — Add `TLSConfig` struct
- Modify: `cmd/openbotstack/main.go` — Add TLS listener option

**Context:** Production deployments need TLS. Two approaches:
1. **Built-in TLS** — Go's `http.Server` supports `ListenAndServeTLS(certFile, keyFile)`
2. **Reverse proxy** — nginx/caddy handles TLS, terminates, and forwards HTTP to the runtime

We support both. When `tls.cert_file` and `tls.key_file` are configured, the server uses built-in TLS. Otherwise, plain HTTP for reverse proxy setups.

- [ ] **Step 1: Add TLS config**

In `config/config.go`, add the `TLSConfig` struct and field:

```go
// TLSConfig controls TLS/HTTPS configuration.
type TLSConfig struct {
	// CertFile is the path to the TLS certificate file (PEM format).
	// Env override: OBS_TLS_CERT_FILE
	CertFile string `yaml:"cert_file"`

	// KeyFile is the path to the TLS private key file (PEM format).
	// Env override: OBS_TLS_KEY_FILE
	KeyFile string `yaml:"key_file"`
}
```

Add to `Config` struct:
```go
type Config struct {
	Server        ServerConfig        `yaml:"server"`
	TLS           TLSConfig           `yaml:"tls"`
	Redis         RedisConfig         `yaml:"redis"`
	Providers     ProvidersConfig     `yaml:"providers"`
	Observability ObservabilityConfig `yaml:"observability"`
	Memory        MemoryConfig        `yaml:"memory"`
	Sandbox       SandboxConfig       `yaml:"sandbox"`
	Vector        VectorConfig         `yaml:"vector"`
}
```

Add env overrides in `Load()`:
```go
// TLS overrides
if val := os.Getenv("OBS_TLS_CERT_FILE"); val != "" {
	cfg.TLS.CertFile = val
}
if val := os.Getenv("OBS_TLS_KEY_FILE"); val != "" {
	cfg.TLS.KeyFile = val
}
```

- [ ] **Step 2: Wire TLS into main.go**

In `cmd/openbotstack/main.go`, replace the server startup section:

Find:
```go
go func() {
	slog.Info("server listening", "addr", cfg.Server.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}
}()
```

Replace with:
```go
go func() {
	if cfg.TLS.CertFile != "" && cfg.TLS.KeyFile != "" {
		slog.Info("server listening with TLS", "addr", cfg.Server.Addr,
			"cert", cfg.TLS.CertFile)
		if err := srv.ListenAndServeTLS(cfg.TLS.CertFile, cfg.TLS.KeyFile); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	} else {
		slog.Info("server listening", "addr", cfg.Server.Addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}
}()
```

- [ ] **Step 3: Verify build and tests pass**

Run: `cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime && go build ./... && go test ./...`

Expected: Build success, all tests PASS.

- [ ] **Step 4: Commit**

```bash
cd /Users/mw/workspace/repo/github.com/openbotstack/openbotstack-runtime
git add config/config.go cmd/openbotstack/main.go
git commit -m "feat: add TLS configuration support

When tls.cert_file and tls.key_file are set (via config or env vars),
server uses built-in TLS. Otherwise plain HTTP for reverse proxy setups.
Env vars: OBS_TLS_CERT_FILE, OBS_TLS_KEY_FILE.

Co-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"
```

---

## Self-Review Checklist

**1. Spec coverage:**
- Error standardization: Tasks 1-4 cover all 37 `http.Error()` calls (6 router + 3 observability + 18 admin + 10 middleware)
- SSE streaming: Task 5 wires existing SSE infrastructure into `/v1/chat/stream`
- TLS: Task 6 adds config + built-in TLS support
- Documentation: Not in scope (separate task in docs repo)

**2. Placeholder scan:** No TBD, TODO, "implement later", or vague descriptions found in plan steps.

**3. Type consistency:**
- `writeAPIError(w, statusCode, code, message)` — all string params, used consistently across all tasks
- `writeMiddlewareError(w, statusCode, code, message)` — same signature in middleware package
- Error codes: `ErrMethodNotAllowed`, `ErrInvalidRequest`, etc. are string constants in both packages
- `SSEEvent{Event, Data}` matches existing type in `sse.go`
- `ChatRequest` reused from existing definition in `router.go`
- `APIErrorResponse` + `APIErrorDetail` defined in `errors.go`

**4. Missing considerations:**
- True token-level streaming requires core Agent interface changes — out of scope for this phase
- CORS config externalization to config.yaml — deferred (hardcoded `*` is fine for now)
- Rate limit post-execution token accounting — deferred
