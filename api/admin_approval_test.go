package api

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/access/auth"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-runtime/api/middleware"
	"github.com/openbotstack/openbotstack-runtime/harness"
	"github.com/openbotstack/openbotstack-runtime/persistence"
)

// NewTestApprovalStore creates an in-memory approval store for testing.
func NewTestApprovalStore(ttl ...time.Duration) execution.ApprovalGateway {
	d := 30 * time.Minute
	if len(ttl) > 0 {
		d = ttl[0]
	}
	return harness.NewInMemoryApprovalStore(d)
}

// --- Admin Approval API Tests ---
//
// TDD: RED phase. Tests for admin approval endpoints.

// setupApprovalTest creates an admin test environment with the approval gateway wired in.
func setupApprovalTest(t *testing.T, store execution.ApprovalGateway) http.Handler {
	t.Helper()
	db, err := persistence.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if _, err := db.SeedDefaults(); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}

	adminRouter := NewAdminRouter(AdminRouterConfig{
		DB:               db.DB,
		ApprovalGateway:  store,
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := &auth.User{ID: "admin", TenantID: "default", Name: "Admin"}
		ctx := middleware.WithUser(r.Context(), user)
		ctx = middleware.WithUserRole(ctx, "admin")
		adminRouter.Handler().ServeHTTP(w, r.WithContext(ctx))
	})
	return handler
}

// NORMAL: Full flow — list (empty) → request → list → get → approve → get
func TestAdminApproval_FullFlow(t *testing.T) {
	store := NewTestApprovalStore()
	handler := setupApprovalTest(t, store)

	// 1. List empty
	rec := doAdminRequest(t, handler, "GET", "/v1/admin/approval", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var empty []execution.ApprovalRequest
	if err := json.NewDecoder(rec.Body).Decode(&empty); err != nil {
		t.Fatalf("decode empty list: %v", err)
	}
	if len(empty) != 0 {
		t.Errorf("initial list = %d items, want 0", len(empty))
	}

	// Create a request in the store directly
	created, err := store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName:    "critical_action",
		StepID:      "step-1",
		ExecutionID: "exec-1",
		TenantID:    "default",
		RiskLevel:   "critical",
		Reason:      "requires approval",
	})
	if err != nil {
		t.Fatalf("RequestApproval: %v", err)
	}

	// 2. List with one pending
	rec = doAdminRequest(t, handler, "GET", "/v1/admin/approval?tenant_id=default", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var list []execution.ApprovalRequest
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("list = %d items, want 1", len(list))
	}

	// 3. Get individual
	rec = doAdminRequest(t, handler, "GET", "/v1/admin/approval/"+created.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
	var got execution.ApprovalRequest
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode get: %v", err)
	}
	if got.Status != execution.ApprovalPending {
		t.Errorf("status = %q, want %q", got.Status, execution.ApprovalPending)
	}

	// 4. Approve
	rec = doAdminRequest(t, handler, "POST", "/v1/admin/approval/"+created.ID+"/approve", map[string]string{
		"approver_id": "admin",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("approve status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// 5. Verify approved
	rec = doAdminRequest(t, handler, "GET", "/v1/admin/approval/"+created.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get-after-approve status = %d; body: %s", rec.Code, rec.Body.String())
	}
	var approved execution.ApprovalRequest
	if err := json.NewDecoder(rec.Body).Decode(&approved); err != nil {
		t.Fatalf("decode approved: %v", err)
	}
	if approved.Status != execution.ApprovalApproved {
		t.Errorf("status after approve = %q, want %q", approved.Status, execution.ApprovalApproved)
	}
}

// ABNORMAL: List empty (no requests).
func TestAdminApproval_ListEmpty(t *testing.T) {
	store := NewTestApprovalStore()
	handler := setupApprovalTest(t, store)

	rec := doAdminRequest(t, handler, "GET", "/v1/admin/approval", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	var list []execution.ApprovalRequest
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("list = %d items, want 0", len(list))
	}
}

// ABNORMAL: Get not found.
func TestAdminApproval_GetNotFound(t *testing.T) {
	store := NewTestApprovalStore()
	handler := setupApprovalTest(t, store)

	rec := doAdminRequest(t, handler, "GET", "/v1/admin/approval/nonexistent-id", nil)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// ABNORMAL: Approve not found.
func TestAdminApproval_ApproveNotFound(t *testing.T) {
	store := NewTestApprovalStore()
	handler := setupApprovalTest(t, store)

	rec := doAdminRequest(t, handler, "POST", "/v1/admin/approval/nonexistent-id/approve", map[string]string{
		"approver_id": "admin",
	})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// ABNORMAL: Deny not found.
func TestAdminApproval_DenyNotFound(t *testing.T) {
	store := NewTestApprovalStore()
	handler := setupApprovalTest(t, store)

	rec := doAdminRequest(t, handler, "POST", "/v1/admin/approval/nonexistent-id/deny", map[string]string{
		"approver_id": "admin",
		"reason":      "unsafe",
	})
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

// NORMAL: List by tenant filters correctly.
func TestAdminApproval_ListByTenant(t *testing.T) {
	store := NewTestApprovalStore()
	handler := setupApprovalTest(t, store)

	store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName: "step-1", RiskLevel: "critical", TenantID: "tenant-A",
	})
	store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName: "step-2", RiskLevel: "critical", TenantID: "tenant-B",
	})

	rec := doAdminRequest(t, handler, "GET", "/v1/admin/approval?tenant_id=tenant-A", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}
	var list []execution.ApprovalRequest
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("tenant-A list = %d, want 1", len(list))
	}
	if len(list) > 0 && list[0].TenantID != "tenant-A" {
		t.Errorf("tenant = %q, want %q", list[0].TenantID, "tenant-A")
	}
}

