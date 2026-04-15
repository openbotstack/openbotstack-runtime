# OpenBotStack API Reference

## 1. Overview

OpenBotStack exposes a REST API for chat interactions, skill management, execution history, and administrative operations.

| Property | Value |
|---|---|
| Base URL | `http://localhost:8080` (default) |
| Content-Type | `application/json` for request and response bodies |
| Streaming | Server-Sent Events (`text/event-stream`) for `/v1/chat/stream` |

All request and response bodies are JSON unless otherwise noted. Timestamps use RFC 3339 Nano format.

---

## 2. Authentication

All `/v1/` endpoints support two authentication methods. The server attempts API Key authentication first, then falls back to JWT Bearer. In strict mode, unauthenticated requests are rejected with `401 UNAUTHORIZED`.

### API Key (preferred)

Include the API key in a header:

```
X-API-Key: obs_<your-api-key>
```

The server hashes the key with SHA256 and looks it up in the database. Keys are tenant-scoped and can be revoked or set to expire.

### JWT Bearer (fallback)

Include a JWT token in the Authorization header:

```
Authorization: Bearer <jwt-token>
```

The JWT is verified against the server's secret key. Expected claims:

| Claim | Description |
|---|---|
| `sub` or `user_id` | User identifier |
| `tenant_id` | Tenant the user belongs to |
| `name` | Display name |
| `role` | `admin` or `member` |

### Authenticated Identity Override

When a request is authenticated, the user's `tenant_id` and `user_id` from the auth context override any values in the request body. This prevents identity spoofing.

### Admin Endpoints

Admin endpoints require the authenticated user to have the `admin` role. Requests from non-admin users receive `403 FORBIDDEN`.

---

## 3. Error Responses

All errors follow a consistent JSON structure:

```json
{
  "error": {
    "code": "ERROR_CODE",
    "message": "Human-readable message"
  }
}
```

### Error Codes

| Code | HTTP Status | Description |
|---|---|---|
| `METHOD_NOT_ALLOWED` | 405 | Wrong HTTP method for this endpoint |
| `INVALID_REQUEST` | 400 | Malformed or invalid request body |
| `UNAUTHORIZED` | 401 | Missing or invalid credentials |
| `FORBIDDEN` | 403 | Authenticated but lacks required role |
| `NOT_FOUND` | 404 | Requested resource does not exist |
| `RATE_LIMITED` | 429 | Rate limit exceeded for this tenant/user |
| `INTERNAL_ERROR` | 500 | Unexpected server-side failure |
| `SERVICE_UNAVAILABLE` | 503 | A required component is unhealthy |
| `AGENT_NOT_CONFIGURED` | 503 | No agent is configured to handle requests |

---

## 4. Rate Limiting

Rate limiting is applied per tenant/user identity extracted from the authentication context.

| Header | Direction | Description |
|---|---|---|
| `X-RateLimit-Remaining` | Response | Number of requests remaining in the current window |
| `Retry-After` | Response (429) | Seconds until the rate limit window resets |

**Bypassed endpoints:** `/health`, `/healthz`, `/readyz`, `/metrics` are not rate limited.

If no quota is configured for a tenant, requests pass through without rate limiting. If the rate limiter itself fails, the server fails open (allows the request).

---

## 5. Endpoints

### 5.1 Chat Endpoints

#### POST /v1/chat

Send a message and receive a JSON response.

**Auth:** API Key or JWT Bearer

**Request body:**

```json
{
  "tenant_id": "tenant-1",
  "user_id": "user-1",
  "session_id": "sess-abc123",
  "message": "Summarize the quarterly report"
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `tenant_id` | string | No* | Tenant identifier (overridden by auth) |
| `user_id` | string | No* | User identifier (overridden by auth) |
| `session_id` | string | No | Existing session to continue; omit to start new session |
| `message` | string | Yes | The user message |

*Fields are overridden by authenticated identity when present.

**Success response (200):**

```json
{
  "session_id": "sess-abc123",
  "message": "Here is the summary of the quarterly report...",
  "skill_used": "summarize"
}
```

| Field | Type | Description |
|---|---|---|
| `session_id` | string | The session identifier (new or existing) |
| `message` | string | The agent's response |
| `skill_used` | string | Skill that was selected (omitted if none) |

**Possible error codes:** `INVALID_REQUEST`, `AGENT_NOT_CONFIGURED`, `INTERNAL_ERROR`

**curl example:**

```bash
curl -X POST http://localhost:8080/v1/chat \
  -H "Content-Type: application/json" \
  -H "X-API-Key: obs_your_api_key_here" \
  -d '{
    "session_id": "sess-abc123",
    "message": "Summarize the quarterly report"
  }'
