# Testing Guide

## Quick Reference

```bash
# Run all unit tests (125+ tests)
make test

# Run integration tests (requires API Key)
export MODELSCOPE_API_KEY=your_key
make test-integration

# Run specific categories
make test-wasm       # Wasm runtime
make test-executor   # Skill executor
make test-skills     # Skill examples
```

## Integration Tests

Integration tests run against real external services (ModelScope LLM).
They are guarded by `//go:build integration` tags.

### Prerequisites
- ModelScope API Key
- Internet connection

### Running
```bash
export MODELSCOPE_API_KEY=ms-...
make test-integration
```

## Test Categories

### Core Runtime Tests

| Package | Tests | Description |
|---------|-------|-------------|
| wasm | 19 | Wasm runtime, module loading, execution, Host API |
| executor | 19 | Skill lifecycle, execution, Wasm integration |
| api | 8 | HTTP handlers, routing |
| audit | 6 | Audit logging |
| memory | 4 | Redis/Milvus adapters |
| ratelimit | 5 | Rate limiting |
| worker | 4 | Background workers |
| e2e | 3 | End-to-end tests |

### Skill Example Tests

| Skill | Type | Tests | Description |
|-------|------|-------|-------------|
| hello-world | Deterministic | 9 | Basic input/output |
| sentiment | LLM-Assisted | 6 | Sentiment analysis |
| tax-calculator | Deterministic | 9 | Tax calculation |

## Wasm Execution Tests

Real Wasm execution is verified by:

```go
// wasm/runtime_integration_test.go
TestRuntimeExecuteWithStartExport   // ✅ Executes _start
TestRuntimeExecuteWithExecuteExport // ✅ Executes execute
TestRuntimeExecuteInvalidWasm       // ✅ Error handling
TestRuntimeConcurrent               // ✅ 10 concurrent executions

// executor/executor_test.go  
TestExecuteWithRealWasm              // ✅ End-to-end skill execution
TestExecuteWithLoadSkillWithWasm     // ✅ Load + execute workflow
```

## Host API Tests

```go
// wasm/hostapi_test.go
TestHostFunctionsInputOutput  // Input/output buffer management
TestHostFunctionsLLMGenerate  // LLM integration (Unit)
TestHostFunctionsKV           // Key-value store
TestHostFunctionsLog          // Structured logging

// llm/client_integration_test.go (Integration)
TestModelScopeIntegration     // ✅ Real LLM call
TestModelScopeSentimentAnalysis // ✅ Real sentiment analysis
```

## CI Integration

GitHub Actions should run:

```yaml
- name: Test Unit
  run: make test-race test-cover

- name: Test Integration
  if: env.MODELSCOPE_API_KEY != ''
  run: make test-integration
  env:
    MODELSCOPE_API_KEY: ${{ secrets.MODELSCOPE_API_KEY }}
```
