package harness

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
)

// --- Phase 1: End-to-End Harness Tests ---
//
// Black-box tests simulating domain workflows through the ExecutionHarness.
// Domain tools are simulated via mockToolRunner with realistic outputs,
// respecting the repo boundary (runtime does NOT import apps).

// E2E Test 1: Patient Query → Vitals → Labs → Risk Score
func TestE2E_PatientQueryToRiskScore(t *testing.T) {
	cfg := DefaultHarnessConfig()

	// Simulated domain tools via mockToolRunner
	tr := newMockToolRunner()
	tr.result["ehr.query_patient"] = map[string]any{
		"id": "P001", "name": "Zhang Wei", "age": 72, "gender": "M",
		"unit": "ICU", "bed_number": "ICU-01", "diagnosis": "Sepsis",
	}
	tr.result["ehr.query_vitals"] = map[string]any{
		"patient_id": "P001", "heart_rate": 110, "systolic_bp": 85,
		"spo2": 92, "respiratory_rate": 24, "temperature": 38.9,
	}
	tr.result["ehr.query_labs"] = map[string]any{
		"patient_id": "P001", "abnormal_count": 3,
		"results": []any{"WBC: elevated", "CRP: elevated", "Procalcitonin: elevated"},
	}
	tr.result["analytics.risk_score"] = map[string]any{
		"patient_id": "P001", "score": 80.0, "level": "critical",
		"contributors": []any{"elevated HR (110)", "low SBP (85)", "low SpO2 (92)"},
	}

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "ehr.query_patient", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
		execution.ExecutionStep{Name: "ehr.query_vitals", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
		execution.ExecutionStep{Name: "ehr.query_labs", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
		execution.ExecutionStep{
			Name: "analytics.risk_score",
			Type: execution.StepTypeTool,
			Arguments: map[string]any{
				"patient_id":        "P001",
				"heart_rate":        "{{ehr.query_vitals.heart_rate}}",
				"systolic_bp":       "{{ehr.query_vitals.systolic_bp}}",
				"spo2":              "{{ehr.query_vitals.spo2}}",
				"abnormal_lab_count": "{{ehr.query_labs.abnormal_count}}",
			},
		},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify step sequence correctness
	if result.StepsExecuted != 4 {
		t.Fatalf("StepsExecuted = %d, want 4", result.StepsExecuted)
	}

	expectedSequence := []string{"ehr.query_patient", "ehr.query_vitals", "ehr.query_labs", "analytics.risk_score"}
	for i, name := range expectedSequence {
		if result.StepResults[i].StepName != name {
			t.Errorf("step[%d] = %q, want %q", i, result.StepResults[i].StepName, name)
		}
	}

	// Verify final output correctness
	lastStep := result.StepResults[3]
	if lastStep.Error != nil {
		t.Errorf("risk_score step failed: %v", lastStep.Error)
	}

	// Verify no unexpected tool usage
	if tr.callCount() != 4 {
		t.Errorf("tool call count = %d, want 4 (no extra calls)", tr.callCount())
	}

	// Verify goal achieved
	if result.StopCondition.Reason != StopReasonGoalAchieved {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonGoalAchieved)
	}
}

// E2E Test 2a: Missing data — vitals not found for patient
func TestE2E_MissingVitalsData(t *testing.T) {
	cfg := DefaultHarnessConfig()

	tr := newMockToolRunner()
	tr.result["ehr.query_patient"] = map[string]any{"id": "P999", "name": "Unknown"}
	tr.err["ehr.query_vitals"] = fmt.Errorf("vitals not found for patient: P999")

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "ehr.query_patient", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P999"}},
		execution.ExecutionStep{Name: "ehr.query_vitals", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P999"}},
		execution.ExecutionStep{Name: "analytics.risk_score", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P999"}},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	// Harness should not crash — error is captured per-step
	if err != nil {
		t.Fatalf("unexpected error at harness level: %v", err)
	}

	// Step 1 succeeds, step 2 fails, step 3 still attempted (no fail-fast)
	if result.StepsExecuted != 3 {
		t.Errorf("StepsExecuted = %d, want 3", result.StepsExecuted)
	}

	// Step 2 should have error recorded
	if result.StepResults[1].Error == nil {
		t.Error("expected error for missing vitals step")
	}

	// Verify the error message is captured
	if result.StepResults[1].Error.Error() != "vitals not found for patient: P999" {
		t.Errorf("step 2 error = %v, want vitals not found error", result.StepResults[1].Error)
	}
}

// E2E Test 2b: Missing data — labs return empty/nil result
func TestE2E_MissingLabData(t *testing.T) {
	cfg := DefaultHarnessConfig()

	tr := newMockToolRunner()
	tr.result["ehr.query_patient"] = map[string]any{"id": "P002", "name": "Li Na"}
	tr.result["ehr.query_vitals"] = map[string]any{"patient_id": "P002", "heart_rate": 78}
	// Labs return nil (no data available)
	tr.result["ehr.query_labs"] = nil
	tr.result["analytics.risk_score"] = map[string]any{"patient_id": "P002", "score": 0, "level": "low"}

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "ehr.query_patient", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P002"}},
		execution.ExecutionStep{Name: "ehr.query_vitals", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P002"}},
		execution.ExecutionStep{Name: "ehr.query_labs", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P002"}},
		execution.ExecutionStep{Name: "analytics.risk_score", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P002"}},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All 4 steps should execute (graceful degradation, not crash)
	if result.StepsExecuted != 4 {
		t.Errorf("StepsExecuted = %d, want 4", result.StepsExecuted)
	}
}

// E2E Test 3: Tool failure via timeout
func TestE2E_ToolTimeout(t *testing.T) {
	cfg := DefaultHarnessConfig()
	cfg.MaxSteps = 5

	// Use blocking tool runner that blocks until context cancelled
	// combined with a fast tool that succeeds
	tr := &timeoutTestToolRunner{blockTool: "slow-query", fastResult: "ok"}
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "fast-query", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "slow-query", Type: execution.StepTypeTool, Arguments: map[string]any{}, Timeout: 50}, // 50ms timeout
		execution.ExecutionStep{Name: "after-timeout", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Step 1 succeeds, step 2 times out, step 3 should still be attempted
	if result.StepsExecuted != 3 {
		t.Errorf("StepsExecuted = %d, want 3", result.StepsExecuted)
	}

	// Step 2 should have timeout error
	if result.StepResults[1].Error == nil {
		t.Error("expected timeout error for slow-query step")
	}

	// Partial results preserved
	if result.StepResults[0].Output == nil {
		t.Error("expected output from first step (before timeout)")
	}
}

// timeoutTestToolRunner blocks on a specific tool and returns fast for others.
type timeoutTestToolRunner struct {
	blockTool  string
	fastResult string
	calls      int
}

func (t *timeoutTestToolRunner) Execute(ctx context.Context, toolName string, args map[string]any, ec *execution.ExecutionContext) (*execution.StepResult, error) {
	t.calls++
	if toolName == t.blockTool {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return &execution.StepResult{StepName: toolName, Output: t.fastResult}, nil
}
func (t *timeoutTestToolRunner) callCount() int { return t.calls }

// E2E Test 4: Multi-step shift_handover workflow
func TestE2E_ShiftHandoverWorkflow(t *testing.T) {
	cfg := DefaultHarnessConfig()
	cfg.MaxSteps = 10

	tr := newMockToolRunner()
	// Simulated shift handover data
	tr.result["ehr.query_patient"] = []any{
		map[string]any{"id": "P001", "name": "Zhang Wei", "unit": "ICU"},
		map[string]any{"id": "P002", "name": "Li Na", "unit": "ICU"},
	}
	tr.result["ehr.query_vitals"] = map[string]any{
		"patient_id": "P001", "heart_rate": 110, "systolic_bp": 85,
	}
	tr.result["ehr.query_labs"] = map[string]any{
		"patient_id": "P001", "abnormal_count": 3,
	}
	tr.result["analytics.risk_score"] = map[string]any{
		"patient_id": "P001", "score": 80, "level": "critical",
	}

	skillExec := &mockSkillExecutor{resp: []byte(`{"sbar":"Situation: Sepsis patient P001 with elevated vitals..."}`)}

	h := NewExecutionHarness(cfg, tr, skillExec, HarnessDeps{})

	// Plan mirrors ShiftHandoverWorkflow.Steps()
	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "ehr.query_patient", Type: execution.StepTypeTool, Arguments: map[string]any{"unit": "ICU"}},
		execution.ExecutionStep{Name: "ehr.query_vitals", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
		execution.ExecutionStep{Name: "ehr.query_labs", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
		execution.ExecutionStep{Name: "analytics.risk_score", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
		execution.ExecutionStep{Name: "nursing/generate_sbar", Type: execution.StepTypeSkill, Arguments: map[string]any{"patient_id": "P001"}},
	)
	ec := testEC()

	result, err := h.Run(context.Background(), plan, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify all 5 steps executed in sequence
	if result.StepsExecuted != 5 {
		t.Fatalf("StepsExecuted = %d, want 5", result.StepsExecuted)
	}

	// Verify step sequence matches shift handover workflow
	expectedSeq := []string{"ehr.query_patient", "ehr.query_vitals", "ehr.query_labs", "analytics.risk_score", "nursing/generate_sbar"}
	for i, name := range expectedSeq {
		if result.StepResults[i].StepName != name {
			t.Errorf("step[%d] = %q, want %q", i, result.StepResults[i].StepName, name)
		}
	}

	// Verify metrics: 4 tool calls (not skill), 0 LLM turns
	if result.Metrics.TotalSteps != 5 {
		t.Errorf("TotalSteps = %d, want 5", result.Metrics.TotalSteps)
	}
	if result.Metrics.TotalLLMTurns != 0 {
		t.Errorf("TotalLLMTurns = %d, want 0 (no LLM steps)", result.Metrics.TotalLLMTurns)
	}

	// Verify final skill output
	lastStep := result.StepResults[4]
	if lastStep.Output == nil {
		t.Error("expected SBAR output from generate_sbar skill")
	}

	// Verify no unexpected tool usage
	if tr.callCount() != 4 {
		t.Errorf("tool calls = %d, want 4 (exactly the tool steps)", tr.callCount())
	}
	if skillExec.callCount() != 1 {
		t.Errorf("skill calls = %d, want 1", skillExec.callCount())
	}

	// Verify execution completed successfully
	if result.StopCondition.Reason != StopReasonGoalAchieved {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonGoalAchieved)
	}

	// Verify reasonable execution time
	if result.Duration > 5*time.Second {
		t.Errorf("Duration = %v, should be < 5s for mock tools", result.Duration)
	}
}
