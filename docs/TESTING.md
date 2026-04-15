# Testing Guide

## Quick Reference

```bash
# Run all unit tests (440+ tests)
make test

# Run with race detector
make test-race

# Run with coverage
make test-cover

# Run specific packages
go test -v ./api/...
go test -v ./api/middleware/...
go test -v ./memory/...
go test -v ./sandbox/...
go test -v ./integration/...
```

## Test Categories

### API Layer

| Package | Description |
|---------|-------------|
| api | Router, Chat, SSE streaming, Skills, Executions, Sessions, Health, Errors |
| api/middleware | CORS, Rate Limiting, API Key Auth, JWT Auth, Admin Role, Error Helper |

### Memory Layer

| Package | Description |
|---------|-------------|
| memory | Markdown store, SQLite store, Summarizer, Bridge, Async indexer, Embedding service, Checkpoints |

### Execution Layer

| Package | Description |
|---------|-------------|
| sandbox/wasm | Wasm runtime, Host API, Sandboxed HTTP |
| executor | Skill lifecycle, execution, workflow queue |
| ratelimit | SQLite rate limiter, quota store |
| persistence | SQLite database, migrations, seeding |
| config | Configuration loading, env vars |
| loop | Outer/inner loop, checkpoints, stop conditions, context compaction |
| context | Context assembler |
| toolrunner | Tool runner, invocation pipeline |
| observability | OTel SDK, Prometheus metrics |

### Skill Example Tests

| Skill | Type | Description |
|-------|------|-------------|
| hello-world | Deterministic | Basic input/output |
| sentiment | LLM-Assisted | Sentiment analysis |
| tax-calculator | Deterministic | Tax calculation |
| math-add | Deterministic | Math operations |
| wordcount | Deterministic | Word counting |
| meeting-summarize | Declarative | Meeting summarization |

### Integration Tests

| File | Description |
|------|-------------|
| full_system_test.go | Full system test with mock LLM, auto-builds binary |
| error_test.go | JSON error response format validation |
| streaming_test.go | SSE streaming format and error handling |

## Running Integration Tests

Integration tests require the server to be running. The binary is built automatically.

```bash
# Run integration tests (auto-builds binary)
go test -v ./integration/ -timeout 120s

# Or use the existing full system test
go test -v ./integration/ -run TestFullSystem
```

## CI Integration

GitHub Actions configuration:

```yaml
- name: Test
  run: make test-race

- name: Integration Test
  run: go test -v ./integration/ -timeout 120s
```
