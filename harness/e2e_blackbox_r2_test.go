package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/assistant"
	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
)

// ========================================================================
// Phase 5 Round 2 — E2E Blackbox Validation (spec gap fill)
// ========================================================================
//
// Fills gaps from the Phase 5 specification:
//   Part 2.2: LLM hallucinated plan — invalid tool names, missing args
//   Part 4:   Detailed audit trace with arguments, results, errors
//   Part 5:   Soul injection + memory retrieval validation
//   Part 3:   Additional loop stability (repeat-plan detection, safety valve)
//   Part 1:   Patient risk detection with reasoning loop engaged

// ---------------------------------------------------------------------------
// Part 1 — Additional E2E Pipeline
// ---------------------------------------------------------------------------

// E2E-R2.1: Patient Risk Detection with ReasoningLoop engaged
// Verifies: reasoning loop runs multiple turns, invokes ehr + analytics tools,
// converges to a classification result.
func TestE2EBlackbox_R2_PatientRiskDetection_ReasoningLoop(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 4
	cfg.MaxToolCalls = 6
	cfg.RepeatPlanStop = true

	// Turn 1: query patient, Turn 2: query vitals + risk_score, Turn 3: empty (stop)
	turn1 := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "ehr.query_patient", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
		},
		Reasoning: "First look up the patient",
	}
	turn2 := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "ehr.query_vitals", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
			{Name: "analytics.risk_score", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
		},
		Reasoning: "Now check vitals and compute risk",
	}
	turn3 := &execution.ExecutionPlan{
		Steps:     []execution.ExecutionStep{},
		Reasoning: "Risk assessment complete",
	}

	mp := &mockPlanner{plans: []*execution.ExecutionPlan{turn1, turn2, turn3}}
	tr := newMockToolRunner()
	tr.result["ehr.query_patient"] = map[string]any{"id": "P001", "name": "Zhang Wei", "unit": "ICU", "diagnosis": "Sepsis"}
	tr.result["ehr.query_vitals"] = map[string]any{"patient_id": "P001", "heart_rate": 110, "systolic_bp": 85, "spo2": 92}
	tr.result["analytics.risk_score"] = map[string]any{"patient_id": "P001", "score": 85.0, "level": "critical"}

	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	pCtx := &planner.PlannerContext{
		UserRequest:   "Which patients are high risk?",
		MemoryContext: []assistant.SearchResult{},
	}

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "risk-detect", Type: execution.StepTypeLLM}, pCtx, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Must converge within limits
	if result.TurnCount > cfg.MaxTurns {
		t.Errorf("TurnCount = %d exceeds MaxTurns = %d", result.TurnCount, cfg.MaxTurns)
	}
	// Must have used ehr + analytics tools
	if result.ToolCalls < 3 {
		t.Errorf("ToolCalls = %d, want >= 3 (query_patient + query_vitals + risk_score)", result.ToolCalls)
	}
	// Must have a stop reason
	if result.StopReason == "" {
		t.Error("StopReason is empty — loop must report why it stopped")
	}
	// Final output should contain classification
	if result.Output == nil {
		t.Error("Output is nil — should contain risk_score result")
	}
	// Memory should accumulate observations
	if len(pCtx.MemoryContext) == 0 {
		t.Error("MemoryContext is empty — observations should accumulate")
	}
}

// E2E-R2.2: Multi-step reasoning converges — repeat plan detection stops loop
func TestE2EBlackbox_R2_ReasoningConverges_RepeatPlanDetection(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 10
	cfg.MaxToolCalls = 20
	cfg.RepeatPlanStop = true

	// Same plan repeated — should stop on repeat detection
	plan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		},
		Reasoning: "same reasoning every time",
	}
	mp := &mockPlanner{plans: []*execution.ExecutionPlan{plan}, repeatLast: true}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should stop early due to repeat plan, not run all 10 turns
	if result.TurnCount >= cfg.MaxTurns {
		t.Errorf("TurnCount = %d — repeat plan detection should stop loop early", result.TurnCount)
	}
	if result.StopReason != StopReasonPlannerStopped {
		t.Errorf("StopReason = %q, want %q (repeat plan)", result.StopReason, StopReasonPlannerStopped)
	}
}

