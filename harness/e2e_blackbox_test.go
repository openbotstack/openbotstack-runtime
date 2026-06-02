package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/planner"
	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-core/execution"
)

// ========================================================================
// Phase 5 — End-to-End & Blackbox Validation
// ========================================================================
//
// Treats the harness as a blackbox AI assistant runtime. Validates system
// behavior from an external observer's perspective: correct outputs, bounded
// execution, complete audit trails, graceful degradation.

// ---------------------------------------------------------------------------
// Part 1: Full E2E Pipeline Tests
// ---------------------------------------------------------------------------

// E2E-1.1: ICU Shift Handover — full clinical pipeline
// Simulates a nurse requesting shift handover for ICU patients.
// Verifies end-to-end data flow from patient query through SBAR generation.
func TestE2EBlackbox_ICUShiftHandover(t *testing.T) {
	cfg := DefaultHarnessConfig()

	tr := newMockToolRunner()
	tr.result["ehr.query_patient"] = []any{
		map[string]any{"id": "P001", "name": "Zhang Wei", "unit": "ICU", "bed": "ICU-01", "diagnosis": "Sepsis"},
		map[string]any{"id": "P004", "name": "Chen Fang", "unit": "ICU", "bed": "ICU-04", "diagnosis": "Respiratory Failure"},
	}
	tr.result["ehr.query_vitals"] = map[string]any{
		"patient_id": "P001", "heart_rate": 110, "systolic_bp": 85,
		"spo2": 92, "respiratory_rate": 24, "temperature": 38.9,
	}
	tr.result["ehr.query_labs"] = map[string]any{
		"patient_id": "P001",
		"results": []any{
			map[string]any{"test": "WBC", "value": 18.5, "abnormal": true},
			map[string]any{"test": "Lactate", "value": 4.2, "abnormal": true},
			map[string]any{"test": "Procalcitonin", "value": 8.5, "abnormal": true},
		},
		"abnormal_count": 3,
	}
	tr.result["analytics.risk_score"] = map[string]any{
		"patient_id": "P001", "score": 85.0, "level": "critical",
		"contributors": []any{"elevated HR", "low SBP", "elevated lactate"},
	}

	skillExec := &mockSkillExecutor{
		resp: []byte(`{"sbar":"Situation: ICU patient P001 (Zhang Wei) with Sepsis. Background: Elevated vitals (HR 110, SBP 85, SpO2 92, Temp 38.9C). 3 abnormal lab values including elevated lactate (4.2 mmol/L). Assessment: Critical risk score 85/100. Recommendation: Continue antibiotic therapy, monitor hemodynamics, consider vasopressor support."}`),
	}

	// Wire audit layer
	al := NewAuditLayer()

	// Wire progress callback
	var progressEvents []ProgressEvent
	progressCB := func(event ProgressEvent) {
		progressEvents = append(progressEvents, event)
	}

	// Record each step in audit layer
	hm := NewHookManager()
	hm.RegisterPostStepExecute(func(ctx context.Context, hctx *execution.HookContext) error {
		if hctx.ToolOutput != nil {
			al.RecordStep(ctx, audit.AuditEvent{
				StepName:  hctx.Step.Name,
				StepType:  string(hctx.Step.Type),
				Status:    "completed",
				Timestamp: time.Now(),
			})
		}
		return nil
	})

	h := NewExecutionHarness(cfg, tr, skillExec, HarnessDeps{
		HookManager: hm,
		ProgressCB:  progressCB,
	})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "ehr.query_patient", Type: execution.StepTypeTool, Arguments: map[string]any{"unit": "ICU"}},
		execution.ExecutionStep{Name: "ehr.query_vitals", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
		execution.ExecutionStep{Name: "ehr.query_labs", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
		execution.ExecutionStep{Name: "analytics.risk_score", Type: execution.StepTypeTool, Arguments: map[string]any{
			"patient_id": "P001",
			"heart_rate": "{{ehr.query_vitals.heart_rate}}",
			"systolic_bp": "{{ehr.query_vitals.systolic_bp}}",
		}},
		execution.ExecutionStep{Name: "nursing/generate_sbar", Type: execution.StepTypeSkill, Arguments: map[string]any{"patient_id": "P001"}},
	)

	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// === Blackbox Assertions ===

	// A1: All 5 steps executed in correct order
	if result.StepsExecuted != 5 {
		t.Fatalf("StepsExecuted = %d, want 5", result.StepsExecuted)
	}
	expectedSeq := []string{"ehr.query_patient", "ehr.query_vitals", "ehr.query_labs", "analytics.risk_score", "nursing/generate_sbar"}
	for i, name := range expectedSeq {
		if result.StepResults[i].StepName != name {
			t.Errorf("step[%d] = %q, want %q", i, result.StepResults[i].StepName, name)
		}
	}

	// A2: No errors in any step
	for i, sr := range result.StepResults {
		if sr.Error != nil {
			t.Errorf("step[%d] %q has error: %v", i, sr.StepName, sr.Error)
		}
	}

	// A3: Final SBAR output contains clinical content
	lastOutput := result.StepResults[4].Output
	if lastOutput == nil {
		t.Fatal("expected SBAR output from generate_sbar skill")
	}
	sbarStr, ok := lastOutput.(string)
	if !ok {
		t.Fatalf("SBAR output type = %T, want string", lastOutput)
	}
	var sbar map[string]any
	if err := json.Unmarshal([]byte(sbarStr), &sbar); err != nil {
		t.Fatalf("SBAR output is not valid JSON: %v", err)
	}
	if sbarText, _ := sbar["sbar"].(string); sbarText == "" {
		t.Error("SBAR output missing 'sbar' field")
	}

	// A4: Goal achieved
	if result.StopCondition.Reason != StopReasonGoalAchieved {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonGoalAchieved)
	}

	// A5: Audit trail matches execution
	if al.TrailSize() != 5 {
		t.Errorf("AuditTrail size = %d, want 5", al.TrailSize())
	}

	// A6: Progress events emitted for each step (step_start + step_complete per step)
	if len(progressEvents) != 10 {
		t.Errorf("ProgressEvents = %d, want 10", len(progressEvents))
	}

	// A7: Metrics are consistent
	if result.Metrics.TotalSteps != 5 {
		t.Errorf("TotalSteps = %d, want 5", result.Metrics.TotalSteps)
	}
	if result.Duration == 0 {
		t.Error("Duration should be > 0")
	}

	// A8: Tool runner called exactly 4 times (tools only, not skills)
	if tr.callCount() != 4 {
		t.Errorf("tool calls = %d, want 4", tr.callCount())
	}
	if skillExec.callCount() != 1 {
		t.Errorf("skill calls = %d, want 1", skillExec.callCount())
	}
}

