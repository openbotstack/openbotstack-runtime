# openbotstack-runtime

Execution plane for OpenBotStack - Wasm skill execution, Host API, and runtime services.

## Quick Start

```bash
# Run all tests (125 tests)
make test

# Build
make build

# Build for all platforms (Linux x64/arm64, macOS arm64)
make build-all

# Run with embedded UI
make run
```

## Features

| Feature | Status |
|---------|--------|
| Wasm Execution | ✅ Working (wazero) |
| Host API (LLM/KV/Log) | ✅ Implemented |
| Skill Lifecycle | ✅ Load/Execute/Unload |
| Module Caching | ✅ LRU cache |
| Audit Logging | ✅ Structured logs |
| Rate Limiting | ✅ Tenant/User level |

## Architecture

```
┌──────────────────────────────────────────────────┐
│                  HTTP API                         │
│           /api/v1/skills/execute                 │
└────────────────────▲─────────────────────────────┘
                     │
┌────────────────────┴─────────────────────────────┐
│                 Executor                          │
│   LoadSkill → CanExecute → Execute → Unload      │
└────────────────────▲─────────────────────────────┘
                     │
┌────────────────────┴─────────────────────────────┐
│              Wasm Runtime (wazero)               │
│   ┌─────────────────────────────────────────┐   │
│   │           Host API Exports              │   │
│   │  get_input, set_output, llm_generate    │   │
│   │  kv_get, kv_set, log                    │   │
│   └─────────────────────────────────────────┘   │
└──────────────────────────────────────────────────┘
```

## Testing

```bash
make test          # All tests
make test-wasm     # Wasm runtime only
make test-executor # Executor only  
make test-skills   # Skill examples
make test-count    # Count all tests
```

See [docs/TESTING.md](docs/TESTING.md) for details.

## Skill Examples

| Skill | Type | Location |
|-------|------|----------|
| hello-world | Deterministic | examples/skills/hello-world |
| sentiment | LLM-Assisted | examples/skills/sentiment |
| tax-calculator | Deterministic | examples/skills/tax-calculator |
| summarize | Declarative | examples/skills/summarize |

## License

Apache 2.0