```

---

#### POST /v1/chat/stream

Send a message and receive the response as a Server-Sent Events stream.

**Auth:** API Key or JWT Bearer

**Request body:** Same as `POST /v1/chat`.

**Success response (200):** Content-Type `text/event-stream`

See [Section 6: SSE Stream Format](#6-sse-stream-format) for the event structure.

**Possible error codes:** `INVALID_REQUEST`, `AGENT_NOT_CONFIGURED`, `INTERNAL_ERROR`

**curl example:**

```bash
curl -X POST http://localhost:8080/v1/chat/stream \
  -H "Content-Type: application/json" \
  -H "X-API-Key: obs_your_api_key_here" \
  -d '{
    "message": "Summarize the quarterly report"
  }'
```

---

### 5.2 Information Endpoints

#### GET /v1/skills

List all registered skills.

**Auth:** API Key or JWT Bearer

**Request body:** None

**Success response (200):**

```json
[
  {
    "id": "summarize",
    "name": "Summarize",
    "description": "Summarizes text content",
    "type": "llm",
    "input_schema": { ... },
    "output_schema": { ... },
    "version": "1.0.0",
    "enabled": true
  }
]
```

| Field | Type | Description |
|---|---|---|
| `id` | string | Unique skill identifier |
| `name` | string | Human-readable name |
| `description` | string | What the skill does |
| `type` | string | `wasm`, `llm`, or `code` |
| `input_schema` | object | JSON Schema for input (omitted if nil) |
| `output_schema` | object | JSON Schema for output (omitted if nil) |
| `version` | string | Skill version |
| `enabled` | boolean | Whether the skill is active |

**Possible error codes:** `METHOD_NOT_ALLOWED`

**curl example:**

```bash
curl http://localhost:8080/v1/skills \
  -H "X-API-Key: obs_your_api_key_here"
```

---

#### GET /v1/executions

List recent skill executions (up to 50).

**Auth:** API Key or JWT Bearer

**Request body:** None

**Success response (200):**

```json
[
  {
    "execution_id": "exec-001",
    "session_id": "sess-abc123",
    "skill_id": "summarize",
    "duration_ms": 234,
    "status": "success"
  }
]
```

| Field | Type | Description |
|---|---|---|
| `execution_id` | string | Unique execution identifier |
| `session_id` | string | Session this execution belongs to |
| `skill_id` | string | Skill that was executed |
| `duration_ms` | integer | Execution duration in milliseconds |
| `status` | string | `success`, `failure`, or `error` |
| `error` | string | Error message (only on failure/error, omitted if successful) |

**Possible error codes:** `INTERNAL_ERROR`

**curl example:**

```bash
curl http://localhost:8080/v1/executions \
  -H "X-API-Key: obs_your_api_key_here"
```

---

#### GET /v1/sessions/{sessionID}/history

Get conversation history for a session.

**Auth:** API Key or JWT Bearer

**Path parameters:**

| Parameter | Description |
|---|---|
| `sessionID` | The session identifier |

**Request body:** None

**Success response (200):**

```json
{
  "session_id": "sess-abc123",
  "messages": [
    { "role": "user", "content": "Summarize the report" },
    { "role": "assistant", "content": "Here is the summary..." }
  ]
}
```

| Field | Type | Description |
|---|---|---|
| `session_id` | string | Session identifier |
| `messages` | array | Ordered list of messages |
| `messages[].role` | string | `user` or `assistant` |
| `messages[].content` | string | Message text |

**Possible error codes:** `NOT_FOUND`, `INTERNAL_ERROR`

**curl example:**

```bash
curl http://localhost:8080/v1/sessions/sess-abc123/history \
  -H "X-API-Key: obs_your_api_key_here"
