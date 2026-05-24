# Contributing to OpenBotStack

Thank you for your interest in contributing to OpenBotStack.

## Development Setup

### Prerequisites

- Go 1.26.1+ (includes wasip1 for Wasm skill compilation)
- Node.js 20+ (for frontend development)
- Rust + wasm32-wasip1 target (optional, for Rust-based skills)

### Repository Structure

```
openbotstack-core/     Control Plane — interfaces, state machine, skill registry
openbotstack-runtime/  Execution Plane — skill execution, HTTP server, frontend
openbotstack-apps/     Application Plane — domain skills, tools, workflows
openbotstack-docs/     Design docs, ADRs, guides
```

Each sub-repository is independent with its own `AI_CONTRACT.md` defining what it MAY and MUST NOT contain. Read the contract before making changes.

### Building

```bash
# Core (library only)
cd openbotstack-core && make all

# Runtime (binary + frontend)
cd openbotstack-runtime && make binary   # requires make web-build first

# Apps (domain skills)
cd openbotstack-apps && make test
```

### Testing

```bash
# Run tests in any repo
make test

# Run with race detector
make test-race

# Run single test
go test -v -run TestFunctionName ./path/to/package/...
```

## Code Conventions

- **Go style**: Follow standard Go conventions. Run `go vet` and `gofumpt` before committing.
- **No side effects in core**: `openbotstack-core` MUST NOT contain executable entrypoints, network calls (except LLM providers per ADR-011), or side effects.
- **No control logic in runtime**: `openbotstack-runtime` MUST NOT contain assistant identity definitions or policy decisions.
- **No runtime dependencies in apps**: `openbotstack-apps` depends ONLY on `openbotstack-core`.
- **Explicit over clever**: Prefer clear code over clever prompts. Prompts are configuration, not logic.
- **Bounded execution**: All loops MUST have explicit step/time limits. No infinite loops.

## Commit Messages

Use conventional commit format:
- `feat(scope): description` — new feature
- `fix(scope): description` — bug fix
- `refactor(scope): description` — code restructuring
- `docs: description` — documentation changes
- `chore: description` — maintenance tasks

## Pull Request Process

1. Create a feature branch from `main`
2. Make changes following the relevant `AI_CONTRACT.md`
3. Run `make check` (lint + test) in affected repos
4. Write clear PR description explaining the "why"
5. Ensure all CI checks pass

## Architecture Decisions

All non-trivial decisions should be documented as ADRs (Architecture Decision Records) in `openbotstack-docs/design/`. Follow the existing ADR format (Status, Date, Context, Decision, Consequences, Alternatives, References).