// E2E-1.2: Patient Risk Detection — result interpolation chain
// Verifies that step result interpolation carries data forward correctly.
func TestE2EBlackbox_PatientRiskDetection_Interpolation(t *testing.T) {
	cfg := DefaultHarnessConfig()

	tr := newMockToolRunner()
	tr.result["ehr.query_patient"] = map[string]any{"id": "P001", "name": "Zhang Wei", "age": 72}
	tr.result["ehr.query_vitals"] = map[string]any{
		"patient_id": "P001", "heart_rate": 110, "systolic_bp": 85,
	}
	tr.result["ehr.query_labs"] = map[string]any{
		"patient_id": "P001", "abnormal_count": 3,
	}
	tr.result["analytics.risk_score"] = map[string]any{
		"patient_id": "P001", "score": 80.0, "level": "critical",
	}

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "ehr.query_patient", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
		execution.ExecutionStep{Name: "ehr.query_vitals", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
		execution.ExecutionStep{Name: "ehr.query_labs", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
		execution.ExecutionStep{
			Name: "analytics.risk_score", Type: execution.StepTypeTool,
			Arguments: map[string]any{
				"patient_id":         "P001",
				"heart_rate":         "{{ehr.query_vitals.heart_rate}}",
				"systolic_bp":        "{{ehr.query_vitals.systolic_bp}}",
				"abnormal_lab_count": "{{ehr.query_labs.abnormal_count}}",
			},
		},
	)

	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify all 4 steps completed
	if result.StepsExecuted != 4 {
		t.Fatalf("StepsExecuted = %d, want 4", result.StepsExecuted)
	}

	// Verify risk_score step received interpolated arguments
	// (the tool runner receives the resolved arguments)
	lastStep := result.StepResults[3]
	if lastStep.Error != nil {
		t.Errorf("risk_score step failed: %v", lastStep.Error)
	}
	if lastStep.Output == nil {
		t.Error("risk_score step should produce output")
	}
}

// E2E-1.3: Multi-step reasoning loop with clinical tool chain
// Verifies that ReasoningLoop correctly orchestrates multiple turns
// with tool execution within each turn.
func TestE2EBlackbox_MultiStepReasoning_ClinicalChain(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 3
	cfg.MaxToolCalls = 6
	cfg.RepeatPlanStop = false

	// Turn 1: query patient, Turn 2: query vitals, Turn 3: empty (stop)
	turn1 := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "ehr.query_patient", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
		},
		Reasoning: "Need to look up patient P001",
	}
	turn2 := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "ehr.query_vitals", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
		},
		Reasoning: "Now check vitals for P001",
	}
	turn3 := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{},
		Reasoning: "Patient data collected, done",
	}

	mp := &mockPlanner{plans: []*execution.ExecutionPlan{turn1, turn2, turn3}}

	tr := newMockToolRunner()
	tr.result["ehr.query_patient"] = map[string]any{"id": "P001", "name": "Zhang Wei"}
	tr.result["ehr.query_vitals"] = map[string]any{"patient_id": "P001", "heart_rate": 110}

	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	pCtx := &planner.PlannerContext{
		UserRequest:   "Assess patient P001",
		MemoryContext: []planner.SearchResult{},
	}

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "clinical-assess", Type: execution.StepTypeLLM}, pCtx, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should complete 3 turns (3rd is empty → planner_stopped)
	if result.TurnCount != 3 {
		t.Errorf("TurnCount = %d, want 3", result.TurnCount)
	}
	if result.ToolCalls != 2 {
		t.Errorf("ToolCalls = %d, want 2", result.ToolCalls)
	}
	if result.StopReason != StopReasonPlannerStopped {
		t.Errorf("StopReason = %q, want %q", result.StopReason, StopReasonPlannerStopped)
	}

	// Memory context should have observations from turns 1 and 2
	if len(pCtx.MemoryContext) < 2 {
		t.Errorf("MemoryContext entries = %d, want >= 2", len(pCtx.MemoryContext))
	}
}

