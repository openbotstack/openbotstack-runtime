package harness

import (
	"context"
	"testing"

	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-core/execution"
)

// --- Audit Tests ---

func TestAuditLayer_RecordStep(t *testing.T) {
	al := NewAuditLayer()
	err := al.RecordStep(context.Background(), audit.AuditEvent{
		StepName: "test-step",
		StepType: string(execution.StepTypeTool),
		Status:   "success",
	})
	if err != nil {
		t.Fatalf("RecordStep: %v", err)
	}
	if al.TrailSize() != 1 {
		t.Errorf("TrailSize = %d, want 1", al.TrailSize())
	}
	trail := al.Trail()
	if trail[0].StepName != "test-step" {
		t.Errorf("StepName = %q, want 'test-step'", trail[0].StepName)
	}
	if trail[0].TraceID == "" {
		t.Error("TraceID should be auto-generated")
	}
}

func TestAuditLayer_AppendOnly(t *testing.T) {
	al := NewAuditLayer()
	for i := 0; i < 5; i++ {
		_ = al.RecordStep(context.Background(), audit.AuditEvent{
			StepName: "step",
			Status:   "ok",
		})
	}
	if al.TrailSize() != 5 {
		t.Errorf("TrailSize = %d, want 5", al.TrailSize())
	}
}

func TestAuditLayer_TrailIsCopy(t *testing.T) {
	al := NewAuditLayer()
	_ = al.RecordStep(context.Background(), audit.AuditEvent{StepName: "original"})
	trail := al.Trail()
	trail[0].StepName = "modified"
	original := al.Trail()
	if original[0].StepName != "original" {
		t.Error("Trail() should return a copy, not a reference")
	}
}

// --- Context Control Tests ---

func TestCompactionTrigger_ShouldCompact(t *testing.T) {
	ct := CompactionTrigger{MaxTurns: 4, MaxTokens: 8000}
	if ct.ShouldCompact(3, 1000) {
		t.Error("should not compact below thresholds")
	}
	if !ct.ShouldCompact(5, 1000) {
		t.Error("should compact when turns exceed MaxTurns")
	}
	if !ct.ShouldCompact(3, 10000) {
		t.Error("should compact when tokens exceed MaxTokens")
	}
}

func TestThresholdCompactionStrategy_Compact(t *testing.T) {
	strategy := NewThresholdCompactionStrategy(DefaultCompactionTrigger(), 3)
	turns := []TurnResult{
		{TurnNumber: 1, PlanText: "first"},
		{TurnNumber: 2, PlanText: "second"},
		{TurnNumber: 3, PlanText: "third"},
		{TurnNumber: 4, PlanText: "fourth"},
		{TurnNumber: 5, PlanText: "fifth"},
	}
	result, err := strategy.Compact(context.Background(), turns)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	// Should keep first + last 2 = 3 turns
	if len(result) != 3 {
		t.Fatalf("expected 3 turns, got %d", len(result))
	}
	if result[0].TurnNumber != 1 {
		t.Errorf("first turn should be kept, got turn %d", result[0].TurnNumber)
	}
	if result[1].TurnNumber != 4 {
		t.Errorf("second result should be turn 4, got %d", result[1].TurnNumber)
	}
	if result[2].TurnNumber != 5 {
		t.Errorf("third result should be turn 5, got %d", result[2].TurnNumber)
	}
}

func TestThresholdCompactionStrategy_NoCompactNeeded(t *testing.T) {
	strategy := NewThresholdCompactionStrategy(DefaultCompactionTrigger(), 3)
	turns := []TurnResult{
		{TurnNumber: 1},
		{TurnNumber: 2},
	}
	result, err := strategy.Compact(context.Background(), turns)
	if err != nil {
		t.Fatalf("Compact: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("should not compact 2 turns, got %d", len(result))
	}
}

func TestEstimateTokens(t *testing.T) {
	turns := []TurnResult{
		{PlanText: "12345678"},                    // 8 chars → 2 tokens
		{Observations: []string{"1234567812"}},    // 10 chars → 2 tokens
	}
	tokens := EstimateTokens(turns)
	if tokens != 4 {
		t.Errorf("EstimateTokens = %d, want 4", tokens)
	}
}

// --- SubAgent Tests ---

func TestSubAgent_NoPlan(t *testing.T) {
	sa := NewSubAgent(SubAgentConfig{}, nil)
	ec := execution.NewExecutionContext(context.Background(), "req", "asst", "sess", "tenant", "user")
	_, err := sa.Run(context.Background(), ec)
	if err == nil {
		t.Fatal("expected error for no plan")
	}
}

func TestSubAgent_UnfrozenPlan(t *testing.T) {
	plan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{{Name: "test", Type: execution.StepTypeTool}},
	}
	sa := NewSubAgent(SubAgentConfig{Plan: plan}, nil)
	ec := execution.NewExecutionContext(context.Background(), "req", "asst", "sess", "tenant", "user")
	_, err := sa.Run(context.Background(), ec)
	if err == nil {
		t.Fatal("expected error for unfrozen plan")
	}
}
