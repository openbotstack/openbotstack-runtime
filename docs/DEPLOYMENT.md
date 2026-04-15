# Deployment Guide

This guide covers deploying OpenBotStack Runtime in production: from a single binary to Docker and TLS termination.

## Quick Start

### Binary

Download the latest release for your platform, or build from source:

```bash
# Build from source (requires Go 1.23+ and Node 20+)
cd web && npm ci && npm run build && cd ..
go build -o openbotstack ./cmd/openbotstack

# Run with environment variables
OBS_LLM_API_KEY=sk-... ./openbotstack
```

On first run, OpenBotStack:

1. Creates `openbotstack.db` (SQLite) in the current directory
2. Creates `./data/memory/` for markdown-based memory storage
3. Seeds a default tenant, admin user, and API key (printed once to stdout)
4. Starts listening on `:8080`

Open `http://localhost:8080/ui/` for the web interface.

### Docker

```bash
# Build from the parent directory (includes openbotstack-core for go.mod replace)
cd ..  # from openbotstack-runtime/
docker compose -f openbotstack-runtime/docker-compose.yml up -d
```

See the Docker Deployment section below for full details.

## Docker Deployment

### Build

The Dockerfile uses a multi-stage build:

1. **Frontend**: Node 22 builds React + Vite into `web/webui/dist/`
2. **Backend**: Go 1.23 compiles a static binary with embedded frontend
3. **Runtime**: Minimal Alpine image with ca-certificates

Because `go.mod` has a `replace` directive pointing to `../openbotstack-core`, the build context must include both repositories. Build from the parent directory:

```bash
# From the monorepo root (openbotstack/)
docker build -t openbotstack:latest -f openbotstack-runtime/Dockerfile .
```

Or use docker compose from the runtime directory:

```bash
cd openbotstack-runtime
docker compose build
```

### Run

```bash
docker compose up -d

# Check health
docker compose exec openbotstack wget -qO- http://localhost:8080/health

# View logs
docker compose logs -f openbotstack

# Stop
docker compose down
```

The default admin API key is printed to stdout on first startup. Retrieve it with:

```bash
docker compose logs openbotstack 2>&1 | grep "Default admin API Key" -A1
```

### Persistent Data

All persistent data lives in the `/app/data` volume:

- `openbotstack.db` - SQLite database (tenants, users, API keys, audit logs)
- `memory/` - Markdown-based memory files

The docker-compose.yml mounts a named volume `openbotstack-data` at `/app/data`.

## TLS Configuration

Three approaches for HTTPS:

### Option 1: Built-in TLS

Set environment variables to enable HTTPS directly in OpenBotStack:

```bash
OBS_TLS_CERT_FILE=/etc/tls/tls.crt \
OBS_TLS_KEY_FILE=/etc/tls/tls.key \
./openbotstack
```

With Docker, mount the certificate directory:

```yaml
# docker-compose.yml
volumes:
  - ./tls:/etc/tls:ro
environment:
  - OBS_TLS_CERT_FILE=/etc/tls/tls.crt
  - OBS_TLS_KEY_FILE=/etc/tls/tls.key
ports:
  - "8443:8080"  # Map to standard HTTPS port
```

### Option 2: Nginx Reverse Proxy

Recommended for production. OpenBotStack listens on localhost, Nginx handles TLS.

```nginx
server {
    listen 443 ssl http2;
    server_name openbotstack.example.com;

    ssl_certificate /etc/tls/tls.crt;
    ssl_certificate_key /etc/tls/tls.key;

    # Security headers
    add_header X-Content-Type-Options nosniff;
    add_header X-Frame-Options DENY;
    add_header Strict-Transport-Security "max-age=63072000" always;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # SSE support for streaming chat
    location /v1/chat/stream {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Connection '';
        proxy_http_version 1.1;
        proxy_buffering off;
        proxy_cache off;
        chunked_transfer_encoding off;
    }
}

server {
    listen 80;
    server_name openbotstack.example.com;
    return 301 https://$host$request_uri;
}
```

### Option 3: Caddy Reverse Proxy

Caddy provides automatic HTTPS with Let's Encrypt:

```
openbotstack.example.com {
    reverse_proxy localhost:8080
}
```

## Environment Variables

All configuration can be set via environment variables, overriding `config.yaml` values.

### Server

| Variable | Default | Description |
|----------|---------|-------------|
| `OBS_SERVER_ADDR` | `:8080` | Listen address (host:port) |

### Authentication

| Variable | Default | Description |
|----------|---------|-------------|
| `OBS_AUTH_STRICT` | `false` | Require authentication on all endpoints (except health) |
| `JWT_SECRET` | _(empty)_ | Enable JWT authentication. When set, both API Key and JWT are accepted |
| `JWT_STRICT` | `false` | Require JWT validation (not just API Key) |
| `OBS_SEED_DEFAULTS` | `true` | Seed default tenant, admin user, and API key on first run. Set to `false` to disable |