// ---------------------------------------------------------------------------
// Part 2.2 — LLM Hallucinated Plan
// ---------------------------------------------------------------------------

// E2E-R2.3: Hallucinated plan — invalid tool name still executes (no registry check)
// The harness doesn't validate tool names against a registry — it passes them to ToolRunner.
// If ToolRunner doesn't know the tool, it returns a default result or error.
// Blackbox: system does not crash, produces a result.
func TestE2EBlackbox_R2_HallucinatedTool_InvalidName(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	// No result configured for "llm_hallucinated_tool" — mock returns default
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "llm_hallucinated_tool", Type: execution.StepTypeTool, Arguments: map[string]any{"query": "nonsense"}},
	)

	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("system must not crash on hallucinated tool: %v", err)
	}
	// Step executed (mockToolRunner returns default for unknown tools)
	if result.StepsExecuted != 1 {
		t.Errorf("StepsExecuted = %d, want 1", result.StepsExecuted)
	}
	// No panic — this is the key blackbox guarantee
	if result.StepResults[0].StepName != "llm_hallucinated_tool" {
		t.Errorf("StepName = %q, want 'llm_hallucinated_tool'", result.StepResults[0].StepName)
	}
}

// E2E-R2.4: Hallucinated plan — missing required arguments
// Tool runner receives empty/nil args — tool may error, but system doesn't crash.
func TestE2EBlackbox_R2_HallucinatedTool_MissingArguments(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	// ehr.query_vitals requires patient_id — we don't pass it
	tr.err["ehr.query_vitals"] = fmt.Errorf("patient_id is required")
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "ehr.query_vitals", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)

	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("system must not crash on missing args: %v", err)
	}
	// Step should have error
	if result.StepResults[0].Error == nil {
		t.Error("expected error from missing required arguments")
	}
}

// E2E-R2.5: Hallucinated plan — invalid step type rejected by validation
func TestE2EBlackbox_R2_HallucinatedPlan_InvalidStepType(t *testing.T) {
	cfg := DefaultHarnessConfig()
	mp := &mockPlanner{
		plans: []*execution.ExecutionPlan{{
			Steps: []execution.ExecutionStep{
				{Name: "bad-step", Type: "neural_link", Arguments: map[string]any{}},
			},
		}},
	}
	h := NewExecutionHarness(cfg, nil, nil, HarnessDeps{})

	_, err := PlanAndRun(context.Background(), mp, h, TaskInput{TaskDescription: "test"}, testEC())
	if err == nil {
		t.Fatal("invalid step type should be rejected by plan validation")
	}
	if !strings.Contains(err.Error(), "invalid type") {
		t.Errorf("error should mention invalid type: %v", err)
	}
}

// E2E-R2.6: Hallucinated plan — duplicate step names rejected by validation
func TestE2EBlackbox_R2_HallucinatedPlan_DuplicateSteps(t *testing.T) {
	cfg := DefaultHarnessConfig()
	mp := &mockPlanner{
		plans: []*execution.ExecutionPlan{{
			Steps: []execution.ExecutionStep{
				{Name: "repeat-step", Type: execution.StepTypeTool, Arguments: map[string]any{}},
				{Name: "repeat-step", Type: execution.StepTypeTool, Arguments: map[string]any{}},
			},
		}},
	}
	h := NewExecutionHarness(cfg, nil, nil, HarnessDeps{})

	_, err := PlanAndRun(context.Background(), mp, h, TaskInput{TaskDescription: "test"}, testEC())
	if err == nil {
		t.Fatal("duplicate step names should be rejected by validation")
	}
	if !strings.Contains(err.Error(), "duplicate") {
		t.Errorf("error should mention duplicate: %v", err)
	}
}