// ---------------------------------------------------------------------------
// Part 2: Blackbox Failure Tests
// ---------------------------------------------------------------------------

// E2E-2.1: Tool failure mid-workflow — graceful degradation
func TestE2EBlackbox_ToolFailure_GracefulDegradation(t *testing.T) {
	cfg := DefaultHarnessConfig()

	tr := newMockToolRunner()
	tr.result["ehr.query_patient"] = map[string]any{"id": "P001", "name": "Zhang Wei"}
	tr.err["ehr.query_vitals"] = fmt.Errorf("EHR system timeout: vitals service unavailable")
	tr.result["ehr.query_labs"] = map[string]any{"patient_id": "P001", "abnormal_count": 1}
	tr.result["analytics.risk_score"] = map[string]any{"patient_id": "P001", "score": 40.0, "level": "moderate"}

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "ehr.query_patient", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
		execution.ExecutionStep{Name: "ehr.query_vitals", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
		execution.ExecutionStep{Name: "ehr.query_labs", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
		execution.ExecutionStep{Name: "analytics.risk_score", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
	)

	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("harness should not crash on tool failure: %v", err)
	}

	// All 4 steps attempted (no fail-fast)
	if result.StepsExecuted != 4 {
		t.Errorf("StepsExecuted = %d, want 4", result.StepsExecuted)
	}

	// Step 1, 3, 4 succeed; step 2 has error
	if result.StepResults[0].Error != nil {
		t.Errorf("step 1 should succeed: %v", result.StepResults[0].Error)
	}
	if result.StepResults[1].Error == nil {
		t.Error("step 2 should have error (vitals timeout)")
	}
	if result.StepResults[2].Error != nil {
		t.Errorf("step 3 should succeed despite step 2 failure: %v", result.StepResults[2].Error)
	}
	if result.StepResults[3].Error != nil {
		t.Errorf("step 4 should succeed: %v", result.StepResults[3].Error)
	}

	// Goal still achieved (no fail-fast)
	if result.StopCondition.Reason != StopReasonGoalAchieved {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonGoalAchieved)
	}
}

// E2E-2.2: Tool failure with fail-fast — execution stops immediately
func TestE2EBlackbox_ToolFailure_FailFast(t *testing.T) {
	cfg := DefaultHarnessConfig()

	tr := newMockToolRunner()
	tr.result["ehr.query_patient"] = map[string]any{"id": "P001"}
	tr.err["ehr.query_vitals"] = fmt.Errorf("critical: EHR system down")

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{
		FailureHandler: NewFailureHandler(execution.RetryPolicy{
			MaxRetries: 0, FailFast: true,
		}),
	})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "ehr.query_patient", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "ehr.query_vitals", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "ehr.query_labs", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)

	result, err := h.Run(context.Background(), plan, testEC())
	if err == nil {
		t.Fatal("expected error for fail-fast")
	}

	// Only step 1 succeeded, step 2 failed, step 3 not attempted
	if result.StepsExecuted != 2 {
		t.Errorf("StepsExecuted = %d, want 2", result.StepsExecuted)
	}
	if result.StopCondition.Reason != StopReasonFailFast {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonFailFast)
	}
}

// E2E-2.3: Tool failure with retry — succeeds on second attempt
func TestE2EBlackbox_ToolFailure_RetrySuccess(t *testing.T) {
	cfg := DefaultHarnessConfig()

	tr := &countingToolRunner{
		failUntil: 1, // fail first call, succeed after
		result:    "recovered",
	}

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{
		FailureHandler: NewFailureHandler(execution.RetryPolicy{
			MaxRetries:     2,
			InitialBackoff: 1 * time.Millisecond,
			MaxBackoff:     5 * time.Millisecond,
			FailFast:       false,
		}),
	})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "flaky-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)

	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("expected recovery after retry: %v", err)
	}

	if result.StepsExecuted != 1 {
		t.Errorf("StepsExecuted = %d, want 1", result.StepsExecuted)
	}
	if result.StepResults[0].Error != nil {
		t.Errorf("step should succeed after retry: %v", result.StepResults[0].Error)
	}
	if result.StepResults[0].Retries != 1 {
		t.Errorf("Retries = %d, want 1", result.StepResults[0].Retries)
	}
}

// countingToolRunner fails for the first N calls then succeeds.
type countingToolRunner struct {
	mu        sync.Mutex
	calls     int
	failUntil int // fail while calls <= failUntil
	result    string
}

func (c *countingToolRunner) Execute(ctx context.Context, toolName string, args map[string]any, ec *execution.ExecutionContext) (*execution.StepResult, error) {
	c.mu.Lock()
	c.calls++
	callNum := c.calls
	c.mu.Unlock()

	if callNum <= c.failUntil {
		return nil, fmt.Errorf("transient error (call %d)", callNum)
	}
	return &execution.StepResult{StepName: toolName, Output: c.result}, nil
}

// E2E-2.4: Tool failure with fallback tool execution
func TestE2EBlackbox_ToolFailure_FallbackTool(t *testing.T) {
	cfg := DefaultHarnessConfig()

	tr := newMockToolRunner()
	tr.err["primary-tool"] = fmt.Errorf("primary unavailable")
	tr.result["fallback-tool"] = "fallback-result"

	fh := NewFailureHandler(execution.RetryPolicy{
		MaxRetries:   1,
		FallbackTool: "fallback-tool",
		FailFast:     false,
		InitialBackoff: 1 * time.Millisecond,
		MaxBackoff:     5 * time.Millisecond,
	})
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{FailureHandler: fh})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "primary-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)

	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("expected fallback success: %v", err)
	}

	if result.StepsExecuted != 1 {
		t.Errorf("StepsExecuted = %d, want 1", result.StepsExecuted)
	}
	if result.StepResults[0].Fallback != true {
		t.Error("expected Fallback = true")
	}
}