// ABNORMAL: Not configured returns 503.
func TestAdminApproval_NotConfigured(t *testing.T) {
	// Use nil gateway
	handler := setupApprovalTest(t, nil)

	rec := doAdminRequest(t, handler, "GET", "/v1/admin/approval", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
	}
}

// ABNORMAL: Wrong method returns 405.
func TestAdminApproval_WrongMethod(t *testing.T) {
	store := NewTestApprovalStore()
	handler := setupApprovalTest(t, store)

	rec := doAdminRequest(t, handler, "PUT", "/v1/admin/approval", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusMethodNotAllowed, rec.Body.String())
	}
}

// ABNORMAL: Double approve returns error.
func TestAdminApproval_DoubleApprove(t *testing.T) {
	store := NewTestApprovalStore()
	handler := setupApprovalTest(t, store)

	created, _ := store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName: "step-1", RiskLevel: "critical",
	})

	// First approve
	rec := doAdminRequest(t, handler, "POST", "/v1/admin/approval/"+created.ID+"/approve", map[string]string{
		"approver_id": "admin",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("first approve status = %d; body: %s", rec.Code, rec.Body.String())
	}

	// Second approve
	rec = doAdminRequest(t, handler, "POST", "/v1/admin/approval/"+created.ID+"/approve", map[string]string{
		"approver_id": "admin-2",
	})
	if rec.Code == http.StatusOK {
		t.Error("expected non-200 for double approve")
	}
}

// ABNORMAL: Deny after approve returns error.
func TestAdminApproval_DenyAfterApprove(t *testing.T) {
	store := NewTestApprovalStore()
	handler := setupApprovalTest(t, store)

	created, _ := store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName: "step-1", RiskLevel: "critical",
	})

	// Approve first
	doAdminRequest(t, handler, "POST", "/v1/admin/approval/"+created.ID+"/approve", map[string]string{
		"approver_id": "admin",
	})

	// Then deny
	rec := doAdminRequest(t, handler, "POST", "/v1/admin/approval/"+created.ID+"/deny", map[string]string{
		"approver_id": "admin",
		"reason":      "changed mind",
	})
	if rec.Code == http.StatusOK {
		t.Error("expected non-200 for deny after approve")
	}
}

// ABNORMAL: Expired approval is visible but immutable.
func TestAdminApproval_ExpiredApproval(t *testing.T) {
	store := NewTestApprovalStore(1 * time.Millisecond)
	handler := setupApprovalTest(t, store)

	created, _ := store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName: "step-1", RiskLevel: "critical",
	})

	// Wait for expiry
	time.Sleep(5 * time.Millisecond)

	// Get should show expired
	rec := doAdminRequest(t, handler, "GET", "/v1/admin/approval/"+created.ID, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d; body: %s", rec.Code, rec.Body.String())
	}
	var got execution.ApprovalRequest
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Status != execution.ApprovalExpired {
		t.Errorf("status = %q, want %q", got.Status, execution.ApprovalExpired)
	}
}

// NORMAL: Deny with reason via API.
func TestAdminApproval_DenyWithReason(t *testing.T) {
	store := NewTestApprovalStore()
	handler := setupApprovalTest(t, store)

	created, _ := store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName: "step-1", RiskLevel: "critical",
	})

	rec := doAdminRequest(t, handler, "POST", "/v1/admin/approval/"+created.ID+"/deny", map[string]string{
		"approver_id": "admin",
		"reason":      "not safe for production",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("deny status = %d; body: %s", rec.Code, rec.Body.String())
	}

	// Verify
	got, _ := store.GetApproval(created.ID)
	if got.Status != execution.ApprovalDenied {
		t.Errorf("status = %q, want %q", got.Status, execution.ApprovalDenied)
	}
	if got.DenyReason != "not safe for production" {
		t.Errorf("deny_reason = %q, want %q", got.DenyReason, "not safe for production")
	}
}

// ABNORMAL: Approve without approver_id returns 400.
func TestAdminApproval_ApproveMissingApproverID(t *testing.T) {
	store := NewTestApprovalStore()
	handler := setupApprovalTest(t, store)

	created, _ := store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName: "step-1", RiskLevel: "critical",
	})

	rec := doAdminRequest(t, handler, "POST", "/v1/admin/approval/"+created.ID+"/approve", map[string]string{})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

// ABNORMAL: Deny without approver_id returns 400.
func TestAdminApproval_DenyMissingApproverID(t *testing.T) {
	store := NewTestApprovalStore()
	handler := setupApprovalTest(t, store)

	created, _ := store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName: "step-1", RiskLevel: "critical",
	})

	rec := doAdminRequest(t, handler, "POST", "/v1/admin/approval/"+created.ID+"/deny", map[string]string{
		"reason": "nope",
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

// ABNORMAL: Expired approval cannot be approved via API.
func TestAdminApproval_ExpiredCannotApprove(t *testing.T) {
	store := NewTestApprovalStore(1 * time.Millisecond)
	handler := setupApprovalTest(t, store)

	created, _ := store.RequestApproval(context.Background(), &execution.ApprovalRequest{
		StepName: "step-1", RiskLevel: "critical",
	})
	time.Sleep(5 * time.Millisecond)

	rec := doAdminRequest(t, handler, "POST", "/v1/admin/approval/"+created.ID+"/approve", map[string]string{
		"approver_id": "admin",
	})
	if rec.Code == http.StatusOK {
		t.Error("expected non-200 for approving expired request")
	}
}
