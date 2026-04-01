package execution_logs

import (
	"context"
	"sync"
	"time"
)

// InMemoryAuditLogger implements AuditLogger using an in-memory slice.
type InMemoryAuditLogger struct {
	mu     sync.RWMutex
	events []Event
}

// NewInMemoryAuditLogger creates a new in-memory audit logger.
func NewInMemoryAuditLogger() *InMemoryAuditLogger {
	return &InMemoryAuditLogger{
		events: make([]Event, 0),
	}
}

// Log records an audit event.
func (l *InMemoryAuditLogger) Log(ctx context.Context, event Event) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	l.events = append(l.events, event)
	return nil
}

// Query retrieves audit events matching the filter.
func (l *InMemoryAuditLogger) Query(ctx context.Context, filter QueryFilter) ([]Event, error) {
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
func (l *InMemoryAuditLogger) Count(ctx context.Context, filter QueryFilter) (int, error) {
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
func (l *InMemoryAuditLogger) matches(e Event, f QueryFilter) bool {
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
