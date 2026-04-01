# AI RULES — READ FIRST

This file defines mandatory rules for AI coding tools.
All instructions in this file are authoritative for this repository.

# REPOSITORY: openbotstack-runtime

## ROLE:
This repository implements the EXECUTION PLANE of OpenBotStack.

## IT MAY CONTAIN:
- `cmd/openbotstack/main.go` — **the ONLY executable entrypoint**
- HTTP/gRPC servers
- Skill execution engine
- Tool adapters (HTTP, DB, API, Wasm, etc.)
- Timeout, retry, and sandbox mechanisms
- Execution logging and traces
- Runtime-level error handling

## IT MUST NOT:
- Define assistant identity or persona
- Make policy or permission decisions
- Contain tenant or user configuration logic
- Define long-term memory models
- Interact directly with concrete LLM providers (e.g., openai, anthropic); all AI calls must be routed through the `openbotstack-core/model` ModelRouter abstraction.

## DESIGN RULES:
- Execution is always invoked by the control plane
- Runtime must be stateless between requests
- All executions must emit structured audit events
- Fail fast and fail safely