### LLM Provider

| Variable | Default | Description |
|----------|---------|-------------|
| `OBS_LLM_PROVIDER` | `openai` | Default LLM provider name (`openai`, `modelscope`, etc.) |
| `OBS_LLM_API_KEY` | _(empty)_ | API key. Applied to both OpenAI and ModelScope providers |
| `OBS_LLM_URL` | _(empty)_ | Override base URL. Useful for self-hosted or proxy endpoints |
| `OBS_LLM_MODEL` | _(empty)_ | Override model name (e.g., `gpt-4o`, `Qwen/Qwen2.5-Coder-32B-Instruct`) |

### Database and Storage

| Variable | Default | Description |
|----------|---------|-------------|
| `OBS_DATABASE_PATH` | `openbotstack.db` | Path to SQLite database file |
| `OBS_DATA_DIR` | `./data` | Directory for memory data (markdown files) |
| `OBS_SKILLS_PATH` | `./examples/skills` | Directory containing Wasm skill modules |

### Observability

| Variable | Default | Description |
|----------|---------|-------------|
| `OBS_LOG_LEVEL` | `info` | Log level: `debug`, `info`, `warn`, `error` |

### TLS

| Variable | Default | Description |
|----------|---------|-------------|
| `OBS_TLS_CERT_FILE` | _(empty)_ | Path to TLS certificate file (PEM format) |
| `OBS_TLS_KEY_FILE` | _(empty)_ | Path to TLS private key file (PEM format) |

### Vector Search (Optional)

| Variable | Default | Description |
|----------|---------|-------------|
| `OBS_VECTOR_DB_URL` | _(empty)_ | PostgreSQL connection string with pgvector. Setting this enables vector search automatically |

### Legacy / Unused

| Variable | Default | Description |
|----------|---------|-------------|
| `OBS_REDIS_URL` | `redis://localhost:6379` | Redis URL (no longer required; SQLite replaced Redis for persistence) |

## Configuration File

OpenBotStack reads `config.yaml` for defaults, which environment variables override.

```yaml
server:
  addr: ":8080"

tls:
  cert_file: ""    # OBS_TLS_CERT_FILE
  key_file: ""     # OBS_TLS_KEY_FILE

database:
  path: "openbotstack.db"

redis:
  url: "redis://localhost:6379"  # No longer required; SQLite is default

providers:
  llm:
    default: openai
    openai:
      base_url: "https://api.openai.com/v1"
      api_key: ""
      model: "gpt-4o"
    modelscope:
      base_url: "https://api-inference.modelscope.cn/v1"
      api_key: ""
      model: "Qwen/Qwen2.5-Coder-32B-Instruct"

observability:
  log_level: "info"       # debug, info, warn, error
  otel_enabled: false
  otel_endpoint: ""       # OTLP gRPC endpoint (e.g., localhost:4317)

memory:
  data_dir: "./data"
  summary_threshold: 20
  summary_enabled: true
  max_history_messages: 50

sandbox:
  http_allowlist:
    - "*"                  # Restrict in production
  tool_registry_url: "http://localhost:8080"

vector:
  enabled: false
  database_url: ""         # postgres://user:pass@host:5432/db?sslmode=disable
  model: "text-embedding-3-small"
  dimensions: 512
```

## First Run

### Default Seed

On first startup with `OBS_SEED_DEFAULTS=true` (the default), OpenBotStack creates:

1. A default tenant
2. An admin user
3. An admin API key (printed to stdout once)

```bash
$ ./openbotstack
⚠️  Default admin API Key (save this, it won't be shown again):
    obs_sk_abcdef1234567890
```

Save this key. You will need it for all API calls.

### Making Your First Request

```bash
# Check health
curl http://localhost:8080/health

# List available skills
curl -H "Authorization: Bearer obs_sk_abcdef1234567890" \
     http://localhost:8080/v1/skills

# Send a chat message
curl -X POST http://localhost:8080/v1/chat \
  -H "Authorization: Bearer obs_sk_abcdef1234567890" \
  -H "Content-Type: application/json" \
  -d '{"message": "Hello!", "session_id": "test-1"}'
```

### Disabling Seed

For production deployments where you want to manage credentials explicitly:

```bash
OBS_SEED_DEFAULTS=false ./openbotstack
```

Then use the Admin API to create tenants and API keys (see Multi-Tenant Setup below).

## Database

OpenBotStack uses SQLite via `modernc.org/sqlite` (pure Go, zero CGO dependency). No external database server is required.

### File Location

Default: `openbotstack.db` in the working directory.

Override with `OBS_DATABASE_PATH`:

```bash
OBS_DATABASE_PATH=/var/lib/openbotstack/openbotstack.db ./openbotstack
```

### Backup

SQLite supports safe backup while the server is running:

