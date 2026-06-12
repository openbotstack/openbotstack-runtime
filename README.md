# openbotstack-runtime

Execution Plane for OpenBotStack — HTTP API, Wasm skill execution, LLM routing, and runtime services.

## Quick Start

```bash
# Build (requires Go 1.26.1+ and Node 20+)
make binary && ./build/openbotstack

# Or with Docker (from monorepo root)
docker compose -f openbotstack-runtime/docker-compose.yml up -d

# Open the web UI
open http://localhost:8080/ui/
```

On first run, OpenBotStack seeds a default admin API key (printed to stdout once). See the [Deployment Guide](https://github.com/openbotstack/openbotstack-docs/blob/main/guides/DEPLOYMENT.md) for details.

## Features

| Feature | Description |
|---------|-------------|
| Chat API | REST + SSE streaming (`/v1/chat`, `/v1/chat/stream`) |
| Wasm Skills | Go wasip1 command mode via wazero (no TinyGo needed) |
| Harness Agent | Bounded execution with reasoning loop (default, always active) |
| Multi-Tenant | Tenant isolation with API key and JWT auth |
| Admin API | Full CRUD for tenants, users, API keys, providers, skills |
| Admin Console | Built-in web UI at `/admin/` for provider management |
| Memory | Markdown-based memory with summary and checkpoints |
| Observability | OTel SDK + Prometheus metrics + configurable log levels |
| SQLite Storage | Zero external dependencies (no Redis, no PostgreSQL required) |
| Vector Search | Optional PostgreSQL + pgvector for semantic search |

## Architecture

```
┌──────────────────────────────────────────────────────┐
│  HTTP API                                            │
│  /v1/chat  /v1/chat/stream  /v1/skills  /v1/admin/* │
└────────────────────────┬─────────────────────────────┘
                         │
┌────────────────────────┴─────────────────────────────┐
│  Middleware                                          │
│  CORS → Auth → Rate Limit → Admin Check              │
└────────────────────────┬─────────────────────────────┘
                         │
┌────────────────────────┴─────────────────────────────┐
│  Agent / Harness                                     │
│  ExecutionHarness (task orchestration)                │
│    └── ReasoningLoop (LLM reasoning, bounded)        │
└────────────────────────┬─────────────────────────────┘
                         │
┌────────────────────────┴─────────────────────────────┐
│  Execution Layer                                     │
│  Skill Executor → Wasm Runtime (wazero)              │
│    Host API: get_input, set_output, llm_generate     │
│              kv_get, kv_set, log, http                │
└──────────────────────────────────────────────────────┘
```

## Testing

```bash
make test          # All tests (890+)
make test-race     # With race detector
make test-cover    # With coverage report
make test-wasm     # Wasm runtime only
make test-executor # Executor only
make test-skills   # Skill examples
make check         # lint + test (pre-commit)
```

See the [Testing Guide](https://github.com/openbotstack/openbotstack-docs/blob/main/guides/TESTING.md) for details.

## Skill Examples

| Skill | Type | Description |
|-------|------|-------------|
| hello-world | Wasm (wasip1) | Basic input/output |
| summarize | Declarative | Text summarization |
| classify | Declarative | Text classification |
| extract_structured_data | Declarative | Structured data extraction |

System-default skills in `skills/`. Example skills in `openbotstack-apps/examples/`. See the [Skill Development Guide](https://github.com/openbotstack/openbotstack-docs/blob/main/guides/SKILL_DEVELOPMENT_GUIDE.md).

## Configuration

Configuration is split into **static** (deploy-time, environment variables) and
**runtime-mutable** (stored in SQLite, managed via the Admin API) — per the
12-factor guidance that env vars hold deploy-time config while values that
change without a redeploy live in a data store.

### Static config (environment variables)

```bash
# Minimal — start the server, then configure the LLM provider via the Admin API.
OBS_SERVER_ADDR=:8080 \
OBS_DATABASE_PATH=data/openbotstack.db \
JWT_SECRET=$(openssl rand -hex 32) \
./openbotstack
```

| Variable | Purpose |
|----------|---------|
| `OBS_SERVER_ADDR` | HTTP listen address (default `:8080`) |
| `OBS_DATABASE_PATH` | SQLite DB path, parent dir auto-created |
| `JWT_SECRET` | JWT signing secret (≥32 chars recommended) |
| `OBS_LOG_LEVEL` | `debug` / `info` / `warn` / `error` |
| `OBS_DB_ENCRYPTION_KEY` | AES-GCM key for provider API keys at rest (optional) |
| `OBS_FILE_ALLOWED_DIRS` | Comma-separated dirs for builtin read/write tools |

### Runtime-mutable config (Admin API)

LLM providers (provider, base URL, API key, model) are **not** environment
variables — they live in SQLite and are managed at runtime. On first start the
server prints a one-time default admin API key; use it to register a provider:

```bash
# Register a provider (API key is encrypted at rest when OBS_DB_ENCRYPTION_KEY/JWT_SECRET is set)
curl -X POST http://localhost:8080/v1/admin/providers \
  -H "Authorization: Bearer obs_<admin-key-from-startup>" \
  -H "Content-Type: application/json" \
  -d '{"provider":"openai","base_url":"https://api.openai.com/v1","api_key":"sk-...","model":"gpt-4o","is_default":true}'
```

> **Breaking change:** `OBS_LLM_PROVIDER` / `OBS_LLM_API_KEY` / `OBS_LLM_URL` /
> `OBS_LLM_MODEL` are no longer read. Configure providers via the Admin API instead.

See the [Configuration Reference](https://github.com/openbotstack/openbotstack-docs/blob/main/config/README.md) for all options.

## HTTP API

| Endpoint | Description |
|----------|-------------|
| `POST /v1/chat` | Chat (JSON response) |
| `POST /v1/chat/stream` | Chat (SSE streaming, rich progress events) |
| `POST /v1/chat/completions` | **OpenAI-compatible** chat (stream + non-stream). Use any OpenAI SDK. |
| `GET /v1/skills` | List skills |
| `GET /v1/sessions` | List sessions |
| `GET /v1/sessions/{id}/history` | Session history |
| `GET /v1/admin/tenants` | Tenant CRUD |
| `GET /v1/admin/users/{id}/keys` | API key management |
| `GET /v1/admin/providers/config` | Provider configuration |
| `GET /v1/admin/skills` | Skill management |
| `GET /v1/admin/audit` | Audit logs |
| `GET /health`, `/healthz`, `/readyz` | Health checks |
| `GET /metrics` | Prometheus metrics |

Full API reference: [openbotstack-docs/api/README.md](https://github.com/openbotstack/openbotstack-docs/blob/main/api/README.md)

### OpenAI-compatible endpoint (`/v1/chat/completions`)

For third-party integration, use the OpenAI-compatible endpoint with any stock
OpenAI SDK — point `base_url` at this server and pass your API key. The last
`user` message drives the agent; internal planning/step events are **not**
exposed on this wire, only assistant content deltas (standard OpenAI shape).

```python
from openai import OpenAI
client = OpenAI(base_url="http://localhost:8080/v1", api_key="obs_<admin-key>")
resp = client.chat.completions.create(
    model="openbotstack",
    messages=[{"role": "user", "content": "Summarize this document"}],
)
print(resp.choices[0].message.content)
```

Streaming (`stream=True`) emits `chat.completion.chunk` deltas terminated by
`data: [DONE]`, identical to OpenAI's streaming contract.

## Documentation

| Document | Location |
|----------|----------|
| Project Index | [openbotstack-docs/README.md](https://github.com/openbotstack/openbotstack-docs#readme) |
| Deployment Guide | [DEPLOYMENT.md](https://github.com/openbotstack/openbotstack-docs/blob/main/guides/DEPLOYMENT.md) |
| API Reference | [api/README.md](https://github.com/openbotstack/openbotstack-docs/blob/main/api/README.md) |
| Operations Manual | [OPERATIONS.md](https://github.com/openbotstack/openbotstack-docs/blob/main/guides/OPERATIONS.md) |
| Configuration | [config/README.md](https://github.com/openbotstack/openbotstack-docs/blob/main/config/README.md) |
| Harness Audit | [audit-dual-loop.md](docs/audit-dual-loop.md) (historical) |

## Contract

See [AI_CONTRACT.md](./AI_CONTRACT.md) for architectural boundaries.

## License

Apache 2.0 — see [LICENSE](./LICENSE)
