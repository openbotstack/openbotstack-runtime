package harness

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-core/execution"
)

// --- Phase 3: Execution Trace Verification ---
//
// Verify every request produces a complete, reproducible execution trace.

// Trace 1: Step-by-step results captured in HarnessResult
func TestTrace_StepByStepResults(t *testing.T) {
	cfg := DefaultHarnessConfig()

	tr := newMockToolRunner()
	tr.result["step-a"] = "result-a"
	tr.result["step-b"] = "result-b"
	tr.result["step-c"] = "result-c"

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "step-a", Type: execution.StepTypeTool, Arguments: map[string]any{"key": "val"}},
		execution.ExecutionStep{Name: "step-b", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "step-c", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(result.StepResults) != 3 {
		t.Fatalf("StepResults count = %d, want 3", len(result.StepResults))
	}

	// Verify each step result has required fields
	for i, sr := range result.StepResults {
		if sr.StepID == "" {
			t.Errorf("StepResults[%d].StepID is empty", i)
		}
		if sr.StepName == "" {
			t.Errorf("StepResults[%d].StepName is empty", i)
		}
		if sr.Duration == 0 {
			t.Errorf("StepResults[%d].Duration is 0", i)
		}
		if sr.Type != string(execution.StepTypeTool) {
			t.Errorf("StepResults[%d].StepType = %q, want 'tool'", i, sr.Type)
		}
	}

	// Verify step names in order
	expectedNames := []string{"step-a", "step-b", "step-c"}
	for i, name := range expectedNames {
		if result.StepResults[i].StepName != name {
			t.Errorf("StepResults[%d].StepName = %q, want %q", i, result.StepResults[i].StepName, name)
		}
	}

	// Verify outputs preserved
	if result.StepResults[0].Output != "result-a" {
		t.Errorf("StepResults[0].Output = %v, want 'result-a'", result.StepResults[0].Output)
	}
}

// Trace 2: Error step has error captured
func TestTrace_ErrorStepCaptured(t *testing.T) {
	cfg := DefaultHarnessConfig()

	tr := newMockToolRunner()
	tr.result["ok-step"] = "ok"
	tr.err["fail-step"] = fmt.Errorf("connection refused")

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "ok-step", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "fail-step", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error at harness level: %v", err)
	}

	// Step 1: success, Step 2: failure
	if result.StepResults[0].Error != nil {
		t.Errorf("step[0] should succeed, got error: %v", result.StepResults[0].Error)
	}
	if result.StepResults[1].Error == nil {
		t.Error("step[1] should have error")
	}
	if result.StepResults[1].Error.Error() != "connection refused" {
		t.Errorf("step[1].Error = %v, want 'connection refused'", result.StepResults[1].Error)
	}
}

// Trace 3: Reproducibility — same input → same step sequence
func TestTrace_Reproducibility(t *testing.T) {
	cfg := DefaultHarnessConfig()

	runOnce := func() []string {
		tr := newMockToolRunner()
		tr.result["step-1"] = "a"
		tr.result["step-2"] = "b"

		h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})
		plan := makeFrozenPlan(
			execution.ExecutionStep{Name: "step-1", Type: execution.StepTypeTool, Arguments: map[string]any{}},
			execution.ExecutionStep{Name: "step-2", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		)
		ec := testEC()

		result, _ := h.Run(context.Background(), plan, ec)
		names := make([]string, len(result.StepResults))
		for i, sr := range result.StepResults {
			names[i] = sr.StepName
		}
		return names
	}

	run1 := runOnce()
	run2 := runOnce()

	if len(run1) != len(run2) {
		t.Fatalf("different lengths: %d vs %d", len(run1), len(run2))
	}
	for i := range run1 {
		if run1[i] != run2[i] {
			t.Errorf("step[%d]: %q vs %q — not reproducible", i, run1[i], run2[i])
		}
	}
}