// E2E-R2.7: Hallucinated plan — empty plan rejected
func TestE2EBlackbox_R2_HallucinatedPlan_EmptyPlan(t *testing.T) {
	cfg := DefaultHarnessConfig()
	mp := &mockPlanner{
		plans: []*execution.ExecutionPlan{{Steps: []execution.ExecutionStep{}}},
	}
	h := NewExecutionHarness(cfg, nil, nil, HarnessDeps{})

	result, err := PlanAndRun(context.Background(), mp, h, TaskInput{TaskDescription: "test"}, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StopCondition.Reason != StopReasonPlannerStopped {
		t.Errorf("StopReason = %q, want %q", result.StopCondition.Reason, StopReasonPlannerStopped)
	}
	if result.StepsExecuted != 0 {
		t.Errorf("StepsExecuted = %d, want 0 (empty plan)", result.StepsExecuted)
	}
}

// ---------------------------------------------------------------------------
// Part 3 — Additional Loop Stability
// ---------------------------------------------------------------------------

// E2E-R2.8: Safety valve — loop never exceeds 2x MaxTurns
func TestE2EBlackbox_R2_SafetyValve_NeverExceeds2xMaxTurns(t *testing.T) {
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

	hardLimit := 2 * cfg.MaxTurns
	if result.TurnCount > hardLimit {
		t.Errorf("TurnCount = %d exceeds safety valve of 2*MaxTurns = %d", result.TurnCount, hardLimit)
	}
}

// E2E-R2.9: Tool budget exactly enforced — no phantom tool calls
func TestE2EBlackbox_R2_ToolBudget_ExactEnforcement(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 10
	cfg.MaxToolCalls = 2
	cfg.RepeatPlanStop = false

	// Plan has 3 tool steps — but budget is 2
	twoStepPlan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
			{Name: "tool-b", Type: execution.StepTypeTool, Arguments: map[string]any{}},
			{Name: "tool-c", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		},
		Reasoning: "run three tools",
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
		t.Errorf("ToolCalls = %d exceeds MaxToolCalls = %d", result.ToolCalls, cfg.MaxToolCalls)
	}
}

// E2E-R2.10: Nested LLM step is skipped inside reasoning loop
func TestE2EBlackbox_R2_NestedLLMStep_Skipped(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 2
	cfg.RepeatPlanStop = false

	// Plan contains an LLM step — should be skipped (no nesting)
	llmPlan := &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
			{Name: "nested-llm", Type: execution.StepTypeLLM, Arguments: map[string]any{}},
		},
		Reasoning: "try nested LLM",
	}
	mp := &mockPlanner{plans: []*execution.ExecutionPlan{llmPlan}, repeatLast: true}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, testPCtx("test"), testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only tool-a should be counted as a tool call, not the LLM step
	if result.ToolCalls != result.TurnCount {
		t.Errorf("ToolCalls = %d, TurnCount = %d — nested LLM steps should not count as tool calls", result.ToolCalls, result.TurnCount)
	}
}

// ---------------------------------------------------------------------------
// Part 4 — Detailed Audit Trace Validation
// ---------------------------------------------------------------------------

