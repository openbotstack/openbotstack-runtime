// Package runtime is the execution plane for OpenBotStack.
//
// This module implements the runtime components that execute skills,
// manage tool adapters, and handle audit logging. It depends on
// openbotstack-core for interface definitions.
//
// Key components:
//   - SkillExecutor: Executes skills with timeout and sandboxing
//   - ToolAdapter: HTTP/DB/API tool wrappers
//   - AuditLogger: SQLite-backed structured audit logging
//   - RateLimiter: SQLite-backed token bucket rate limiting
//
// Persistence uses embedded SQLite for zero external dependencies:
//   - SQLite: Audit logs, rate limits, session memory, quota
package runtime
