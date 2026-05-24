package harness

import (
	"context"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
)

// --- Harness Approval Integration Tests ---
//
// TDD: RED phase. Tests for harness approval gateway integration.

// mockApprovalGateway is a controllable approval gateway for tests.
type mockApprovalGateway struct {
	store     *InMemoryApprovalStore
	approveCh chan string // send approval ID to auto-approve
	denyCh    chan string // send approval ID to auto-deny
}

func newMockApprovalGateway() *mockApprovalGateway {
	return &mockApprovalGateway{
		store:     NewInMemoryApprovalStore(30 * time.Minute),
		approveCh: make(chan string, 10),
		denyCh:    make(chan string, 10),
	}
}

func (m *mockApprovalGateway) RequestApproval(ctx context.Context, req *execution.ApprovalRequest) (*execution.ApprovalRequest, error) {
	return m.store.RequestApproval(ctx, req)
}
func (m *mockApprovalGateway) GetApproval(id string) (*execution.ApprovalRequest, error) {
	return m.store.GetApproval(id)
}
func (m *mockApprovalGateway) Approve(id, approverID string) error {
	return m.store.Approve(id, approverID)
}
func (m *mockApprovalGateway) Deny(id, approverID, reason string) error {
	return m.store.Deny(id, approverID, reason)
}
func (m *mockApprovalGateway) ListPending(tenantID string) []execution.ApprovalRequest {
	return m.store.ListPending(tenantID)
}

// NORMAL: Critical step gets approved and execution continues.
func TestHarness_CriticalStepApproval(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["safe_step"] = map[string]any{"status": "ok"}
	tr.result["critical_step"] = map[string]any{"status": "executed"}

	gw := newMockApprovalGateway()
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})
	h.SetApprovalGateway(gw)

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "safe_step", Type: execution.StepTypeTool, RiskLevel: "info"},
		execution.ExecutionStep{Name: "critical_step", Type: execution.StepTypeTool, RiskLevel: "critical"},
	)
	ec := testEC()

	// Run in goroutine; approve when we see the request appear
	done := make(chan *HarnessResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := h.Run(context.Background(), plan, ec)
		if err != nil {
			errCh <- err
			return
		}
		done <- result
	}()

	// Wait for approval request to appear, then approve
	for i := 0; i < 100; i++ {
		pending := gw.ListPending("")
		if len(pending) > 0 {
			gw.Approve(pending[0].ID, "test-admin")
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case result := <-done:
		if result.StepsExecuted != 2 {
			t.Errorf("StepsExecuted = %d, want 2", result.StepsExecuted)
		}
		if result.StopCondition.Reason != StopReasonGoalAchieved {
			t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonGoalAchieved)
		}
	case err := <-errCh:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for harness to complete")
	}
}

// ABNORMAL: Critical step denied stops execution.
func TestHarness_CriticalStepDenied(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["safe_step"] = map[string]any{"status": "ok"}

	gw := newMockApprovalGateway()
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})
	h.SetApprovalGateway(gw)

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "safe_step", Type: execution.StepTypeTool, RiskLevel: "info"},
		execution.ExecutionStep{Name: "critical_step", Type: execution.StepTypeTool, RiskLevel: "critical"},
	)
	ec := testEC()

	done := make(chan *HarnessResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := h.Run(context.Background(), plan, ec)
		if err != nil {
			errCh <- err
			return
		}
		done <- result
	}()

	// Wait for approval request, then deny
	for i := 0; i < 100; i++ {
		pending := gw.ListPending("")
		if len(pending) > 0 {
			gw.Deny(pending[0].ID, "test-admin", "too risky")
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	select {
	case result := <-done:
		if result.StepsExecuted != 1 {
			t.Errorf("StepsExecuted = %d, want 1 (only safe_step should run)", result.StepsExecuted)
		}
		if result.StopCondition.Reason != StopReasonApprovalDenied {
			t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonApprovalDenied)
		}
	case err := <-errCh:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for harness to complete")
	}
}

