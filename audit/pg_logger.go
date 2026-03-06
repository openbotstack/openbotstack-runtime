// Package audit implements structured audit logging to PostgreSQL.
package audit

import (
	"context"
	"sync"
	"time"
)

// Event represents an audit log entry.
type Event struct {
	ID        string
	TenantID  string
	UserID    string
	RequestID string
	Action    string // e.g., "skill.execute", "model.generate"
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

// PGAuditLogger implements AuditLogger using PostgreSQL (in-memory stub).
type PGAuditLogger struct {
	mu     sync.RWMutex
	events []Event
}

// NewPGAuditLogger creates a new audit logger.
func NewPGAuditLogger() *PGAuditLogger {
	return &PGAuditLogger{
		events: make([]Event, 0),
	}
}

// Log records an audit event.
func (l *PGAuditLogger) Log(ctx context.Context, event Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	l.events = append(l.events, event)
	return nil
}

// Query retrieves audit events matching the filter.
func (l *PGAuditLogger) Query(ctx context.Context, filter QueryFilter) ([]Event, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make([]Event, 0)
	for _, e := range l.events {
		if l.matches(e, filter) {
			result = append(result, e)
		}
	}

	if filter.Limit > 0 && len(result) > filter.Limit {
		result = result[:filter.Limit]
	}

	return result, nil
}

// Count returns the number of events matching the filter.
func (l *PGAuditLogger) Count(ctx context.Context, filter QueryFilter) (int, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()

	count := 0
	for _, e := range l.events {
		if l.matches(e, filter) {
			count++
		}
	}

	return count, nil
}

// matches checks if an event matches the filter.
func (l *PGAuditLogger) matches(e Event, f QueryFilter) bool {
	if f.TenantID != "" && e.TenantID != f.TenantID {
		return false
	}
	if f.UserID != "" && e.UserID != f.UserID {
		return false
	}
	if f.RequestID != "" && e.RequestID != f.RequestID {
		return false
	}
	if f.Action != "" && e.Action != f.Action {
		return false
	}
	if !f.From.IsZero() && e.Timestamp.Before(f.From) {
		return false
	}
	if !f.To.IsZero() && e.Timestamp.After(f.To) {
		return false
	}
	return true
}
