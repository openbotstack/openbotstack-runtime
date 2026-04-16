package execution_logs

import (
	"context"
	"time"
)

// Event represents an audit log entry.
type Event struct {
	ID        string
	TenantID  string
	UserID    string
	RequestID string
	Action    string // e.g., "skills.execute", "model.generate"
	Resource  string // e.g., "skill/search", "model/claude"
	Outcome   string // "success", "failure", "timeout"
	Duration  time.Duration
	Metadata  map[string]string
	Timestamp time.Time
}

// QueryFilter defines filters for audit queries.
type QueryFilter struct {
	TenantID  string
	UserID    string
	RequestID string
	Action    string
	From      time.Time
	To        time.Time
	Limit     int
}

// AuditLogger provides audit logging operations.
type AuditLogger interface {
	Log(ctx context.Context, event Event) error
	Query(ctx context.Context, filter QueryFilter) ([]Event, error)
	Count(ctx context.Context, filter QueryFilter) (int, error)
}