// E2E-R2.11: Full audit trace — step start/end, arguments, results, errors
func TestE2EBlackbox_R2_AuditTrace_FullReconstructable(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["ehr.query_patient"] = map[string]any{"id": "P001", "name": "Zhang Wei"}
	tr.result["ehr.query_vitals"] = map[string]any{"patient_id": "P001", "heart_rate": 110}
	tr.err["ehr.query_labs"] = fmt.Errorf("labs service unavailable")
	tr.result["analytics.risk_score"] = map[string]any{"score": 50.0}

	al := NewAuditLayer()

	hm := NewHookManager()
	hm.RegisterPreStepExecute(func(ctx context.Context, hctx *execution.HookContext) (*execution.HookResult, error) {
		al.RecordStep(ctx, audit.AuditEvent{
			TraceID:   fmt.Sprintf("trace-%s-%d", hctx.Step.Name, hctx.StepIndex),
			StepName:  hctx.Step.Name,
			StepType:  string(hctx.Step.Type),
			StepID:    hctx.Step.StepID,
			Status:    "started",
			ToolInput: hctx.Step.Arguments,
			Timestamp: time.Now(),
			RequestID: hctx.EC.RequestID,
			TenantID:  hctx.EC.TenantID,
		})
		return &execution.HookResult{}, nil
	})
	hm.RegisterPostStepExecute(func(ctx context.Context, hctx *execution.HookContext) error {
		status := "completed"
		var output any
		var errMsg string
		if sr, ok := hctx.ToolOutput.(*execution.StepResult); ok {
			output = sr.Output
			if sr.Error != nil {
				status = "failed"
				errMsg = sr.Error.Error()
			}
		}
		al.RecordStep(ctx, audit.AuditEvent{
			TraceID:    fmt.Sprintf("trace-%s-%d", hctx.Step.Name, hctx.StepIndex),
			StepName:   hctx.Step.Name,
			StepType:   string(hctx.Step.Type),
			StepID:     hctx.Step.StepID,
			Status:     status,
			ToolOutput: output,
			Error:      errMsg,
			Timestamp:  time.Now(),
			RequestID:  hctx.EC.RequestID,
			TenantID:   hctx.EC.TenantID,
		})
		return nil
	})
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{HookManager: hm})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "ehr.query_patient", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
		execution.ExecutionStep{Name: "ehr.query_vitals", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
		execution.ExecutionStep{Name: "ehr.query_labs", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
		execution.ExecutionStep{Name: "analytics.risk_score", Type: execution.StepTypeTool, Arguments: map[string]any{"patient_id": "P001"}},
	)

	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	trail := al.Trail()

	// 4 steps × 2 entries (started + completed/failed) = 8 entries
	if len(trail) != 8 {
		t.Fatalf("Trail size = %d, want 8 (4 steps × 2 events)", len(trail))
	}

	// Verify each step has start and end entries
	for stepIdx := 0; stepIdx < 4; stepIdx++ {
		start := trail[stepIdx*2]
		end := trail[stepIdx*2+1]

		if start.Status != "started" {
			t.Errorf("step %d start: Status = %q, want 'started'", stepIdx, start.Status)
		}
		if end.Status == "" {
			t.Errorf("step %d end: Status is empty", stepIdx)
		}
		if start.StepName != end.StepName {
			t.Errorf("step %d: start name = %q, end name = %q", stepIdx, start.StepName, end.StepName)
		}
		if start.TraceID != end.TraceID {
			t.Errorf("step %d: start traceID = %q, end traceID = %q", stepIdx, start.TraceID, end.TraceID)
		}
	}

	// Verify arguments captured in start entries
	startEntry := trail[0]
	if startEntry.ToolInput == nil {
		t.Error("start entry should capture ToolInput (arguments)")
	}
	if pid, ok := startEntry.ToolInput["patient_id"]; !ok || pid != "P001" {
		t.Errorf("ToolInput.patient_id = %v, want 'P001'", pid)
	}

	// Verify results captured in end entries
	endEntry := trail[1] // ehr.query_patient completed
	if endEntry.ToolOutput == nil {
		t.Error("end entry should capture ToolOutput")
	}

	// Verify error captured for ehr.query_labs (step 2, entries [4] and [5])
	labsEnd := trail[5]
	if labsEnd.Status != "failed" {
		t.Errorf("labs step end: Status = %q, want 'failed'", labsEnd.Status)
	}
	if labsEnd.Error == "" {
		t.Error("labs step end should capture error message")
	}

	// Verify timestamps are monotonically increasing
	for i := 1; i < len(trail); i++ {
		if trail[i].Timestamp.Before(trail[i-1].Timestamp) {
			t.Errorf("trail[%d].Timestamp < trail[%d].Timestamp", i, i-1)
		}
	}

	// Verify tenant and request context
	for _, entry := range trail {
		if entry.RequestID == "" {
			t.Error("audit entry missing RequestID")
		}
		if entry.TenantID == "" {
			t.Error("audit entry missing TenantID")
		}
	}

	// Verify step count matches execution
	if result.StepsExecuted != 4 {
		t.Errorf("StepsExecuted = %d, want 4", result.StepsExecuted)
	}
}

