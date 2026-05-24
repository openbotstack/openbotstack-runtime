package execution_logs

import (
	"context"
	"time"

	"github.com/openbotstack/openbotstack-core/audit"
)

// QueryFilter defines filters for audit queries.
type QueryFilter struct {
	TenantID  string
	UserID    string
	RequestID string
	Action    string
	Source    audit.Source
	From      time.Time
	To        time.Time
	Limit     int
}

// AuditLogger provides audit logging operations.
type AuditLogger interface {
	Log(ctx context.Context, event audit.AuditEvent) error
	Query(ctx context.Context, filter QueryFilter) ([]audit.AuditEvent, error)
	Count(ctx context.Context, filter QueryFilter) (int, error)
}