```

---

### 5.3 Admin Endpoints

All admin endpoints require an authenticated user with the `admin` role.

#### POST /v1/admin/tenants

Create a new tenant.

**Auth:** Admin

**Request body:**

```json
{
  "id": "acme-corp",
  "name": "Acme Corporation"
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | Unique tenant identifier |
| `name` | string | Yes | Human-readable tenant name |

**Success response (201):**

```json
{
  "id": "acme-corp",
  "name": "Acme Corporation",
  "created_at": "2025-06-15T10:30:00.123456789Z"
}
```

**Possible error codes:** `INVALID_REQUEST`, `INTERNAL_ERROR`

**curl example:**

```bash
curl -X POST http://localhost:8080/v1/admin/tenants \
  -H "Content-Type: application/json" \
  -H "X-API-Key: obs_admin_api_key_here" \
  -d '{"id": "acme-corp", "name": "Acme Corporation"}'
```

---

#### GET /v1/admin/tenants

List all tenants.

**Auth:** Admin

**Request body:** None

**Success response (200):**

```json
[
  {
    "id": "acme-corp",
    "name": "Acme Corporation",
    "created_at": "2025-06-15T10:30:00.123456789Z"
  }
]
```

Returns an empty array `[]` when no tenants exist.

**Possible error codes:** `INTERNAL_ERROR`

**curl example:**

```bash
curl http://localhost:8080/v1/admin/tenants \
  -H "X-API-Key: obs_admin_api_key_here"
```

---

#### POST /v1/admin/tenants/{tenantID}/users

Create a user under a tenant.

**Auth:** Admin

**Path parameters:**

| Parameter | Description |
|---|---|
| `tenantID` | The tenant to add the user to |

**Request body:**

```json
{
  "id": "user-42",
  "name": "Jane Doe",
  "role": "member"
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string | Yes | Unique user identifier |
| `name` | string | Yes | Display name |
| `role` | string | No | `admin` or `member` (defaults to `member`) |

**Success response (201):**

```json
{
  "id": "user-42",
  "tenant_id": "acme-corp",
  "name": "Jane Doe",
  "role": "member",
  "created_at": "2025-06-15T11:00:00.123456789Z"
}
```

**Possible error codes:** `INVALID_REQUEST`, `INTERNAL_ERROR`

**curl example:**

```bash
curl -X POST http://localhost:8080/v1/admin/tenants/acme-corp/users \
  -H "Content-Type: application/json" \
  -H "X-API-Key: obs_admin_api_key_here" \
  -d '{"id": "user-42", "name": "Jane Doe", "role": "member"}'
```

---

#### GET /v1/admin/tenants/{tenantID}/users

List users belonging to a tenant.

**Auth:** Admin

**Path parameters:**

| Parameter | Description |
|---|---|
| `tenantID` | The tenant to list users for |

**Request body:** None

**Success response (200):**

```json
[
  {
    "id": "user-42",
    "tenant_id": "acme-corp",
    "name": "Jane Doe",
    "role": "member",
    "created_at": "2025-06-15T11:00:00.123456789Z"
  }
]
```

Returns an empty array `[]` when no users exist for the tenant.

**Possible error codes:** `INTERNAL_ERROR`

**curl example:**

```bash
curl http://localhost:8080/v1/admin/tenants/acme-corp/users \
  -H "X-API-Key: obs_admin_api_key_here"
```

---

#### POST /v1/admin/users/{userID}/keys

Create an API key for a user.

**Auth:** Admin

**Path parameters:**

| Parameter | Description |
|---|---|
| `userID` | The user to create a key for |

**Request body:**

```json
{
  "name": "production-key"
}
```

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | Yes | Descriptive name for this key |

**Success response (201):**

```json
{
  "id": "key-a1b2c3d4e5f6a1b2",
  "key": "obs_a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4",
  "key_prefix": "obs_a1b2c3d4",
  "name": "production-key",
  "created_at": "2025-06-15T12:00:00.123456789Z"
}
```

The full `key` value is only returned once at creation time. Store it securely.

| Field | Type | Description |
|---|---|---|
| `id` | string | Key identifier (used for revocation) |
| `key` | string | Full API key (only returned here) |
| `key_prefix` | string | First 12 characters (for identification) |
| `name` | string | The name you provided |
| `created_at` | string | Creation timestamp |

**Possible error codes:** `INVALID_REQUEST`, `NOT_FOUND`, `INTERNAL_ERROR`

**curl example:**

```bash
curl -X POST http://localhost:8080/v1/admin/users/user-42/keys \
  -H "Content-Type: application/json" \
  -H "X-API-Key: obs_admin_api_key_here" \
  -d '{"name": "production-key"}'
```

---

#### GET /v1/admin/users/{userID}/keys

List API keys for a user. The full key value is never returned; only the prefix is shown.

**Auth:** Admin

**Path parameters:**

| Parameter | Description |
|---|---|
| `userID` | The user to list keys for |

**Request body:** None

**Success response (200):**

```json
[
  {
    "id": "key-a1b2c3d4e5f6a1b2",
    "key_prefix": "obs_a1b2c3d4",
    "name": "production-key",
    "created_at": "2025-06-15T12:00:00.123456789Z",
    "revoked": false
  }
]
```

Returns an empty array `[]` when no keys exist.

**Possible error codes:** `INTERNAL_ERROR`

**curl example:**

```bash
curl http://localhost:8080/v1/admin/users/user-42/keys \
  -H "X-API-Key: obs_admin_api_key_here"
```

---

#### DELETE /v1/admin/keys/{keyID}

Revoke an API key. Revoked keys immediately stop working.

**Auth:** Admin

**Path parameters:**

| Parameter | Description |
|---|---|
| `keyID` | The key identifier (from key creation or listing) |

**Request body:** None

**Success response (200):**

```json
{
  "id": "key-a1b2c3d4e5f6a1b2",
  "revoked": true
}
```

**Possible error codes:** `NOT_FOUND`, `INTERNAL_ERROR`

**curl example:**

```bash
curl -X DELETE http://localhost:8080/v1/admin/keys/key-a1b2c3d4e5f6a1b2 \
  -H "X-API-Key: obs_admin_api_key_here"
```

---

### 5.4 Infrastructure Endpoints

These endpoints do not require authentication and are not rate limited.

#### GET /health / GET /healthz

Simple health check indicating the server is running.

**Auth:** None

**Request body:** None

**Success response (200):**

```json
{
  "status": "healthy"
}
```

**curl example:**

```bash
curl http://localhost:8080/health
```

---

#### GET /readyz

Readiness check that verifies all configured dependencies (e.g., LLM provider, Redis).

**Auth:** None

**Request body:** None

**Success response (200):**

```json
{
  "status": "healthy",
  "version": "0.1.0",
  "commit": "abc1234",
  "branch": "main",
  "build_time": "2025-06-15T09:00:00Z",
  "go_version": "go1.26.1",
  "components": {
    "llm_provider": {
      "status": "healthy",
      "duration_ms": 45
    }
  },
  "checked_at": "2025-06-15T10:30:00.123456789Z"
}
```

When any component is unhealthy, the overall `status` is `unhealthy` and the response uses HTTP 503. When components are slow but working, `status` is `degraded` and the response is HTTP 200.

| Field | Type | Description |
|---|---|---|
| `status` | string | `healthy`, `degraded`, or `unhealthy` |
| `version` | string | Build version |
| `commit` | string | Git commit hash |
| `branch` | string | Git branch |
| `build_time` | string | Build timestamp |
| `go_version` | string | Go runtime version |
| `components` | object | Per-component health details |
| `components[].status` | string | `healthy`, `degraded`, or `unhealthy` |
| `components[].duration_ms` | integer | Check latency in milliseconds |
| `components[].error` | string | Error message (omitted when healthy) |
| `checked_at` | string | Timestamp of this check |

**curl example:**

```bash
curl http://localhost:8080/readyz
```

---

#### GET /version

Return build version information.

**Auth:** None

**Request body:** None

**Success response (200):**

```json
{
  "version": "0.1.0",
  "commit": "abc1234",
  "branch": "main",
  "build_time": "2025-06-15T09:00:00Z",
  "go_version": "go1.26.1"
}
```

**curl example:**

```bash
curl http://localhost:8080/version
```

---

#### GET /metrics

Prometheus-format metrics endpoint.

**Auth:** None

**Request body:** None

**Success response (200):** Content-Type `text/plain; version=0.0.4; charset=utf-8`

```
# HELP openbotstack_requests_total Total number of HTTP requests received.
# TYPE openbotstack_requests_total counter
openbotstack_requests_total 142

# HELP openbotstack_requests_errored_total Total number of HTTP requests that resulted in errors.
# TYPE openbotstack_requests_errored_total counter
openbotstack_requests_errored_total 3
```

**curl example:**

```bash
curl http://localhost:8080/metrics
```

---

## 6. SSE Stream Format

The `POST /v1/chat/stream` endpoint returns a `text/event-stream` response. The server sends two events:

### Event: session

Sent first. Contains the session identifier.

```
event: session
data: sess-abc123

```

### Event: done

Sent second. Contains the agent's response message.

```
event: done
data: Here is the summary of the quarterly report...

```

### Full SSE Example

```
event: session
data: sess-abc123

event: done
data: Here is the summary of the quarterly report...

```

### SSE Response Headers

```
Content-Type: text/event-stream
Cache-Control: no-cache
Connection: keep-alive
```

Multi-line response data is split into multiple `data:` lines per the SSE specification.