```bash
# Online backup using sqlite3 CLI
sqlite3 /var/lib/openbotstack/openbotstack.db ".backup /backup/openbotstack-$(date +%Y%m%d).db"

# Or simply copy (may produce a slightly inconsistent snapshot)
cp /var/lib/openbotstack/openbotstack.db /backup/openbotstack-$(date +%Y%m%d).db
```

For Docker deployments, back up the volume:

```bash
docker compose exec openbotstack \
  cp /app/data/openbotstack.db /app/data/backup-$(date +%Y%m%d).db
```

### What SQLite Stores

- Tenants and users
- API keys (hashed)
- Rate limit counters
- Quota tracking
- Audit log entries

Memory data is stored separately as markdown files in `OBS_DATA_DIR`.

## Monitoring

### Health Checks

Two endpoints for health monitoring:

```bash
# Liveness (is the process running?)
curl http://localhost:8080/health

# Readiness (is it ready to serve requests?)
curl http://localhost:8080/healthz
```

Both return JSON:

```json
{"status": "ok", "version": "v1.0.0"}
```

### Prometheus Metrics

OpenBotStack exposes Prometheus metrics at `/metrics`:

```bash
curl http://localhost:8080/metrics
```

Add to your `prometheus.yml`:

```yaml
scrape_configs:
  - job_name: openbotstack
    static_configs:
      - targets: ['localhost:8080']
    metrics_path: /metrics
```

### OpenTelemetry

Enable OTel tracing and metrics export:

```yaml
# config.yaml
observability:
  otel_enabled: true
  otel_endpoint: "localhost:4317"  # OTLP gRPC
```

## Multi-Tenant Setup

OpenBotStack supports multi-tenant isolation. Each tenant has separate API keys, quotas, and data.

### Create a Tenant

```bash
curl -X POST http://localhost:8080/v1/admin/tenants \
  -H "Authorization: Bearer <admin-api-key>" \
  -H "Content-Type: application/json" \
  -d '{"name": "Acme Corp", "plan": "pro"}'
```

Response:

```json
{"id": "tenant_abc123", "name": "Acme Corp", "plan": "pro", "created_at": "..."}
```

### Create a User

```bash
curl -X POST http://localhost:8080/v1/admin/users \
  -H "Authorization: Bearer <admin-api-key>" \
  -H "Content-Type: application/json" \
  -d '{"tenant_id": "tenant_abc123", "name": "Alice", "role": "user"}'
```

### Create an API Key

```bash
curl -X POST http://localhost:8080/v1/admin/api-keys \
  -H "Authorization: Bearer <admin-api-key>" \
  -H "Content-Type: application/json" \
  -d '{"tenant_id": "tenant_abc123", "user_id": "user_xyz", "name": "Production Key"}'
```

Response includes the key (shown once):

```json
{"key": "obs_sk_...", "name": "Production Key", "tenant_id": "tenant_abc123"}
```

### Tenant Isolation

All API calls are scoped to the tenant associated with the API key:

- Sessions and conversation history are tenant-isolated
- Skill execution is tenant-isolated
- Memory and context are tenant-isolated
- Audit logs are tagged per-tenant

## Vector Search (Optional)

OpenBotStack supports optional vector semantic search via PostgreSQL + pgvector. This is not required for basic operation; the system defaults to keyword-based matching.

### Setup

1. Start PostgreSQL with pgvector:

```bash
docker run -d \
  --name openbotstack-pg \
  -e POSTGRES_DB=openbotstack \
  -e POSTGRES_USER=openbotstack \
  -e POSTGRES_PASSWORD=changeme \
  -p 5432:5432 \
  pgvector/pgvector:pg16
```

2. Set the connection string:

```bash
OBS_VECTOR_DB_URL=postgres://openbotstack:changeme@localhost:5432/openbotstack?sslmode=disable \
  ./openbotstack
```

Setting `OBS_VECTOR_DB_URL` automatically enables vector search. The schema is created on startup.

3. Or use docker-compose with the commented-out `postgres` service.

## Production Checklist

- [ ] Set `OBS_LLM_API_KEY` with a valid provider key
- [ ] Set `OBS_AUTH_STRICT=true` to require authentication on all endpoints
- [ ] Set `JWT_SECRET` to a cryptographically random value (32+ bytes)
- [ ] Set `OBS_SEED_DEFAULTS=false` after initial setup
- [ ] Mount `openbotstack-data` volume for persistence
- [ ] Configure TLS (built-in or reverse proxy)
- [ ] Restrict `sandbox.http_allowlist` to specific domains
- [ ] Set up database backup schedule
- [ ] Configure Prometheus to scrape `/metrics`
- [ ] Review and set appropriate `OBS_LOG_LEVEL` (recommend `info` for production)
- [ ] Restrict the `sandbox.http_allowlist` from `*` to specific domains
- [ ] Configure rate limits and quotas per tenant