// Trace 4: ReasoningLoop turn trace
func TestTrace_ReasoningLoopTurnTrace(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 3
	cfg.RepeatPlanStop = false

	plan := &execution.ExecutionPlan{
		Steps:     []execution.ExecutionStep{{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}}},
		Reasoning: "need to check something",
	}
	_ = plan.Validate()

	mp := &mockPlanner{plans: []*execution.ExecutionPlan{plan, plan, plan}}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TurnCount != 3 {
		t.Errorf("TurnCount = %d, want 3", result.TurnCount)
	}
	if result.Duration == 0 {
		t.Error("Duration should be > 0")
	}
	if result.ToolCalls != 3 {
		t.Errorf("ToolCalls = %d, want 3 (1 per turn)", result.ToolCalls)
	}
	if result.StopReason != StopReasonMaxTurns {
		t.Errorf("StopReason = %q, want %q", result.StopReason, StopReasonMaxTurns)
	}
}

// Trace 5: Metrics completeness
func TestTrace_MetricsCompleteness(t *testing.T) {
	cfg := DefaultHarnessConfig()

	tr := newMockToolRunner()
	tr.result["tool-a"] = "result"
	skillExec := &mockSkillExecutor{resp: []byte(`{"ok":true}`)}

	h := NewExecutionHarness(cfg, tr, skillExec, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "skill-b", Type: execution.StepTypeSkill, Arguments: map[string]any{}},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Metrics.TotalSteps != 2 {
		t.Errorf("TotalSteps = %d, want 2", result.Metrics.TotalSteps)
	}
	if result.Duration == 0 {
		t.Error("Duration should be > 0")
	}
	if result.StepsExecuted != len(result.StepResults) {
		t.Errorf("StepsExecuted (%d) != len(StepResults) (%d)", result.StepsExecuted, len(result.StepResults))
	}

	for i, sr := range result.StepResults {
		if sr.StepName == "" {
			t.Errorf("StepResults[%d].StepName is empty", i)
		}
		if sr.StepID == "" {
			t.Errorf("StepResults[%d].StepID is empty", i)
		}
	}
}

// Trace 6: AuditLayer records all steps
func TestTrace_AuditLayerRecordsAll(t *testing.T) {
	al := NewAuditLayer()

	// Manually record steps (simulating what StepExecutor.logStep would do)
	steps := []struct {
		name   string
		status string
	}{
		{"step-a", "success"},
		{"step-b", "failed"},
		{"step-c", "success"},
	}

	for _, s := range steps {
		err := al.RecordStep(context.Background(), audit.AuditEvent{
			StepName:  s.name,
			StepType:  string(execution.StepTypeTool),
			Status:    s.status,
			Timestamp: time.Now(),
		})
		if err != nil {
			t.Fatalf("RecordStep failed: %v", err)
		}
	}

	if al.TrailSize() != 3 {
		t.Fatalf("TrailSize = %d, want 3", al.TrailSize())
	}

	trail := al.Trail()

	// Verify append-only (order preserved)
	if trail[0].StepName != "step-a" || trail[1].StepName != "step-b" || trail[2].StepName != "step-c" {
		t.Errorf("trail order wrong: %s, %s, %s", trail[0].StepName, trail[1].StepName, trail[2].StepName)
	}

	// Verify error step
	if trail[1].Status != "failed" {
		t.Errorf("trail[1].Status = %q, want 'failed'", trail[1].Status)
	}

	// Verify timestamps monotonically increasing
	for i := 1; i < len(trail); i++ {
		if trail[i].Timestamp.Before(trail[i-1].Timestamp) {
			t.Errorf("trail[%d] timestamp before trail[%d]", i, i-1)
		}
	}

	// Verify trail is a copy (modifying doesn't affect original)
	trail[0].StepName = "modified"
	original := al.Trail()
	if original[0].StepName != "step-a" {
		t.Error("Trail() should return a copy, not a reference")
	}
}
