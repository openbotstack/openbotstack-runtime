package reasoning

import (
	"context"
	"sync"

	"github.com/openbotstack/openbotstack-core/audit"
)

// Store provides access to audit entries and execution traces for reasoning visualization.
type Store interface {
	// GetAuditTrail retrieves the audit entries for a specific execution.
	GetAuditTrail(ctx context.Context, executionID string) ([]audit.AuditEvent, error)
	// StoreTrail stores an audit trail under an execution ID.
	StoreTrail(executionID string, trail []audit.AuditEvent)
	// StoreTraceData stores an execution trace under an execution ID.
	StoreTraceData(executionID string, trace any)
	// GetTraceData retrieves an execution trace for an execution ID.
	GetTraceData(ctx context.Context, executionID string) (any, error)
}

// InMemoryStore is a Store backed by in-memory maps.
// Suitable for testing and single-process deployments.
// Thread-safe via sync.RWMutex.
type InMemoryStore struct {
	mu     sync.RWMutex
	trails map[string][]audit.AuditEvent
	traces map[string]any
	order  []string
	maxCap int
}

// NewInMemoryStore creates an empty in-memory reasoning store.
func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{
		trails: make(map[string][]audit.AuditEvent),
		traces: make(map[string]any),
		maxCap: 1000,
	}
}

// StoreTrail stores an audit trail under an execution ID.
func (s *InMemoryStore) StoreTrail(executionID string, trail []audit.AuditEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictIfNeeded()
	cp := make([]audit.AuditEvent, len(trail))
	copy(cp, trail)
	s.trails[executionID] = cp
	s.trackOrder(executionID)
}

// GetAuditTrail retrieves the audit trail for an execution ID.
func (s *InMemoryStore) GetAuditTrail(_ context.Context, executionID string) ([]audit.AuditEvent, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	trail, ok := s.trails[executionID]
	if !ok {
		return nil, nil
	}
	cp := make([]audit.AuditEvent, len(trail))
	copy(cp, trail)
	return cp, nil
}

// StoreTraceData stores an execution trace under an execution ID.
func (s *InMemoryStore) StoreTraceData(executionID string, trace any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.evictIfNeeded()
	s.traces[executionID] = trace
	s.trackOrder(executionID)
}

// GetTraceData retrieves an execution trace for an execution ID.
// Returns (nil, nil) if not found.
func (s *InMemoryStore) GetTraceData(_ context.Context, executionID string) (any, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	trace, ok := s.traces[executionID]
	if !ok {
		return nil, nil
	}
	return trace, nil
}

// StoreTrace implements harness.ReasoningStorer by delegating to StoreTraceData.
func (s *InMemoryStore) StoreTrace(executionID string, trace any) {
	s.StoreTraceData(executionID, trace)
}

func (s *InMemoryStore) trackOrder(executionID string) {
	for _, id := range s.order {
		if id == executionID {
			return
		}
	}
	s.order = append(s.order, executionID)
}

func (s *InMemoryStore) evictIfNeeded() {
	if len(s.order) < s.maxCap {
		return
	}
	evictCount := s.maxCap / 10
	if evictCount < 1 {
		evictCount = 1
	}
	for i := 0; i < evictCount && i < len(s.order); i++ {
		id := s.order[i]
		delete(s.trails, id)
		delete(s.traces, id)
	}
	s.order = s.order[evictCount:]
}