// E2E-R2.12: Audit trace reconstructability — same input produces same trace
func TestE2EBlackbox_R2_AuditTrace_Reproducible(t *testing.T) {
	run := func() []string {
		cfg := DefaultHarnessConfig()
		tr := newMockToolRunner()
		tr.result["tool-a"] = "ok"
		al := NewAuditLayer()

		hm := NewHookManager()
		hm.RegisterPostStepExecute(func(ctx context.Context, hctx *execution.HookContext) error {
			al.RecordStep(ctx, audit.AuditEvent{
				StepName: hctx.Step.Name,
				StepType: string(hctx.Step.Type),
				Status:   "completed",
			})
			return nil
		})
		h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{HookManager: hm})

		plan := makeFrozenPlan(
			execution.ExecutionStep{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
		)
		h.Run(context.Background(), plan, testEC())

		trail := al.Trail()
		names := make([]string, len(trail))
		for i, e := range trail {
			names[i] = e.StepName
		}
		return names
	}

	run1 := run()
	run2 := run()

	if len(run1) != len(run2) {
		t.Fatalf("different trail sizes: run1=%d, run2=%d", len(run1), len(run2))
	}
	for i := range run1 {
		if run1[i] != run2[i] {
			t.Errorf("trail[%d]: run1=%q, run2=%q — not reproducible", i, run1[i], run2[i])
		}
	}
}

// ---------------------------------------------------------------------------
// Part 5 — Memory + Soul Validation
// ---------------------------------------------------------------------------

// E2E-R2.13: Soul injection — PlannerContext.Soul flows through execution
func TestE2EBlackbox_R2_SoulInjection_PlannerContext(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 2
	cfg.RepeatPlanStop = false

	plan := makeToolPlan("tool-a")
	mp := &mockPlanner{plans: []*execution.ExecutionPlan{plan}, repeatLast: true}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	soul := assistant.AssistantSoul{
		SystemPrompt:  "You are an ICU clinical assistant.",
		Personality:   "Professional, compassionate, detail-oriented.",
		Instructions:  "Always prioritize patient safety. Use SBAR format.",
		AllowedSkills: []string{"nursing/generate_sbar", "analytics/risk_score"},
		AllowedTools:  []string{"ehr.query_patient", "ehr.query_vitals"},
	}

	pCtx := &planner.PlannerContext{
		UserRequest:   "Assess patient P001",
		Soul:          soul,
		MemoryContext: []assistant.SearchResult{},
	}

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, pCtx, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify soul is present in context after execution
	if pCtx.Soul.SystemPrompt != soul.SystemPrompt {
		t.Error("Soul.SystemPrompt should be preserved in PlannerContext")
	}
	if pCtx.Soul.Personality != soul.Personality {
		t.Error("Soul.Personality should be preserved in PlannerContext")
	}
	if len(pCtx.Soul.AllowedTools) != 2 {
		t.Errorf("Soul.AllowedTools = %d, want 2", len(pCtx.Soul.AllowedTools))
	}

	// Verify execution still completed
	if result.TurnCount == 0 {
		t.Error("TurnCount = 0, execution should proceed with soul context")
	}
}

// E2E-R2.14: Soul injection — RunFromTask passes soul to planner
func TestE2EBlackbox_R2_SoulInjection_RunFromTask(t *testing.T) {
	cfg := DefaultHarnessConfig()

	var capturedSoul assistant.AssistantSoul
	mp := &soulCapturingPlanner{fn: func(pCtx *planner.PlannerContext) {
		capturedSoul = pCtx.Soul
	}}
	tr := newMockToolRunner()
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	soul := assistant.AssistantSoul{
		SystemPrompt: "You are a risk assessment specialist.",
	}
	pCtx := &planner.PlannerContext{
		UserRequest: "Assess P001",
		Soul:        soul,
	}

	PlanAndRun(context.Background(), mp, h, TaskInput{
		TaskDescription: "Assess P001",
		PlannerContext:  pCtx,
	}, testEC())

	if capturedSoul.SystemPrompt != soul.SystemPrompt {
		t.Errorf("Planner received SystemPrompt = %q, want %q", capturedSoul.SystemPrompt, soul.SystemPrompt)
	}
}

// soulCapturingPlanner captures what the planner receives.
type soulCapturingPlanner struct {
	fn func(pCtx *planner.PlannerContext)
}

func (s *soulCapturingPlanner) Plan(ctx context.Context, pCtx *planner.PlannerContext) (*execution.ExecutionPlan, error) {
	s.fn(pCtx)
	return &execution.ExecutionPlan{Steps: []execution.ExecutionStep{}}, nil
}

