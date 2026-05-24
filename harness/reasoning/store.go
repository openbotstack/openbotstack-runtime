package reasoning

import (
	"context"

	"github.com/openbotstack/openbotstack-core/audit"
)

// Store provides access to audit entries for reasoning visualization.
type Store interface {
	// GetAuditTrail retrieves the audit entries for a specific execution.
	GetAuditTrail(ctx context.Context, executionID string) ([]audit.AuditEvent, error)
	// StoreTrail stores an audit trail under an execution ID.
	StoreTrail(executionID string, trail []audit.AuditEvent)
}

// InMemoryStore is a Store backed by an in-memory map of execution → audit trail.
// Suitable for testing and single-process deployments.
type InMemoryStore struct {
	trails map[string][]audit.AuditEvent
}

// NewInMemoryStore creates an empty in-memory reasoning store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		trails: make(map[string][]audit.AuditEvent),
	}
}

// StoreTrail stores an audit trail under an execution ID.
func (s *InMemoryStore) StoreTrail(executionID string, trail []audit.AuditEvent) {
	cp := make([]audit.AuditEvent, len(trail))
	copy(cp, trail)
	s.trails[executionID] = cp
}

// GetAuditTrail retrieves the audit trail for an execution ID.
func (s *InMemoryStore) GetAuditTrail(_ context.Context, executionID string) ([]audit.AuditEvent, error) {
	trail, ok := s.trails[executionID]
	if !ok {
		return nil, nil
	}
	cp := make([]audit.AuditEvent, len(trail))
	copy(cp, trail)
	return cp, nil
}