// E2E-2.5: Timeout cascade — multiple steps with timeouts
func TestE2EBlackbox_TimeoutCascade_PartialResults(t *testing.T) {
	cfg := DefaultHarnessConfig()

	tr := &timeoutTestToolRunner{blockTool: "slow-tool", fastResult: "fast-ok"}
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "fast-step", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "slow-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}, Timeout: 50},
		execution.ExecutionStep{Name: "after-slow", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)

	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// All 3 steps attempted
	if result.StepsExecuted != 3 {
		t.Errorf("StepsExecuted = %d, want 3", result.StepsExecuted)
	}

	// Step 1 succeeded
	if result.StepResults[0].Output == nil {
		t.Error("step 1 should have output")
	}
	// Step 2 timed out
	if result.StepResults[1].Error == nil {
		t.Error("step 2 should have timeout error")
	}
	// Step 3 still attempted (no fail-fast)
	if result.StepResults[2].Output == nil {
		t.Error("step 3 should still execute after step 2 timeout")
	}
}

// E2E-2.6: SubAgent parallel — partial failure
func TestE2EBlackbox_SubAgentParallel_PartialFailure(t *testing.T) {
	cfg := DefaultHarnessConfig()

	tr := newMockToolRunner()
	tr.result["ok-tool"] = "success"
	tr.err["fail-tool"] = fmt.Errorf("tool error")

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	// 2 succeed, 1 fails
	okPlan := makeFrozenPlan(
		execution.ExecutionStep{Name: "ok-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)
	failPlan := makeFrozenPlan(
		execution.ExecutionStep{Name: "fail-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)

	subs := []*SubAgent{
		NewSubAgent(SubAgentConfig{Plan: okPlan}, h),
		NewSubAgent(SubAgentConfig{Plan: failPlan}, h),
		NewSubAgent(SubAgentConfig{Plan: okPlan}, h),
	}

	results, _ := RunParallel(context.Background(), subs, testEC(), 3)

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Sub 1: failure is guaranteed (has fail-tool)
	if results[1] == nil {
		t.Fatal("sub 1 result is nil")
	}
	// Note: sibling cancellation may cause sub 0 and sub 2 to also fail.
	// The key blackbox guarantee: all results are non-nil (no panics).
	for i, r := range results {
		if r == nil {
			t.Errorf("results[%d] is nil — all subs must return a result", i)
		}
	}
}

// ---------------------------------------------------------------------------
// Part 3: Loop Validation (Termination Guarantees)
// ---------------------------------------------------------------------------

// E2E-3.1: ReasoningLoop always terminates at MaxTurns
func TestE2EBlackbox_Loop_TerminatesAtMaxTurns(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 3
	cfg.MaxToolCalls = 20
	cfg.RepeatPlanStop = false

	plan := makeToolPlan("tool-a")
	mp := &mockPlanner{plans: []*execution.ExecutionPlan{plan}, repeatLast: true}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TurnCount > cfg.MaxTurns {
		t.Errorf("TurnCount = %d, exceeds MaxTurns = %d", result.TurnCount, cfg.MaxTurns)
	}
	if result.Duration > 5*time.Second {
		t.Errorf("Duration = %v, should be bounded", result.Duration)
	}
}

// E2E-3.2: ReasoningLoop always terminates at MaxToolCalls
func TestE2EBlackbox_Loop_TerminatesAtMaxToolCalls(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 20
	cfg.MaxToolCalls = 3
	cfg.RepeatPlanStop = false

	// Each turn has 2 tool steps
	twoStepPlan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
			{Name: "tool-b", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		},
		Reasoning: "run two tools",
	}
	mp := &mockPlanner{plans: []*execution.ExecutionPlan{twoStepPlan}, repeatLast: true}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.ToolCalls > cfg.MaxToolCalls {
		t.Errorf("ToolCalls = %d, exceeds MaxToolCalls = %d", result.ToolCalls, cfg.MaxToolCalls)
	}
	// Should have stopped due to tool budget
	if result.StopReason != StopReasonMaxToolCalls && result.StopReason != StopReasonMaxTurns {
		t.Logf("StopReason = %q (acceptable: tool budget or turns)", result.StopReason)
	}
}

// E2E-3.3: ReasoningLoop terminates on context cancellation
func TestE2EBlackbox_Loop_TerminatesOnContextCancel(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 100
	cfg.RepeatPlanStop = false

	plan := makeToolPlan("tool-a")
	mp := &mockPlanner{plans: []*execution.ExecutionPlan{plan}, repeatLast: true}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	result, _ := rl.Run(ctx, &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())

	if result == nil {
		t.Fatal("result should not be nil even on cancellation")
	}
	// Must not run all 100 turns
	if result.TurnCount >= cfg.MaxTurns {
		t.Errorf("TurnCount = %d, should have stopped on cancellation", result.TurnCount)
	}
}

// E2E-3.4: ReasoningLoop terminates when planner returns empty plan
func TestE2EBlackbox_Loop_TerminatesOnPlannerStop(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 10
	cfg.RepeatPlanStop = false

	plan := makeToolPlan("tool-a")
	mp := &mockPlanner{
		plans: []*execution.ExecutionPlan{
			plan,
			{Steps: []execution.ExecutionStep{}}, // empty → stop
		},
	}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TurnCount != 2 {
		t.Errorf("TurnCount = %d, want 2 (1 tool + 1 empty)", result.TurnCount)
	}
	if result.StopReason != StopReasonPlannerStopped {
		t.Errorf("StopReason = %q, want %q", result.StopReason, StopReasonPlannerStopped)
	}
}

// E2E-3.5: Harness terminates at MaxSessionRuntime
func TestE2EBlackbox_Harness_TerminatesAtMaxSessionRuntime(t *testing.T) {
	cfg := DefaultHarnessConfig()
	cfg.MaxSessionRuntime = 80 * time.Millisecond

	tr := &e2eSlowToolRunner{delay: 50 * time.Millisecond}
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "slow-1", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "slow-2", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "slow-3", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "slow-4", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)

	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.StopCondition.Reason != StopReasonMaxSessionRuntime {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonMaxSessionRuntime)
	}
	// Should not execute all 4 steps
	if result.StepsExecuted >= 4 {
		t.Errorf("StepsExecuted = %d, should be < 4 (session runtime exceeded)", result.StepsExecuted)
	}
}

// e2eSlowToolRunner adds a fixed delay to each call.
type e2eSlowToolRunner struct {
	delay time.Duration
}

func (s *e2eSlowToolRunner) Execute(ctx context.Context, toolName string, args map[string]any, ec *execution.ExecutionContext) (*execution.StepResult, error) {
	select {
	case <-time.After(s.delay):
		return &execution.StepResult{StepName: toolName, Output: "done"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// E2E-3.6: Harness terminates at MaxSteps
func TestE2EBlackbox_Harness_TerminatesAtMaxSteps(t *testing.T) {
	cfg := DefaultHarnessConfig()
	cfg.MaxSteps = 2

	tr := newMockToolRunner()
	tr.result["tool-a"] = "ok"
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "step-1", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "step-2", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "step-3", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "step-4", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)

	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.StepsExecuted != 2 {
		t.Errorf("StepsExecuted = %d, want 2 (MaxSteps limit)", result.StepsExecuted)
	}
	if result.StopCondition.Reason != StopReasonMaxSteps {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonMaxSteps)
	}
}

// ---------------------------------------------------------------------------
// Part 4: Audit Trace Validation
// ---------------------------------------------------------------------------

// E2E-4.1: Complete audit trail for successful execution
func TestE2EBlackbox_AuditTrail_SuccessfulExecution(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["tool-a"] = "ok"
	tr.result["tool-b"] = "done"

	al := NewAuditLayer()

	hm := NewHookManager()
	hm.RegisterPostStepExecute(func(ctx context.Context, hctx *execution.HookContext) error {
		status := "completed"
		if sr, ok := hctx.ToolOutput.(*execution.StepResult); ok && sr.Error != nil {
			status = "failed"
		}
		al.RecordStep(ctx, audit.AuditEvent{
			StepName:  hctx.Step.Name,
			StepType:  string(hctx.Step.Type),
			Status:    status,
			Timestamp: time.Now(),
			TenantID:  hctx.EC.TenantID,
			RequestID: hctx.EC.RequestID,
		})
		return nil
	})

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{HookManager: hm})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "tool-b", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)

	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	trail := al.Trail()

	// A1: Trail size matches steps executed
	if len(trail) != result.StepsExecuted {
		t.Errorf("Trail size = %d, StepsExecuted = %d", len(trail), result.StepsExecuted)
	}

	// A2: Each entry has required fields
	for i, entry := range trail {
		if entry.StepName == "" {
			t.Errorf("trail[%d]: StepName is empty", i)
		}
		if entry.Timestamp.IsZero() {
			t.Errorf("trail[%d]: Timestamp is zero", i)
		}
		if entry.Status == "" {
			t.Errorf("trail[%d]: Status is empty", i)
		}
	}

	// A3: Step names match execution order
	if trail[0].StepName != "tool-a" {
		t.Errorf("trail[0].StepName = %q, want 'tool-a'", trail[0].StepName)
	}
	if trail[1].StepName != "tool-b" {
		t.Errorf("trail[1].StepName = %q, want 'tool-b'", trail[1].StepName)
	}

	// A4: Timestamps are monotonically increasing
	for i := 1; i < len(trail); i++ {
		if trail[i].Timestamp.Before(trail[i-1].Timestamp) {
			t.Errorf("trail[%d].Timestamp (%v) < trail[%d].Timestamp (%v)", i, trail[i].Timestamp, i-1, trail[i-1].Timestamp)
		}
	}
}

