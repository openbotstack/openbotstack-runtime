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

On first run, OpenBotStack seeds a default admin API key (printed to stdout once). See [Deployment Guide](../openbotstack-docs/guides/DEPLOYMENT.md) for details.

## Features

| Feature | Description |
|---------|-------------|
| Chat API | REST + SSE streaming (`/v1/chat`, `/v1/chat/stream`) |
| Wasm Skills | Go wasip1 command mode via wazero (no TinyGo needed) |
| Dual Loop Agent | Bounded outer/inner loop kernel (enable with `OBS_AGENT_MODE=dual_loop`) |
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
│  Agent / Dual Loop                                   │
│  Outer Loop (task orchestration)                      │
│    └── Inner Loop (reasoning + tool calls, bounded)   │
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
make test          # All tests (440+)
make test-race     # With race detector
make test-cover    # With coverage report
make test-wasm     # Wasm runtime only
make test-executor # Executor only
make test-skills   # Skill examples
make check         # lint + test (pre-commit)
```

See [Testing Guide](../openbotstack-docs/guides/TESTING.md) for details.

## Skill Examples

| Skill | Type | Description |
|-------|------|-------------|
| hello-world | Deterministic | Basic input/output |
| math-add | Deterministic | Math operations |
| wordcount | Deterministic | Word counting |
| tax-calculator | Deterministic | Tax calculation |
| sentiment | LLM-Assisted | Sentiment analysis |
| summarize | Declarative | Text summarization |
| meeting-summarize | Declarative | Meeting notes |

All examples in `examples/skills/`. See [Skill Development Guide](../openbotstack-docs/guides/SKILL_DEVELOPMENT_GUIDE.md).

## Configuration

```bash
# Minimal (API key only)
OBS_LLM_API_KEY=sk-... ./openbotstack

# With self-hosted provider
OBS_LLM_PROVIDER=openai \
OBS_LLM_URL=https://my-llm.example.com/v1 \
OBS_LLM_API_KEY=sk-... \
OBS_LLM_MODEL=Qwen3.6-35B \
./openbotstack
```

See [Configuration Reference](../openbotstack-docs/config/README.md) for all options.

## HTTP API

| Endpoint | Description |
|----------|-------------|
| `POST /v1/chat` | Chat (JSON response) |
| `POST /v1/chat/stream` | Chat (SSE streaming) |
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

Full API reference: [docs/api/README.md](../openbotstack-docs/api/README.md)

## Documentation

| Document | Location |
|----------|----------|
| Deployment Guide | [openbotstack-docs/guides/DEPLOYMENT.md](../openbotstack-docs/guides/DEPLOYMENT.md) |
| API Reference | [openbotstack-docs/api/README.md](../openbotstack-docs/api/README.md) |
| Operations Manual | [openbotstack-docs/guides/OPERATIONS.md](../openbotstack-docs/guides/OPERATIONS.md) |
| Configuration | [openbotstack-docs/config/README.md](../openbotstack-docs/config/README.md) |
| Dual Loop Audit | [docs/audit-dual-loop.md](docs/audit-dual-loop.md) |

## Contract

See [AI_CONTRACT.md](./AI_CONTRACT.md) for architectural boundaries.

## License

MIT