// ABNORMAL: Approval timeout stops execution.
func TestHarness_CriticalStepApprovalTimeout(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["critical_step"] = map[string]any{"status": "ok"}

	gw := newMockApprovalGateway()
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})
	h.SetApprovalGateway(gw)

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "critical_step", Type: execution.StepTypeTool, RiskLevel: "critical"},
	)
	ec := testEC()

	// Use a context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	result, err := h.Run(ctx, plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopCondition.Reason != StopReasonApprovalTimeout {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonApprovalTimeout)
	}
	if !result.StopCondition.Stopped {
		t.Error("expected stopped = true")
	}
}

// NORMAL: Non-critical steps skip approval entirely.
func TestHarness_NonCriticalStepSkipsApproval(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["info_step"] = map[string]any{"status": "ok"}
	tr.result["sensitive_step"] = map[string]any{"status": "ok"}
	tr.result["clinical_step"] = map[string]any{"status": "ok"}

	gw := newMockApprovalGateway()
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})
	h.SetApprovalGateway(gw)

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "info_step", Type: execution.StepTypeTool, RiskLevel: "info"},
		execution.ExecutionStep{Name: "sensitive_step", Type: execution.StepTypeTool, RiskLevel: "sensitive"},
		execution.ExecutionStep{Name: "clinical_step", Type: execution.StepTypeTool, RiskLevel: "clinical"},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.StepsExecuted != 3 {
		t.Errorf("StepsExecuted = %d, want 3", result.StepsExecuted)
	}
	// No approval requests should have been created
	pending := gw.ListPending("")
	if len(pending) != 0 {
		t.Errorf("pending approvals = %d, want 0 (non-critical steps should not require approval)", len(pending))
	}
}

// NORMAL: Nil approval gateway means critical steps execute without approval.
func TestHarness_NilApprovalGateway(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["critical_step"] = map[string]any{"status": "ok"}

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})
	// No SetApprovalGateway called — gateway is nil

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "critical_step", Type: execution.StepTypeTool, RiskLevel: "critical"},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.StepsExecuted != 1 {
		t.Errorf("StepsExecuted = %d, want 1", result.StepsExecuted)
	}
	if result.StopCondition.Reason != StopReasonGoalAchieved {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonGoalAchieved)
	}
}

// ABNORMAL: Multiple critical steps each require separate approval.
func TestHarness_MultipleCriticalSteps(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["critical_1"] = map[string]any{"status": "ok"}
	tr.result["critical_2"] = map[string]any{"status": "ok"}

	gw := newMockApprovalGateway()
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})
	h.SetApprovalGateway(gw)

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "critical_1", Type: execution.StepTypeTool, RiskLevel: "critical"},
		execution.ExecutionStep{Name: "critical_2", Type: execution.StepTypeTool, RiskLevel: "critical"},
	)
	ec := testEC()

	done := make(chan *HarnessResult, 1)
	errCh := make(chan error, 1)
	go func() {
		result, err := h.Run(context.Background(), plan, ec)
		if err != nil {
			errCh <- err
			return
		}
		done <- result
	}()

	// Approve both steps as they come in
	approved := 0
	for approved < 2 {
		pending := gw.ListPending("")
		for _, p := range pending {
			gw.Approve(p.ID, "test-admin")
			approved++
		}
		if approved < 2 {
			time.Sleep(10 * time.Millisecond)
		}
	}

	select {
	case result := <-done:
		if result.StepsExecuted != 2 {
			t.Errorf("StepsExecuted = %d, want 2", result.StepsExecuted)
		}
		if result.StopCondition.Reason != StopReasonGoalAchieved {
			t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonGoalAchieved)
		}
	case err := <-errCh:
		t.Fatalf("unexpected error: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout waiting for harness to complete")
	}
}
