package harness

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
)

// --- InMemoryApprovalStore Tests ---
//
// TDD: RED phase. All tests written before implementation.

// NORMAL: Request → Get → Approve → Get confirms full lifecycle.
func TestInMemoryApprovalStore_RequestAndApprove(t *testing.T) {
	store := NewInMemoryApprovalStore(30 * time.Minute)

	req := &execution.ApprovalRequest{
		StepName:    "critical_action",
		StepID:      "step-123",
		ExecutionID: "exec-456",
		TenantID:    "tenant-1",
		RiskLevel:   "critical",
		Reason:      "requires human review",
	}

	created, err := store.RequestApproval(context.Background(), req)
	if err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if created.ID == "" {
		t.Fatal("expected non-empty approval ID")
	}
	if created.Status != execution.ApprovalPending {
		t.Errorf("Status = %q, want %q", created.Status, execution.ApprovalPending)
	}
	if created.CreatedAt.IsZero() {
		t.Fatal("expected non-zero CreatedAt")
	}
	if created.ExpiresAt.IsZero() {
		t.Fatal("expected non-zero ExpiresAt")
	}
	if created.ExpiresAt.Before(created.CreatedAt) {
		t.Fatal("ExpiresAt should be after CreatedAt")
	}

	// Retrieve
	got, err := store.GetApproval(created.ID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.Status != execution.ApprovalPending {
		t.Errorf("Status = %q, want %q", got.Status, execution.ApprovalPending)
	}

	// Approve
	if err := store.Approve(created.ID, "admin-1"); err != nil {
		t.Fatalf("Approve: %v", err)
	}

	got, err = store.GetApproval(created.ID)
	if err != nil {
		t.Fatalf("GetApproval after approve: %v", err)
	}
	if got.Status != execution.ApprovalApproved {
		t.Errorf("Status = %q, want %q", got.Status, execution.ApprovalApproved)
	}
	if got.ApproverID != "admin-1" {
		t.Errorf("ApproverID = %q, want %q", got.ApproverID, "admin-1")
	}
	if got.ResolvedAt == nil {
		t.Fatal("expected non-nil ResolvedAt after approval")
	}
}

// ABNORMAL: Approve non-existent request.
func TestInMemoryApprovalStore_ApproveNonExistent(t *testing.T) {
	store := NewInMemoryApprovalStore(30 * time.Minute)
	err := store.Approve("nonexistent-id", "admin-1")
	if err == nil {
		t.Fatal("expected error approving non-existent request")
	}
}

// ABNORMAL: Deny non-existent request.
func TestInMemoryApprovalStore_DenyNonExistent(t *testing.T) {
	store := NewInMemoryApprovalStore(30 * time.Minute)
	err := store.Deny("nonexistent-id", "admin-1", "not safe")
	if err == nil {
		t.Fatal("expected error denying non-existent request")
	}
}

// ABNORMAL: Get non-existent request.
func TestInMemoryApprovalStore_GetNonExistent(t *testing.T) {
	store := NewInMemoryApprovalStore(30 * time.Minute)
	_, err := store.GetApproval("nonexistent-id")
	if err == nil {
		t.Fatal("expected error getting non-existent request")
	}
}

// ABNORMAL: Double approve same request.
func TestInMemoryApprovalStore_DoubleApprove(t *testing.T) {
	store := NewInMemoryApprovalStore(30 * time.Minute)
	created, _ := store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName:  "test",
		RiskLevel: "critical",
	})
	if err := store.Approve(created.ID, "admin-1"); err != nil {
		t.Fatalf("first Approve: %v", err)
	}
	err := store.Approve(created.ID, "admin-2")
	if err == nil {
		t.Fatal("expected error on double approve")
	}
}

// ABNORMAL: Double deny same request.
func TestInMemoryApprovalStore_DoubleDeny(t *testing.T) {
	store := NewInMemoryApprovalStore(30 * time.Minute)
	created, _ := store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName:  "test",
		RiskLevel: "critical",
	})
	if err := store.Deny(created.ID, "admin-1", "unsafe"); err != nil {
		t.Fatalf("first Deny: %v", err)
	}
	err := store.Deny(created.ID, "admin-2", "still unsafe")
	if err == nil {
		t.Fatal("expected error on double deny")
	}
}

