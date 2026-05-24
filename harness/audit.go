package harness

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/openbotstack/openbotstack-core/audit"
)

// AuditLayer provides immutable, append-only audit trail for execution.
type AuditLayer struct {
	mu    sync.Mutex
	trail []audit.AuditEvent
}

// NewAuditLayer creates an audit layer.
func NewAuditLayer() *AuditLayer {
	return &AuditLayer{
		trail: make([]audit.AuditEvent, 0),
	}
}

// RecordStep appends an audit event to the immutable trail.
func (al *AuditLayer) RecordStep(ctx context.Context, event audit.AuditEvent) error {
	if event.TraceID == "" {
		event.TraceID = uuid.NewString()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	al.mu.Lock()
	al.trail = append(al.trail, event)
	al.mu.Unlock()

	return nil
}

// Trail returns a copy of the complete audit trail.
func (al *AuditLayer) Trail() []audit.AuditEvent {
	al.mu.Lock()
	defer al.mu.Unlock()
	cp := make([]audit.AuditEvent, len(al.trail))
	copy(cp, al.trail)
	return cp
}

// TrailSize returns the number of entries in the audit trail.
func (al *AuditLayer) TrailSize() int {
	al.mu.Lock()
	defer al.mu.Unlock()
	return len(al.trail)
}
