package audit

import (
	"context"

	coreaudit "github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
)

// SQLiteAuditSubscriber adapts an execution_logs.AuditLogger to implement
// the core audit.AuditSubscriber interface. It receives events from the
// AuditEmitter and writes them to SQLite via the underlying logger.
//
// This is the single bridge between the core pub/sub emitter and the
// runtime's SQLite persistence layer.
type SQLiteAuditSubscriber struct {
	logger execution_logs.AuditLogger
}

// NewSQLiteAuditSubscriber creates a new subscriber backed by the given logger.
func NewSQLiteAuditSubscriber(logger execution_logs.AuditLogger) *SQLiteAuditSubscriber {
	return &SQLiteAuditSubscriber{logger: logger}
}

// OnEvent receives an audit event from the emitter and writes it to SQLite.
func (s *SQLiteAuditSubscriber) OnEvent(ctx context.Context, event coreaudit.AuditEvent) error {
	return s.logger.Log(ctx, event)
}

// ID returns the unique identifier for this subscriber.
func (s *SQLiteAuditSubscriber) ID() string {
	return "sqlite-audit-subscriber"
}