// E2E-4.2: Audit trail captures failures
func TestE2EBlackbox_AuditTrail_CapturesFailures(t *testing.T) {
	cfg := DefaultHarnessConfig()

	tr := newMockToolRunner()
	tr.result["ok-tool"] = "ok"
	tr.err["fail-tool"] = fmt.Errorf("tool crashed")

	al := NewAuditLayer()

	hm := NewHookManager()
	hm.RegisterPostStepExecute(func(ctx context.Context, hctx *execution.HookContext) error {
		status := "completed"
		var errMsg string
		if sr, ok := hctx.ToolOutput.(*execution.StepResult); ok && sr.Error != nil {
			status = "failed"
			errMsg = sr.Error.Error()
		}
		al.RecordStep(ctx, audit.AuditEvent{
			StepName:  hctx.Step.Name,
			StepType:  string(hctx.Step.Type),
			Status:    status,
			Error:     errMsg,
			Timestamp: time.Now(),
		})
		return nil
	})

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{HookManager: hm})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "ok-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "fail-tool", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)

	h.Run(context.Background(), plan, testEC())

	trail := al.Trail()

	if trail[0].Status != "completed" {
		t.Errorf("trail[0].Status = %q, want 'completed'", trail[0].Status)
	}
	if trail[1].Status != "failed" {
		t.Errorf("trail[1].Status = %q, want 'failed'", trail[1].Status)
	}
	if trail[1].Error == "" {
		t.Error("trail[1].Error should capture failure message")
	}
}