// ABNORMAL: Approve then deny should fail.
func TestInMemoryApprovalStore_ApproveThenDeny(t *testing.T) {
	store := NewInMemoryApprovalStore(30 * time.Minute)
	created, _ := store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName:  "test",
		RiskLevel: "critical",
	})
	if err := store.Approve(created.ID, "admin-1"); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	err := store.Deny(created.ID, "admin-1", "changed mind")
	if err == nil {
		t.Fatal("expected error denying already-approved request")
	}
}

// ABNORMAL: Deny then approve should fail.
func TestInMemoryApprovalStore_DenyThenApprove(t *testing.T) {
	store := NewInMemoryApprovalStore(30 * time.Minute)
	created, _ := store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName:  "test",
		RiskLevel: "critical",
	})
	if err := store.Deny(created.ID, "admin-1", "unsafe"); err != nil {
		t.Fatalf("Deny: %v", err)
	}
	err := store.Approve(created.ID, "admin-1")
	if err == nil {
		t.Fatal("expected error approving already-denied request")
	}
}

// ABNORMAL: Expired approval cleaned up on access.
func TestInMemoryApprovalStore_ExpiredApproval(t *testing.T) {
	store := NewInMemoryApprovalStore(1 * time.Millisecond)
	created, _ := store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName:  "test",
		RiskLevel: "critical",
	})

	// Wait for expiry
	time.Sleep(5 * time.Millisecond)

	got, err := store.GetApproval(created.ID)
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if got.Status != execution.ApprovalExpired {
		t.Errorf("Status = %q, want %q", got.Status, execution.ApprovalExpired)
	}
}

// ABNORMAL: List pending with empty tenant.
func TestInMemoryApprovalStore_EmptyTenantList(t *testing.T) {
	store := NewInMemoryApprovalStore(30 * time.Minute)
	pending := store.ListPending("")
	if len(pending) != 0 {
		t.Errorf("expected 0 pending, got %d", len(pending))
	}
}

// NORMAL: List pending filtered by tenant.
func TestInMemoryApprovalStore_ListPendingByTenant(t *testing.T) {
	store := NewInMemoryApprovalStore(30 * time.Minute)

	store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName: "step-1", RiskLevel: "critical", TenantID: "tenant-A",
	})
	store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName: "step-2", RiskLevel: "critical", TenantID: "tenant-B",
	})
	store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName: "step-3", RiskLevel: "critical", TenantID: "tenant-A",
	})

	pendingA := store.ListPending("tenant-A")
	if len(pendingA) != 2 {
		t.Errorf("tenant-A pending = %d, want 2", len(pendingA))
	}

	pendingB := store.ListPending("tenant-B")
	if len(pendingB) != 1 {
		t.Errorf("tenant-B pending = %d, want 1", len(pendingB))
	}

	pendingAll := store.ListPending("")
	if len(pendingAll) != 3 {
		t.Errorf("all pending = %d, want 3", len(pendingAll))
	}
}

// ABNORMAL: Concurrent access does not race.
func TestInMemoryApprovalStore_ConcurrentAccess(t *testing.T) {
	store := NewInMemoryApprovalStore(30 * time.Minute)
	var wg sync.WaitGroup

	// Concurrent requests
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			store.RequestApproval(context.Background(), &execution.ApprovalRequest{
				StepName:  "concurrent-step",
				RiskLevel: "critical",
				TenantID:  "tenant-concurrent",
			})
		}(i)
	}

	// Concurrent reads
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			store.ListPending("tenant-concurrent")
		}()
	}

	wg.Wait()
}

// ABNORMAL: Request with pre-populated ID preserves it.
func TestInMemoryApprovalStore_RequestWithPrePopulatedID(t *testing.T) {
	store := NewInMemoryApprovalStore(30 * time.Minute)
	created, err := store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		ID:        "custom-id-123",
		StepName:  "test",
		RiskLevel: "critical",
	})
	if err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}
	if created.ID != "custom-id-123" {
		t.Errorf("ID = %q, want %q", created.ID, "custom-id-123")
	}
}

