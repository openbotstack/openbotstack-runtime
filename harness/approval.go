package harness

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/openbotstack/openbotstack-core/execution"
)

// InMemoryApprovalStore is a thread-safe in-memory approval store.
type InMemoryApprovalStore struct {
	mu         sync.RWMutex
	requests   map[string]*execution.ApprovalRequest
	defaultTTL time.Duration
}

// NewInMemoryApprovalStore creates a new in-memory approval store with the given default TTL.
func NewInMemoryApprovalStore(defaultTTL time.Duration) *InMemoryApprovalStore {
	if defaultTTL <= 0 {
		defaultTTL = 30 * time.Minute
	}
	return &InMemoryApprovalStore{
		requests:   make(map[string]*execution.ApprovalRequest),
		defaultTTL: defaultTTL,
	}
}

// RequestApproval creates a new approval request and stores it.
func (s *InMemoryApprovalStore) RequestApproval(_ context.Context, req *execution.ApprovalRequest) (*execution.ApprovalRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("approval: request cannot be nil")
	}

	now := time.Now()
	id := req.ID
	if id == "" {
		id = uuid.NewString()
	}

	stored := &execution.ApprovalRequest{
		ID:          id,
		StepName:    req.StepName,
		StepID:      req.StepID,
		ExecutionID: req.ExecutionID,
		TenantID:    req.TenantID,
		RiskLevel:   req.RiskLevel,
		Reason:      req.Reason,
		Status:      execution.ApprovalPending,
		CreatedAt:   now,
		ExpiresAt:   now.Add(s.defaultTTL),
	}

	s.mu.Lock()
	s.requests[id] = stored
	s.mu.Unlock()

	return stored, nil
}

// GetApproval retrieves an approval request by ID.
// Expired requests are lazily marked as expired.
func (s *InMemoryApprovalStore) GetApproval(id string) (*execution.ApprovalRequest, error) {
	s.mu.Lock()
	req, ok := s.requests[id]
	if !ok {
		s.mu.Unlock()
		return nil, fmt.Errorf("approval: request %q not found", id)
	}

	// Lazy expiry check: mark as expired if past deadline and still pending
	if req.Status == execution.ApprovalPending && time.Now().After(req.ExpiresAt) {
		req.Status = execution.ApprovalExpired
	}
	s.mu.Unlock()

	// Return a copy to prevent external mutation
	copy := *req
	return &copy, nil
}

// Approve marks an approval request as approved.
func (s *InMemoryApprovalStore) Approve(id, approverID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return fmt.Errorf("approval: request %q not found", id)
	}
	if req.Status == execution.ApprovalPending && time.Now().After(req.ExpiresAt) {
		req.Status = execution.ApprovalExpired
		return fmt.Errorf("approval: request %q has expired", id)
	}
	if req.Status != execution.ApprovalPending {
		return fmt.Errorf("approval: request %q is %s, cannot approve", id, req.Status)
	}

	now := time.Now()
	req.Status = execution.ApprovalApproved
	req.ApproverID = approverID
	req.ResolvedAt = &now
	return nil
}

// Deny marks an approval request as denied.
func (s *InMemoryApprovalStore) Deny(id, approverID, reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	req, ok := s.requests[id]
	if !ok {
		return fmt.Errorf("approval: request %q not found", id)
	}
	if req.Status == execution.ApprovalPending && time.Now().After(req.ExpiresAt) {
		req.Status = execution.ApprovalExpired
		return fmt.Errorf("approval: request %q has expired", id)
	}
	if req.Status != execution.ApprovalPending {
		return fmt.Errorf("approval: request %q is %s, cannot deny", id, req.Status)
	}

	now := time.Now()
	req.Status = execution.ApprovalDenied
	req.ApproverID = approverID
	req.DenyReason = reason
	req.ResolvedAt = &now
	return nil
}

// ListPending returns all pending approvals, optionally filtered by tenantID.
func (s *InMemoryApprovalStore) ListPending(tenantID string) []execution.ApprovalRequest {
	s.mu.Lock()
	defer s.mu.Unlock()

	var result []execution.ApprovalRequest
	for _, req := range s.requests {
		if req.Status == execution.ApprovalPending && time.Now().After(req.ExpiresAt) {
			req.Status = execution.ApprovalExpired
			continue
		}
		if req.Status != execution.ApprovalPending {
			continue
		}
		if tenantID != "" && req.TenantID != tenantID {
			continue
		}
		result = append(result, *req)
	}
	return result
}
