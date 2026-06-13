package server

import (
	"log/slog"

	coreaudit "github.com/openbotstack/openbotstack-core/audit"
	audit "github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
	rtAudit "github.com/openbotstack/openbotstack-runtime/audit"
)

// InitAudit creates the unified audit emitter, subscribes the SQLite writer,
// and wires both into the executor. The AuditEmitter (ADR-023) is the single
// seam for all audit events. The legacy AuditLogger is kept as a backward-compatible
// field for the API layer's query/count operations.
func (b *ServerBuilder) InitAudit() *ServerBuilder {
	if b.err != nil {
		return b
	}
	b.requireInit("pdb", "InitAudit")
	b.requireInit("exec", "InitAudit")

	// Create the SQLite-backed logger (provides both write and query)
	sqliteLogger := audit.NewSQLiteAuditLogger(b.pdb.DB)
	b.auditLogger = sqliteLogger

	// Create the pub/sub emitter — the canonical audit seam (ADR-023)
	emitter := coreaudit.NewEmitter()

	// Subscribe the SQLite writer via the adapter
	subscriber := rtAudit.NewSQLiteAuditSubscriber(sqliteLogger)
	if err := emitter.Subscribe(subscriber); err != nil {
		slog.Error("failed to subscribe sqlite audit subscriber", "error", err)
	}
	b.auditEmitter = emitter

	// Wire the logger into the executor for any direct calls
	b.exec.SetAuditLogger(sqliteLogger)

	slog.Info("unified audit trail initialized",
		"emitter", "enabled",
		"subscribers", emitter.Subscribers(),
	)
	return b
}