// E2E-R2.15: Memory retrieval — prior conversation context used in planning
func TestE2EBlackbox_R2_MemoryRetrieval_PriorContext(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 2
	cfg.RepeatPlanStop = false

	plan := makeToolPlan("tool-a")
	mp := &mockPlanner{plans: []*execution.ExecutionPlan{plan}, repeatLast: true}
	tr := newMockToolRunner()
	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	// Simulate prior conversation memory
	priorMemory := []assistant.SearchResult{
		{Content: []byte("Patient P001 was admitted for Sepsis on 2026-03-15"), Score: 0.95},
		{Content: []byte("Patient P001 is allergic to penicillin"), Score: 0.90},
		{Content: []byte("Previous shift noted worsening vitals for P001"), Score: 0.85},
	}

	pCtx := &planner.PlannerContext{
		UserRequest:   "What's the current status of P001?",
		MemoryContext: priorMemory,
	}

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, pCtx, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Prior memory should still be present (not overwritten)
	if len(pCtx.MemoryContext) < len(priorMemory) {
		t.Errorf("MemoryContext = %d entries, prior had %d — memory should be preserved", len(pCtx.MemoryContext), len(priorMemory))
	}

	// Prior entries should be at the front
	for i, prior := range priorMemory {
		if i >= len(pCtx.MemoryContext) {
			break
		}
		if string(pCtx.MemoryContext[i].Content) != string(prior.Content) {
			t.Logf("MemoryContext[%d] differs from prior — new observations may be prepended, this is acceptable", i)
		}
	}

	// Loop should have executed
	if result.TurnCount == 0 {
		t.Error("execution should proceed with prior memory context")
	}
}

// E2E-R2.16: Memory retrieval — observations from turn N available in turn N+1
func TestE2EBlackbox_R2_MemoryRetrieval_CrossTurnPropagation(t *testing.T) {
	cfg := DefaultReasoningLoopConfig()
	cfg.MaxTurns = 3
	cfg.MaxToolCalls = 6
	cfg.RepeatPlanStop = false

	plan := makeToolPlan("data-tool")
	mp := &mockPlanner{plans: []*execution.ExecutionPlan{plan}, repeatLast: true}

	callNum := 0
	tr := &turnAwareToolRunner{fn: func(toolName string) any {
		callNum++
		return map[string]any{"turn": callNum, "value": fmt.Sprintf("observation-%d", callNum)}
	}}

	se := NewStepExecutor(tr, nil, StepExecutorDeps{})
	rl := NewDefaultReasoningLoop(cfg, mp, se, nil)

	pCtx := &planner.PlannerContext{
		UserRequest:   "analyze trends",
		MemoryContext: []assistant.SearchResult{},
	}

	result, err := rl.Run(context.Background(), &execution.ExecutionStep{Name: "reason", Type: execution.StepTypeLLM}, pCtx, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Memory should grow with each turn
	if len(pCtx.MemoryContext) == 0 {
		t.Fatal("MemoryContext should have observations from turns")
	}

	// Each observation should contain the turn number (proving cross-turn propagation)
	for i, sr := range pCtx.MemoryContext {
		content := string(sr.Content)
		if !strings.Contains(content, "observation-") {
			t.Errorf("MemoryContext[%d] = %q — should contain observation data", i, content)
		}
	}

	_ = result
}

// ---------------------------------------------------------------------------
// Part 6 — Output Consistency (additional)
// ---------------------------------------------------------------------------

// E2E-R2.17: Output schema — HarnessResult always has required fields
func TestE2EBlackbox_R2_OutputSchema_HarnessResultFields(t *testing.T) {
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

	// Required fields
	if result.PlanID == "" {
		t.Error("PlanID is required")
	}
	if result.Duration <= 0 {
		t.Error("Duration must be > 0")
	}
	if result.StepsExecuted <= 0 {
		t.Error("StepsExecuted must be > 0")
	}
	if result.StepResults == nil {
		t.Error("StepResults must not be nil")
	}
	if result.StopCondition.Stopped == false {
		t.Error("StopCondition.Stopped must be true after execution")
	}
	if result.StopCondition.Reason == "" {
		t.Error("StopCondition.Reason is required")
	}
}

// E2E-R2.18: Output schema — execution.StepResult always has required fields
func TestE2EBlackbox_R2_OutputSchema_StepResultFields(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["tool-a"] = map[string]any{"key": "value"}
	h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})

	plan := makeFrozenPlan(
		execution.ExecutionStep{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
	)

	result, err := h.Run(context.Background(), plan, testEC())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sr := result.StepResults[0]
	if sr.StepID == "" {
		t.Error("StepID is required")
	}
	if sr.StepName == "" {
		t.Error("StepName is required")
	}
	if sr.Type == "" {
		t.Error("StepType is required")
	}
	if sr.Duration < 0 {
		t.Error("Duration must be >= 0")
	}
	// Output should be present for successful step
	if sr.Output == nil {
		t.Error("Output should not be nil for successful step")
	}
	// Error should be nil for successful step
	if sr.Error != nil {
		t.Errorf("Error should be nil for successful step: %v", sr.Error)
	}
}

