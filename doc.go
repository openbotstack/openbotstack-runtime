// Package runtime is the execution plane for OpenBotStack.
//
// This module implements the runtime components that execute skills,
// manage tool adapters, and handle audit logging. It depends on
// openbotstack-core for interface definitions.
//
// Key components:
//   - SkillExecutor: Executes skills with timeout and sandboxing
//   - ToolAdapter: HTTP/DB/API tool wrappers
//   - AuditLogger: Structured audit to PostgreSQL
//   - RateLimiter: Redis-backed token bucket implementation
//
// This module is stateless by design - all state is externalized to:
//   - Redis: Session, rate limits
//   - PostgreSQL: Audit logs
//   - Milvus: Long-term memory
package runtime
