package loop

import (
	"context"
	"testing"
)

// =============================================================================
// ContextCompactor interface conformance
// =============================================================================

func TestDefaultContextCompactor_ImplementsInterface(t *testing.T) {
	var _ ContextCompactor = &DefaultContextCompactor{}
}

func TestNoOpCompactor_ImplementsInterface(t *testing.T) {
	var _ ContextCompactor = &NoOpCompactor{}
}

// =============================================================================
// DefaultContextCompactor tests
// =============================================================================

func TestNewDefaultContextCompactor(t *testing.T) {
	c := NewDefaultContextCompactor(4)
	if c == nil {
		t.Fatal("NewDefaultContextCompactor returned nil")
	}
}

func TestDefaultContextCompactor_EmptyInput(t *testing.T) {
	c := NewDefaultContextCompactor(4)
	result, err := c.Compact(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d items", len(result))
	}
}

func TestDefaultContextCompactor_EmptySliceInput(t *testing.T) {
	c := NewDefaultContextCompactor(4)
	result, err := c.Compact(context.Background(), []TurnResult{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d items", len(result))
	}
}

func TestDefaultContextCompactor_BelowThreshold(t *testing.T) {
	c := NewDefaultContextCompactor(4)
	input := makeTurnResults(3)

	result, err := c.Compact(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("expected 3 results (below threshold), got %d", len(result))
	}
}

func TestDefaultContextCompactor_ExactlyAtThreshold(t *testing.T) {
	c := NewDefaultContextCompactor(4)
	input := makeTurnResults(4)

	result, err := c.Compact(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 4 {
		t.Errorf("expected 4 results (at threshold), got %d", len(result))
	}
}

func TestDefaultContextCompactor_AboveThreshold_RetainsFirstAndLast(t *testing.T) {
	c := NewDefaultContextCompactor(4)
	input := makeTurnResults(8)

	result, err := c.Compact(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 4 {
		t.Errorf("expected 4 results (compacted), got %d", len(result))
	}

	// First element should be the original first
	if result[0].PlanText != "plan-0" {
		t.Errorf("first element PlanText = %q, want %q", result[0].PlanText, "plan-0")
	}

	// Last element should be the original last
	if result[len(result)-1].PlanText != "plan-7" {
		t.Errorf("last element PlanText = %q, want %q", result[len(result)-1].PlanText, "plan-7")
	}
}

func TestDefaultContextCompactor_AboveThreshold_MiddleElementsAreLatest(t *testing.T) {
	c := NewDefaultContextCompactor(4)
	input := makeTurnResults(10) // 0..9

	result, err := c.Compact(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// With maxRetained=4: keep first (0), then last 3 (7, 8, 9)
	if len(result) != 4 {
		t.Fatalf("expected 4 results, got %d", len(result))
	}
	if result[0].PlanText != "plan-0" {
		t.Errorf("result[0] = %q, want plan-0", result[0].PlanText)
	}
	if result[1].PlanText != "plan-7" {
		t.Errorf("result[1] = %q, want plan-7", result[1].PlanText)
	}
	if result[2].PlanText != "plan-8" {
		t.Errorf("result[2] = %q, want plan-8", result[2].PlanText)
	}
	if result[3].PlanText != "plan-9" {
		t.Errorf("result[3] = %q, want plan-9", result[3].PlanText)
	}
}

func TestDefaultContextCompactor_SingleElement(t *testing.T) {
	c := NewDefaultContextCompactor(4)
	input := makeTurnResults(1)

	result, err := c.Compact(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 result, got %d", len(result))
	}
}

func TestDefaultContextCompactor_TwoElements_MaxRetainedTwo(t *testing.T) {
	c := NewDefaultContextCompactor(2)
	input := makeTurnResults(5)

	result, err := c.Compact(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	if result[0].PlanText != "plan-0" {
		t.Errorf("result[0] = %q, want plan-0", result[0].PlanText)
	}
	if result[1].PlanText != "plan-4" {
		t.Errorf("result[1] = %q, want plan-4", result[1].PlanText)
	}
}

func TestDefaultContextCompactor_MaxRetainedOne(t *testing.T) {
	c := NewDefaultContextCompactor(1)
	input := makeTurnResults(5)

	result, err := c.Compact(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// With maxRetained=1, we keep only the last element
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	if result[0].PlanText != "plan-4" {
		t.Errorf("result[0] = %q, want plan-4", result[0].PlanText)
	}
}

func TestDefaultContextCompactor_DoesNotMutateInput(t *testing.T) {
	c := NewDefaultContextCompactor(2)
	input := makeTurnResults(5)
	originalLen := len(input)

	_, err := c.Compact(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(input) != originalLen {
		t.Error("Compact must not mutate the input slice")
	}
}

func TestDefaultContextCompactor_ContextCanceled(t *testing.T) {
	c := NewDefaultContextCompactor(4)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := c.Compact(ctx, makeTurnResults(5))
	if err == nil {
		t.Error("expected error on canceled context")
	}
}

// =============================================================================
// NoOpCompactor tests
// =============================================================================

func TestNoOpCompactor_PreservesAll(t *testing.T) {
	c := &NoOpCompactor{}
	input := makeTurnResults(10)

	result, err := c.Compact(context.Background(), input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 10 {
		t.Errorf("expected 10 results (no-op), got %d", len(result))
	}
}

func TestNoOpCompactor_NilInput(t *testing.T) {
	c := &NoOpCompactor{}
	result, err := c.Compact(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 results, got %d", len(result))
	}
}

func TestNoOpCompactor_EmptyInput(t *testing.T) {
	c := &NoOpCompactor{}
	result, err := c.Compact(context.Background(), []TurnResult{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected 0 results, got %d", len(result))
	}
}

// =============================================================================
// Helpers
// =============================================================================

func makeTurnResults(n int) []TurnResult {
	results := make([]TurnResult, n)
	for i := 0; i < n; i++ {
		results[i] = TurnResult{
			PlanText:      "plan-" + itoa(i),
			ToolCallsUsed: i,
		}
	}
	return results
}

func itoa(i int) string {
	return string(rune('0'+i%10)) + func() string {
		if i >= 10 {
			return string(rune('0' + i/10))
		}
		return ""
	}()
}
