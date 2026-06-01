// Package runtime is the execution plane for OpenBotStack.
//
// This module implements the runtime components that execute skills,
// manage tool invocation, and handle audit logging. It depends on
// openbotstack-core for interface definitions.
//
// Key components:
//   - ExecutionHarness: Sequential plan orchestrator with hooks and retry
//   - StepExecutor: Unified tool and skill step dispatch
//   - SkillExecutor: Executes skills with timeout and sandboxing
//   - AuditLogger: SQLite-backed structured audit logging
//
// Persistence uses embedded SQLite for zero external dependencies:
//   - SQLite: Audit logs, rate limits, session memory, quota
package runtime