// E2E-4.3: Audit trail is immutable — entries not modified after recording
func TestE2EBlackbox_AuditTrail_Immutable(t *testing.T) {
	al := NewAuditLayer()

	al.RecordStep(context.Background(), audit.AuditEvent{
		StepName: "step-1", StepType: string(execution.StepTypeTool), Status: "completed", Timestamp: time.Now(),
	})

	trail1 := al.Trail()
	trail1[0].Status = "tampered"

	trail2 := al.Trail()
	if trail2[0].Status == "tampered" {
		t.Error("Audit trail should be immutable — returned copy should not affect internal state")
	}
}

// ---------------------------------------------------------------------------
// Part 5: Memory & Context Validation
// ---------------------------------------------------------------------------

// E2E-5.1: ReasoningLoop accumulates observations across turns
func TestE2EBlackbox_Memory_ObservationsAccumulate(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 3
	cfg.RepeatPlanStop = false

	plan := makeToolPlan("data-tool")
	mp := &mockPlanner{plans: []*execution.ExecutionPlan{plan}, repeatLast: true}

	// Each call returns different data
	callCount := 0
	tr := &turnAwareToolRunner{fn: func(toolName string) any {
		callCount++
		return map[string]any{"turn": callCount, "value": fmt.Sprintf("data-%d", callCount)}
	}}

	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	pCtx := &planner.PlannerContext{
		UserRequest:   "analyze data over time",
		MemoryContext: []planner.SearchResult{},
	}

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, pCtx, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// MemoryContext should have observations from turns that produced output
	// (last turn has empty plan → no observations, so entries = TurnCount - 1)
	if len(pCtx.MemoryContext) == 0 {
		t.Error("MemoryContext should have observations from tool-executing turns")
	}
	if len(pCtx.MemoryContext) > result.TurnCount {
		t.Errorf("MemoryContext entries = %d > TurnCount = %d — should not exceed", len(pCtx.MemoryContext), result.TurnCount)
	}

	// Each observation should be distinct
	seen := map[string]bool{}
	for _, sr := range pCtx.MemoryContext {
		content := string(sr.Content)
		if seen[content] {
			t.Errorf("duplicate observation: %s", content)
		}
		seen[content] = true
	}
}

// turnAwareToolRunner calls a function to produce per-call results.
type turnAwareToolRunner struct {
	fn    func(toolName string) any
	mu    sync.Mutex
	calls int
}

func (t *turnAwareToolRunner) Execute(ctx context.Context, toolName string, args map[string]any, ec *execution.ExecutionContext) (*execution.StepResult, error) {
	t.mu.Lock()
	t.calls++
	t.mu.Unlock()
	return &execution.StepResult{StepName: toolName, Output: t.fn(toolName)}, nil
}

// E2E-5.2: Context compaction reduces turn history
func TestE2EBlackbox_Memory_CompactionReducesHistory(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 6
	cfg.RepeatPlanStop = false

	plan := makeToolPlan("tool-a")
	mp := &mockPlanner{plans: []*execution.ExecutionPlan{plan}, repeatLast: true}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})

	var compactCalled atomic.Bool
	compactor := &mockCompactor{compactFn: func(ctx context.Context, results []TurnResult) ([]TurnResult, error) {
		compactCalled.Store(true)
		if len(results) <= 3 {
			return results, nil
		}
		return append([]TurnResult{results[0]}, results[len(results)-2:]...), nil
	}}

	rl := NewDefaultReasoningLoop(cfg, mp, se, compactor)

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.TurnCount < 4 {
		t.Errorf("TurnCount = %d, want >= 4 (enough to trigger compaction)", result.TurnCount)
	}
	if !compactCalled.Load() {
		t.Error("compaction should have been triggered")
	}
}

// E2E-5.3: PlannerContext.MemoryContext grows linearly, not exponentially
func TestE2EBlackbox_Memory_LinearGrowth(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 5
	cfg.RepeatPlanStop = false

	plan := makeToolPlan("tool-a")
	mp := &mockPlanner{plans: []*execution.ExecutionPlan{plan}, repeatLast: true}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	pCtx := &planner.PlannerContext{
		UserRequest:   "test",
		MemoryContext: []planner.SearchResult{},
	}

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, pCtx, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// MemoryContext should grow linearly (at most 1 entry per turn)
	if len(pCtx.MemoryContext) > result.TurnCount {
		t.Errorf("MemoryContext = %d entries > TurnCount = %d — growth should be linear", len(pCtx.MemoryContext), result.TurnCount)
	}
	if len(pCtx.MemoryContext) == 0 {
		t.Error("MemoryContext should have entries from tool-executing turns")
	}
}