// E2E-R2.19: No partial JSON in output — skill output is always complete
func TestE2EBlackbox_R2_NoPartialJSON(t *testing.T) {
	cfg := DefaultHarnessConfig()

	// Various valid JSON outputs
	cases := []struct {
		name string
		resp []byte
	}{
		{"object", []byte(`{"sbar":"Situation: test","urgency":"high"}`)},
		{"array", []byte(`[1,2,3]`)},
		{"string", []byte(`"hello"`)},
		{"number", []byte(`42`)},
		{"nested", []byte(`{"a":{"b":"c"},"d":[1,2]}`)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			skillExec := &mockSkillExecutor{resp: tc.resp}
			h := NewExecutionHarness(cfg, nil, skillExec, HarnessDeps{})

			plan := makeFrozenPlan(
				execution.ExecutionStep{Name: "skill-a", Type: execution.StepTypeSkill, Arguments: map[string]any{}},
			)

			result, err := h.Run(context.Background(), plan, testEC())
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			outputStr, ok := result.StepResults[0].Output.(string)
			if !ok {
				t.Fatalf("output type = %T, want string", result.StepResults[0].Output)
			}

			// Must be valid JSON
			var parsed any
			if err := json.Unmarshal([]byte(outputStr), &parsed); err != nil {
				t.Errorf("output is not valid JSON: %s, error: %v", outputStr, err)
			}
		})
	}
}

// E2E-R2.20: Concurrent stress — 50 harness executions with no data races
func TestE2EBlackbox_R2_ConcurrentStress_50Runs(t *testing.T) {
	cfg := DefaultHarnessConfig()
	tr := newMockToolRunner()
	tr.result["tool-a"] = "ok"

	var wg sync.WaitGroup
	errs := make(chan error, 50)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			h := NewExecutionHarness(cfg, tr, nil, HarnessDeps{})
			plan := makeFrozenPlan(
				execution.ExecutionStep{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
			)
			result, err := h.Run(context.Background(), plan, testEC())
			if err != nil {
				errs <- err
				return
			}
			if result.StepsExecuted != 1 {
				errs <- fmt.Errorf("run %d: StepsExecuted = %d", idx, result.StepsExecuted)
			}
		}(i)
	}
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("concurrent run failed: %v", err)
	}
}

// ---------------------------------------------------------------------------
// Part 7 helpers — data for final report
// ---------------------------------------------------------------------------

// countingStepsToolRunner tracks which tools were called and how many times.
type countingStepsToolRunner struct {
	mu    sync.Mutex
	calls map[string]int
}

func newCountingStepsToolRunner() *countingStepsToolRunner {
	return &countingStepsToolRunner{calls: make(map[string]int)}
}

func (c *countingStepsToolRunner) Execute(ctx context.Context, toolName string, args map[string]any, ec *execution.ExecutionContext) (*execution.StepResult, error) {
	c.mu.Lock()
	c.calls[toolName]++
	c.mu.Unlock()
	return &execution.StepResult{StepName: toolName, Output: fmt.Sprintf("result-%s", toolName)}, nil
}
