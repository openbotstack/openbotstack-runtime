package loop

import (
	"context"
	"errors"
	"testing"
)

// =============================================================================
// Interface conformance
// =============================================================================

func TestNoOpCheckpoint_ImplementsCheckpoint(t *testing.T) {
	var _ Checkpoint = &NoOpCheckpoint{}
}

func TestNoOpPolicyCheckpoint_ImplementsPolicyCheckpoint(t *testing.T) {
	var _ PolicyCheckpoint = &NoOpPolicyCheckpoint{}
}

func TestCompositeCheckpoint_ImplementsCheckpoint(t *testing.T) {
	var _ Checkpoint = &CompositeCheckpoint{}
}

// =============================================================================
// NoOpCheckpoint tests
// =============================================================================

func TestNoOpCheckpoint_Save_ReturnsNil(t *testing.T) {
	cp := &NoOpCheckpoint{}
	err := cp.Save(context.Background(), 0, nil, nil)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestNoOpCheckpoint_Save_WithValues(t *testing.T) {
	cp := &NoOpCheckpoint{}
	tr := &TaskResult{TurnCount: 3, ToolCallsUsed: 5}
	m := &LoopMetrics{WorkflowSteps: 2}
	err := cp.Save(context.Background(), 1, tr, m)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestNoOpCheckpoint_Save_CanceledContext(t *testing.T) {
	cp := &NoOpCheckpoint{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := cp.Save(ctx, 0, nil, nil)
	if err != nil {
		t.Errorf("NoOpCheckpoint should ignore canceled context, got %v", err)
	}
}

// =============================================================================
// NoOpPolicyCheckpoint tests
// =============================================================================

func TestNoOpPolicyCheckpoint_Check_ReturnsNil(t *testing.T) {
	cp := &NoOpPolicyCheckpoint{}
	err := cp.Check(context.Background(), 0, nil)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

func TestNoOpPolicyCheckpoint_Check_WithValues(t *testing.T) {
	cp := &NoOpPolicyCheckpoint{}
	m := &LoopMetrics{WorkflowSteps: 3, TotalTurns: 10}
	err := cp.Check(context.Background(), 2, m)
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
}

// =============================================================================
// CompositeCheckpoint tests
// =============================================================================

func TestCompositeCheckpoint_EmptyChain(t *testing.T) {
	cp := NewCompositeCheckpoint()
	err := cp.Save(context.Background(), 0, nil, nil)
	if err != nil {
		t.Errorf("empty composite should succeed, got %v", err)
	}
}

func TestCompositeCheckpoint_SingleCheckpoint(t *testing.T) {
	called := false
	mock := &mockCheckpoint{saveFunc: func(ctx context.Context, taskIndex int, taskResult *TaskResult, metrics *LoopMetrics) error {
		called = true
		return nil
	}}
	cp := NewCompositeCheckpoint(mock)
	err := cp.Save(context.Background(), 0, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Error("checkpoint was not called")
	}
}

func TestCompositeCheckpoint_MultipleCheckpoints_AllCalled(t *testing.T) {
	callCount := 0
	makeMock := func() *mockCheckpoint {
		return &mockCheckpoint{saveFunc: func(ctx context.Context, taskIndex int, taskResult *TaskResult, metrics *LoopMetrics) error {
			callCount++
			return nil
		}}
	}
	cp := NewCompositeCheckpoint(makeMock(), makeMock(), makeMock())
	err := cp.Save(context.Background(), 0, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if callCount != 3 {
		t.Errorf("expected 3 calls, got %d", callCount)
	}
}

func TestCompositeCheckpoint_ErrorPropagation_StopsOnFirst(t *testing.T) {
	expectedErr := errors.New("checkpoint failed")
	callCount := 0
	mock1 := &mockCheckpoint{saveFunc: func(ctx context.Context, taskIndex int, taskResult *TaskResult, metrics *LoopMetrics) error {
		callCount++
		return expectedErr
	}}
	mock2 := &mockCheckpoint{saveFunc: func(ctx context.Context, taskIndex int, taskResult *TaskResult, metrics *LoopMetrics) error {
		callCount++
		return nil
	}}
	cp := NewCompositeCheckpoint(mock1, mock2)
	err := cp.Save(context.Background(), 0, nil, nil)
	if !errors.Is(err, expectedErr) {
		t.Errorf("expected %v, got %v", expectedErr, err)
	}
	if callCount != 1 {
		t.Errorf("expected 1 call (stop on first error), got %d", callCount)
	}
}

func TestCompositeCheckpoint_PassesArgumentsCorrectly(t *testing.T) {
	var capturedIndex int
	var capturedResult *TaskResult
	var capturedMetrics *LoopMetrics

	mock := &mockCheckpoint{saveFunc: func(ctx context.Context, taskIndex int, taskResult *TaskResult, metrics *LoopMetrics) error {
		capturedIndex = taskIndex
		capturedResult = taskResult
		capturedMetrics = metrics
		return nil
	}}

	tr := &TaskResult{TurnCount: 5}
	m := &LoopMetrics{WorkflowSteps: 2}
	cp := NewCompositeCheckpoint(mock)
	_ = cp.Save(context.Background(), 3, tr, m)

	if capturedIndex != 3 {
		t.Errorf("taskIndex = %d, want 3", capturedIndex)
	}
	if capturedResult != tr {
		t.Error("taskResult pointer mismatch")
	}
	if capturedMetrics != m {
		t.Error("metrics pointer mismatch")
	}
}

// =============================================================================
// Mock helpers
// =============================================================================

type mockCheckpoint struct {
	saveFunc func(ctx context.Context, taskIndex int, taskResult *TaskResult, metrics *LoopMetrics) error
}

func (m *mockCheckpoint) Save(ctx context.Context, taskIndex int, taskResult *TaskResult, metrics *LoopMetrics) error {
	return m.saveFunc(ctx, taskIndex, taskResult, metrics)
}