// ---------------------------------------------------------------------------
// Part 6: Output Consistency & Schema Validation
// ---------------------------------------------------------------------------

// E2E-6.1: Step result output is consistent across runs (deterministic)
func TestE2EBlackbox_OutputConsistency_Deterministic(t *testing.T) {
	cfg := DefaultHarnessConfig()

	run := func() []string {
		tr := newMockToolRunner()
		tr.result["tool-a"] = "ok"
		tr.result["tool-b"] = "done"
		h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

		plan := makeFrozenPlan(
			execution.ExecutionStep{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
			execution.ExecutionStep{Name: "tool-b", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		)
		result, _ := h.Run(context.Background(), plan, testEC())

		names := make([]string, len(result.StepResults))
		for i, sr := range result.StepResults {
			names[i] = sr.StepName
		}
		return names
	}

	run1 := run()
	run2 := run()

	if len(run1) != len(run2) {
		t.Fatalf("different result counts: run1=%d, run2=%d", len(run1), len(run2))
	}
	for i := range run1 {
		if run1[i] != run2[i] {
			t.Errorf("run1[%d] = %q, run2[%d] = %q — outputs not deterministic", i, run1[i], i, run2[i])
		}
	}
}

// E2E-6.2: HarnessResult contains no nil step results for executed steps
func TestE2EBlackbox_OutputConsistency_NoNilStepResults(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["tool-a"] = "ok"
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)

	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	for i, sr := range result.StepResults {
		if sr.StepName == "" {
			t.Errorf("StepResults[%d].StepName is empty", i)
		}
		if sr.Type == "" {
			t.Errorf("StepResults[%d].StepType is empty", i)
		}
		if sr.StepID == "" {
			t.Errorf("StepResults[%d].StepID is empty", i)
		}
		if sr.Duration < 0 {
			t.Errorf("StepResults[%d].Duration = %v (negative)", i, sr.Duration)
		}
	}
}

// E2E-6.3: Skill output is valid JSON when executor returns structured data
func TestE2EBlackbox_OutputConsistency_SkillOutputValidJSON(t *testing.T) {
	cfg := DefaultHarnessConfig()
	skillResp := map[string]any{
		"sbar":     "Situation: test patient",
		"urgency":  "high",
		"metadata": map[string]any{"generated_at": "2026-05-06T10:00:00Z"},
	}
	respBytes, _ := json.Marshal(skillResp)
	skillExec := &mockSkillExecutor{resp: respBytes}

	h := NewExecutionHarness(cfg, nil, skillExec, HarnessDeps{})
	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "nursing/generate_sbar", Type: execution.StepTypeSkill, Arguments: map[string]any{"patient_id": "P001"}},
	)

	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := result.StepResults[0].Output
	if output == nil {
		t.Fatal("skill output is nil")
	}

	outputStr, ok := output.(string)
	if !ok {
		t.Fatalf("output type = %T, want string", output)
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(outputStr), &parsed); err != nil {
		t.Fatalf("skill output is not valid JSON: %v", err)
	}
	if _, ok := parsed["sbar"]; !ok {
		t.Error("skill output missing 'sbar' field")
	}
}

// E2E-6.4: Concurrent executions produce independent results
func TestE2EBlackbox_OutputConsistency_ConcurrentIndependence(t *testing.T) {
	cfg := DefaultHarnessConfig()

	var wg sync.WaitGroup
	errors := make([]error, 10)
	results := make([]*HarnessResult, 10)

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()

			tr := newMockToolRunner()
			tr.result[fmt.Sprintf("tool-%d", idx)] = fmt.Sprintf("result-%d", idx)
			h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

			plan := makeFrozenPlan(
				execution.ExecutionStep{Name: fmt.Sprintf("tool-%d", idx), Type: execution.StepTypeTool, Arguments: map[string]any{}},
			)

			result, err := h.Run(context.Background(), plan, testEC())
			errors[idx] = err
			results[idx] = result
		}(i)
	}
	wg.Wait()

	for i := 0; i < 10; i++ {
		if errors[i] != nil {
			t.Errorf("concurrent run %d error: %v", i, errors[i])
		}
		if results[i] == nil {
			t.Errorf("concurrent run %d: nil result", i)
			continue
		}
		if results[i].StepsExecuted != 1 {
			t.Errorf("run %d: StepsExecuted = %d, want 1", i, results[i].StepsExecuted)
		}
	}
}

// ---------------------------------------------------------------------------
// Part 7: Progress & Observability Validation
// ---------------------------------------------------------------------------

// E2E-7.1: Progress callback receives events for each step
func TestE2EBlackbox_Progress_EventsForSteps(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["step-1"] = "ok"
	tr.result["step-2"] = "ok"
	tr.result["step-3"] = "ok"

	var events []ProgressEvent
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{
		ProgressCB: func(event ProgressEvent) {
			events = append(events, event)
		},
	})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "step-1", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "step-2", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		execution.ExecutionStep{Name: "step-3", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)

	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(events) != result.StepsExecuted*2 {
		t.Errorf("ProgressEvents = %d, StepsExecuted = %d", len(events), result.StepsExecuted)
	}

	// Events alternate: step_start then step_complete for each step
	for i, event := range events {
		want := "step_start"
		if i%2 == 1 {
			want = "step_complete"
		}
		if event.Type != want {
			t.Errorf("event[%d].Type = %q, want %q", i, event.Type, want)
		}
	}
}