// ABNORMAL: Context cancellation does not block request creation.
func TestInMemoryApprovalStore_ContextCancellation(t *testing.T) {
	store := NewInMemoryApprovalStore(30 * time.Minute)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := store.RequestApproval(ctx, &execution.ApprovalRequest{
		StepName:  "test",
		RiskLevel: "critical",
	})
	// RequestApproval should succeed regardless of context (it's just a store operation)
	if err != nil {
		t.Fatalf("RequestApproval with cancelled context should not fail: %v", err)
	}
}

// NORMAL: Deny with reason stores the reason.
func TestInMemoryApprovalStore_DenyWithReason(t *testing.T) {
	store := NewInMemoryApprovalStore(30 * time.Minute)
	created, _ := store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName:  "test",
		RiskLevel: "critical",
	})

	if err := store.Deny(created.ID, "admin-1", "too risky for production"); err != nil {
		t.Fatalf("Deny: %v", err)
	}

	got, _ := store.GetApproval(created.ID)
	if got.Status != execution.ApprovalDenied {
		t.Errorf("Status = %q, want %q", got.Status, execution.ApprovalDenied)
	}
	if got.DenyReason != "too risky for production" {
		t.Errorf("DenyReason = %q, want %q", got.DenyReason, "too risky for production")
	}
	if got.ApproverID != "admin-1" {
		t.Errorf("ApproverID = %q, want %q", got.ApproverID, "admin-1")
	}
}

// ABNORMAL: Expired approval cannot be approved.
func TestInMemoryApprovalStore_ExpiredCannotApprove(t *testing.T) {
	store := NewInMemoryApprovalStore(1 * time.Millisecond)
	created, _ := store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName:  "test",
		RiskLevel: "critical",
	})
	time.Sleep(5 * time.Millisecond)

	err := store.Approve(created.ID, "admin-1")
	if err == nil {
		t.Fatal("expected error approving expired request")
	}
}

// ABNORMAL: Expired approval cannot be denied.
func TestInMemoryApprovalStore_ExpiredCannotDeny(t *testing.T) {
	store := NewInMemoryApprovalStore(1 * time.Millisecond)
	created, _ := store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName:  "test",
		RiskLevel: "critical",
	})
	time.Sleep(5 * time.Millisecond)

	err := store.Deny(created.ID, "admin-1", "too late")
	if err == nil {
		t.Fatal("expected error denying expired request")
	}
}

// ABNORMAL: ListPending marks expired items lazily.
func TestInMemoryApprovalStore_ListPendingMarksExpired(t *testing.T) {
	store := NewInMemoryApprovalStore(1 * time.Millisecond)
	created, _ := store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName:  "test",
		RiskLevel: "critical",
	})
	time.Sleep(5 * time.Millisecond)

	pending := store.ListPending("")
	if len(pending) != 0 {
		t.Errorf("pending = %d, want 0 after expiry", len(pending))
	}

	// The expired request should now be marked
	got, _ := store.GetApproval(created.ID)
	if got.Status != execution.ApprovalExpired {
		t.Errorf("Status = %q, want %q after ListPending marked it", got.Status, execution.ApprovalExpired)
	}
}

// NORMAL: ListPending excludes approved/denied/expired.
func TestInMemoryApprovalStore_ListPendingExcludesResolved(t *testing.T) {
	store := NewInMemoryApprovalStore(30 * time.Minute)

	req1, _ := store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName: "step-1", RiskLevel: "critical", TenantID: "t1",
	})
	req2, _ := store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName: "step-2", RiskLevel: "critical", TenantID: "t1",
	})
	store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName: "step-3", RiskLevel: "critical", TenantID: "t1",
	})

	// Approve one, deny another
	store.Approve(req1.ID, "admin")
	store.Deny(req2.ID, "admin", "nope")

	pending := store.ListPending("t1")
	if len(pending) != 1 {
		t.Errorf("pending = %d, want 1 (only step-3 should remain)", len(pending))
	}
	if len(pending) > 0 && pending[0].StepName != "step-3" {
		t.Errorf("pending step = %q, want %q", pending[0].StepName, "step-3")
	}
}
