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
| Harness Agent | Bounded execution with reasoning loop (enable with `OBS_AGENT_MODE=dual_loop`) |
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
| hello-world | Deterministic | Basic input/output |
| math-add | Deterministic | Math operations |
| wordcount | Deterministic | Word counting |
| tax-calculator | Deterministic | Tax calculation |
| sentiment | Declarative | Sentiment analysis |
| summarize | Declarative | Text summarization |
| meeting-summarize | Declarative | Meeting notes |

System-default skills in `skills/`. Example skills in `openbotstack-apps/examples/`. See the [Skill Development Guide](https://github.com/openbotstack/openbotstack-docs/blob/main/guides/SKILL_DEVELOPMENT_GUIDE.md).

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

See the [Configuration Reference](https://github.com/openbotstack/openbotstack-docs/blob/main/config/README.md) for all options.

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

Full API reference: [openbotstack-docs/api/README.md](https://github.com/openbotstack/openbotstack-docs/blob/main/api/README.md)

## Documentation

| Document | Location |
|----------|----------|
| Project Index | [openbotstack-docs/README.md](https://github.com/openbotstack/openbotstack-docs#readme) |
| Deployment Guide | [DEPLOYMENT.md](https://github.com/openbotstack/openbotstack-docs/blob/main/guides/DEPLOYMENT.md) |
| API Reference | [api/README.md](https://github.com/openbotstack/openbotstack-docs/blob/main/api/README.md) |
| Operations Manual | [OPERATIONS.md](https://github.com/openbotstack/openbotstack-docs/blob/main/guides/OPERATIONS.md) |
| Configuration | [config/README.md](https://github.com/openbotstack/openbotstack-docs/blob/main/config/README.md) |
| Harness Audit | [audit-dual-loop.md](docs/audit-dual-loop.md) |

## Contract

See [AI_CONTRACT.md](./AI_CONTRACT.md) for architectural boundaries.

## License

Apache 2.0 — see [LICENSE](./LICENSE)