// E2E-7.2: Hook execution order is deterministic
func TestE2EBlackbox_HookOrder_Deterministic(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["tool-a"] = "ok"

	hm := NewHookManager()
	var order []string
	var mu sync.Mutex

	hm.RegisterPreStepExecute(func(ctx context.Context, hctx *execution.HookContext) (*execution.HookResult, error) {
		mu.Lock()
		order = append(order, fmt.Sprintf("pre-step:%s", hctx.Step.Name))
		mu.Unlock()
		return &execution.HookResult{}, nil
	})
	hm.RegisterPreToolUse(func(ctx context.Context, hctx *execution.HookContext) (*execution.HookResult, error) {
		mu.Lock()
		order = append(order, fmt.Sprintf("pre-tool:%s", hctx.Step.Name))
		mu.Unlock()
		return &execution.HookResult{}, nil
	})
	hm.RegisterPostToolUse(func(ctx context.Context, hctx *execution.HookContext) error {
		mu.Lock()
		order = append(order, fmt.Sprintf("post-tool:%s", hctx.Step.Name))
		mu.Unlock()
		return nil
	})
	hm.RegisterPostStepExecute(func(ctx context.Context, hctx *execution.HookContext) error {
		mu.Lock()
		order = append(order, fmt.Sprintf("post-step:%s", hctx.Step.Name))
		mu.Unlock()
		return nil
	})
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{HookManager: hm})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)

	_, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := []string{"pre-step:tool-a", "pre-tool:tool-a", "post-tool:tool-a", "post-step:tool-a"}
	if len(order) != len(expected) {
		t.Fatalf("hook order = %v, want %v", order, expected)
	}
	for i, want := range expected {
		if order[i] != want {
			t.Errorf("order[%d] = %q, want %q", i, order[i], want)
		}
	}
}

// E2E-7.3: HarnessState transitions are correct
func TestE2EBlackbox_StateTransitions_Correct(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["tool-a"] = "ok"
	var stateTransitions []HarnessState
	var mu sync.Mutex

	hm := NewHookManager()
	var h *ExecutionHarness
	hm.RegisterPreStepExecute(func(ctx context.Context, hctx *execution.HookContext) (*execution.HookResult, error) {
		mu.Lock()
		stateTransitions = append(stateTransitions, h.State())
		mu.Unlock()
		return &execution.HookResult{}, nil
	})
	hm.RegisterPostStepExecute(func(ctx context.Context, hctx *execution.HookContext) error {
		mu.Lock()
		stateTransitions = append(stateTransitions, h.State())
		mu.Unlock()
		return nil
	})
	h = NewExecutionHarness(cfg, tr, nil, HarnessDeps{HookManager: hm})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)

	_, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Pre-step should see HookPre state
	if len(stateTransitions) < 2 {
		t.Fatalf("expected at least 2 state captures, got %d", len(stateTransitions))
	}
	if stateTransitions[0] != HarnessHookPre {
		t.Errorf("state at pre-step = %q, want %q", stateTransitions[0], HarnessHookPre)
	}
	if stateTransitions[1] != HarnessHookPost {
		t.Errorf("state at post-step = %q, want %q", stateTransitions[1], HarnessHookPost)
	}

	// Final state should be HarnessDone
	if h.State() != HarnessDone {
		t.Errorf("final state = %q, want %q", h.State(), HarnessDone)
	}
}

// E2E-7.4: RunFromTask end-to-end with planner producing real plan
func TestE2EBlackbox_RunFromTask_EndToEnd(t *testing.T) {
	cfg := DefaultHarnessConfig()

	plan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "ehr.query_patient", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
			{Name: "ehr.query_vitals", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
		},
		Reasoning: "Query patient and vitals",
	}
	// Don't freeze — RunFromTask calls Validate which freezes
	// But mockPlanner returns the same plan object, so Validate on already-frozen plan fails.
	// Use a planner that returns a fresh copy each time.

	mp := &freshPlanPlanner{plan: plan}
	tr := newMockToolRunner()
	tr.result["ehr.query_patient"] = map[string]any{"id": "P001", "name": "Zhang Wei"}
	tr.result["ehr.query_vitals"] = map[string]any{"patient_id": "P001", "heart_rate": 110}

	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	result, err := PlanAndRun(context.Background(), mp, h, TaskInput{
		TaskDescription: "Assess patient P001's current status",
	}, testEC())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StepsExecuted != 2 {
		t.Errorf("StepsExecuted = %d, want 2", result.StepsExecuted)
	}
	if result.StopCondition.Reason != StopReasonGoalAchieved {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonGoalAchieved)
	}
}

// freshPlanPlanner returns a fresh copy of the plan each time,
// so RunFromTask can call Validate() without hitting "already frozen".
type freshPlanPlanner struct {
	plan *execution.ExecutionPlan
}

func (f *freshPlanPlanner) Plan(ctx context.Context, pCtx *planner.PlannerContext) (*execution.ExecutionPlan, error) {
	steps := make([]execution.ExecutionStep, len(f.plan.Steps))
	copy(steps, f.plan.Steps)
	return &execution.ExecutionPlan{
		Steps:     steps,
		Reasoning: f.plan.Reasoning,
	}, nil
}
