package main

import (
	"log/slog"

	audit "github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
)

// InitAudit creates the audit logger and wires it into the executor.
func (b *ServerBuilder) InitAudit() *ServerBuilder {
	auditLogger := audit.NewSQLiteAuditLogger(b.pdb.DB)
	slog.Info("sqlite audit logger initialized")
	b.exec.SetAuditLogger(auditLogger)
	slog.Info("audit logger wired to executor")
	b.auditLogger = auditLogger
	return b
}
