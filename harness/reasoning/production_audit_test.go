package reasoning_test

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-runtime/harness"
	"github.com/openbotstack/openbotstack-runtime/harness/reasoning"
)

// ========================================================================
// Local test helpers (can't access unexported mocks from harness package)
// ========================================================================

// mockToolRunner implements toolrunner.ToolRunner for testing.
type auditToolRunner struct {
	mu     sync.Mutex
	result map[string]any
	err    map[string]error
}

func newAuditToolRunner() *auditToolRunner {
	return &auditToolRunner{
		result: make(map[string]any),
		err:    make(map[string]error),
	}
}

func (m *auditToolRunner) Execute(ctx context.Context, toolName string, args map[string]any, ec *execution.ExecutionContext) (*execution.StepResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.err[toolName]; ok {
		return nil, err
	}
	if out, ok := m.result[toolName]; ok {
		return &execution.StepResult{StepName: toolName, Output: out}, nil
	}
	return &execution.StepResult{StepName: toolName, Output: fmt.Sprintf("result-%s", toolName)}, nil
}

// slowAuditRunner simulates a slow tool.
type slowAuditRunner struct {
	delay time.Duration
}

func (s *slowAuditRunner) Execute(ctx context.Context, toolName string, args map[string]any, ec *execution.ExecutionContext) (*execution.StepResult, error) {
	select {
	case <-time.After(s.delay):
		return &execution.StepResult{StepName: toolName, Output: "delayed"}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// makeAuditPlan creates a frozen plan with tool steps.
func makeAuditPlan(stepNames ...string) *execution.ExecutionPlan {
	steps := make([]execution.ExecutionStep, len(stepNames))
	for i, name := range stepNames {
		steps[i] = execution.ExecutionStep{
			Name:      name,
			Type:      execution.StepTypeTool,
			Arguments: map[string]any{},
		}
	}
	plan := &execution.ExecutionPlan{Steps: steps}
	_ = plan.Validate()
	return plan
}

func auditEC() *execution.ExecutionContext {
	return execution.NewExecutionContext(context.Background(), "req", "asst", "sess", "tenant", "user")
}

// buildTrail converts HarnessResult step results to AuditEvent records.
func buildTrail(result *harness.HarnessResult) []audit.AuditEvent {
	var trail []audit.AuditEvent
	for _, sr := range result.StepResults {
		trail = append(trail, audit.AuditEvent{
			StepID:   sr.StepID,
			StepName: sr.StepName,
			StepType: sr.Type,
			Status:   "started",
		})
		status := "completed"
		errMsg := ""
		if sr.Error != nil {
			status = "failed"
			errMsg = sr.Error.Error()
		}
		trail = append(trail, audit.AuditEvent{
			StepID:     sr.StepID,
			StepName:   sr.StepName,
			StepType:   sr.Type,
			Status:     status,
			ToolOutput: sr.Output,
			Error:      errMsg,
			Duration:   sr.Duration,
		})
	}
	return trail
}

// ========================================================================
// PART 1 — Reasoning Correctness Validation
// ========================================================================

// RC1: Reasoning tree matches execution steps exactly
func TestReasoning_MatchesExecution(t *testing.T) {
	plan := makeAuditPlan("ehr.query_patient", "ehr.query_vitals", "analytics.risk_score")

	tr := newAuditToolRunner()
	tr.result["ehr.query_patient"] = map[string]any{"name": "Alice", "age": 45}
	tr.result["ehr.query_vitals"] = map[string]any{"heart_rate": 110}
	tr.result["analytics.risk_score"] = map[string]any{"score": 0.72}

	h := harness.NewExecutionHarness(harness.DefaultHarnessConfig(), tr, nil, harness.HarnessDeps{})
	result, err := h.Run(context.Background(), plan, auditEC())
	if err != nil {
		t.Fatalf("execution error: %v", err)
	}

	trail := buildTrail(result)
	tree := reasoning.BuildReasoningTree(trail)

	// Count tool_call nodes
	toolCallCount := 0
	for _, child := range tree.Children {
		if child.Type == reasoning.EventToolCall {
			toolCallCount++
		}
	}

	if toolCallCount != result.StepsExecuted {
		t.Errorf("reasoning tree has %d tool_calls, execution had %d steps", toolCallCount, result.StepsExecuted)
	}

	// Validate step names match
	for i, sr := range result.StepResults {
		if i >= len(tree.Children)-1 {
			break
		}
		call := tree.Children[i]
		if call.Type != reasoning.EventToolCall {
			continue
		}
		expectedName := strings.ReplaceAll(sr.StepName, ".", " ")
		expectedName = strings.ReplaceAll(expectedName, "_", " ")
		if !strings.Contains(call.Summary, expectedName) {
			t.Errorf("tree child[%d] summary %q doesn't match step %q", i, call.Summary, sr.StepName)
		}
	}
}

// RC2: Deterministic — same input → same reasoning tree (10 runs)
func TestReasoning_Deterministic_10Runs(t *testing.T) {
	trail := []audit.AuditEvent{
		{StepID: "s1", StepName: "ehr.query_patient", StepType: string(execution.StepTypeTool), Status: "started"},
		{StepID: "s1", StepName: "ehr.query_patient", StepType: string(execution.StepTypeTool), Status: "completed", ToolOutput: map[string]any{"name": "Bob"}, Duration: 30 * time.Millisecond},
		{StepID: "s2", StepName: "analytics.risk_score", StepType: string(execution.StepTypeTool), Status: "started"},
		{StepID: "s2", StepName: "analytics.risk_score", StepType: string(execution.StepTypeTool), Status: "completed", ToolOutput: map[string]any{"score": 0.85}, Duration: 20 * time.Millisecond},
	}

	var hashes [][32]byte
	for i := 0; i < 10; i++ {
		tree := reasoning.BuildReasoningTree(trail)
		data, _ := json.Marshal(tree)
		hashes = append(hashes, sha256.Sum256(data))
	}

	for i := 1; i < 10; i++ {
		if hashes[i] != hashes[0] {
			t.Errorf("DETERMINISM VIOLATION: run %d hash != run 0", i)
		}
	}
}

// RC3: No hallucinated steps
func TestReasoning_NoHallucinatedSteps(t *testing.T) {
	plan := makeAuditPlan("tool-a", "tool-b", "tool-c")
	tr := newAuditToolRunner()
	h := harness.NewExecutionHarness(harness.DefaultHarnessConfig(), tr, nil, harness.HarnessDeps{})

	result, err := h.Run(context.Background(), plan, auditEC())
	if err != nil {
		t.Fatalf("execution error: %v", err)
	}

	trail := buildTrail(result)
	tree := reasoning.BuildReasoningTree(trail)

	// Collect step IDs from tree
	treeIDs := map[string]bool{}
	for _, child := range tree.Children {
		if child.Type == reasoning.EventToolCall {
			treeIDs[child.StepID] = true
		}
	}

	// Collect step IDs from execution
	execIDs := map[string]bool{}
	for _, sr := range result.StepResults {
		execIDs[sr.StepID] = true
	}

	// Every tree step must exist in execution
	for id := range treeIDs {
		if !execIDs[id] {
			t.Errorf("HALLUCINATED: reasoning has step %q not in execution", id)
		}
	}
}

// ========================================================================
// PART 2 — Medical Safety Scenarios
// ========================================================================

// MS1: Incomplete data — system completes execution
func TestMedicalSafety_IncompleteData(t *testing.T) {
	plan := makeAuditPlan("ehr.query_patient", "ehr.query_vitals", "analytics.risk_score")

	tr := newAuditToolRunner()
	tr.result["ehr.query_patient"] = map[string]any{"name": "Alice"}
	tr.result["ehr.query_vitals"] = map[string]any{} // incomplete
	tr.result["analytics.risk_score"] = map[string]any{"score": 0.0, "level": "unknown"}

	h := harness.NewExecutionHarness(harness.DefaultHarnessConfig(), tr, nil, harness.HarnessDeps{})
	result, err := h.Run(context.Background(), plan, auditEC())
	if err != nil {
		t.Fatalf("execution error: %v", err)
	}

	if result.StepsExecuted != 3 {
		t.Errorf("incomplete data: steps = %d, want 3 (must complete even with empty data)", result.StepsExecuted)
	}

	trail := buildTrail(result)
	tree := reasoning.BuildReasoningTree(trail)
	toolCalls := 0
	for _, child := range tree.Children {
		if child.Type == reasoning.EventToolCall {
			toolCalls++
		}
	}
	if toolCalls != 3 {
		t.Errorf("incomplete data tree: %d calls, want 3", toolCalls)
	}
}

// MS2: Conflicting data — system handles gracefully
func TestMedicalSafety_ConflictingData(t *testing.T) {
	plan := makeAuditPlan("ehr.query_patient", "ehr.query_vitals")

	tr := newAuditToolRunner()
	tr.result["ehr.query_patient"] = map[string]any{"name": "Bob", "age": 30}
	tr.result["ehr.query_vitals"] = map[string]any{"heart_rate": 45, "systolic_bp": 180, "conflict": "age_vitals_mismatch"}

	h := harness.NewExecutionHarness(harness.DefaultHarnessConfig(), tr, nil, harness.HarnessDeps{})
	result, err := h.Run(context.Background(), plan, auditEC())
	if err != nil {
		t.Fatalf("execution error: %v", err)
	}

	if result.StopCondition.Reason != harness.StopReasonGoalAchieved {
		t.Errorf("conflicting data stop = %q, want goal_achieved", result.StopCondition.Reason)
	}
}

// MS3: Missing guideline — system continues
func TestMedicalSafety_MissingGuideline(t *testing.T) {
	plan := makeAuditPlan("ehr.query_patient", "guidelines.search")

	tr := newAuditToolRunner()
	tr.result["ehr.query_patient"] = map[string]any{"name": "Carol"}
	tr.result["guidelines.search"] = nil

	h := harness.NewExecutionHarness(harness.DefaultHarnessConfig(), tr, nil, harness.HarnessDeps{})
	result, err := h.Run(context.Background(), plan, auditEC())
	if err != nil {
		t.Fatalf("execution error: %v", err)
	}

	if result.StepsExecuted != 2 {
		t.Errorf("missing guideline: steps = %d, want 2", result.StepsExecuted)
	}
}

// MS4: LLM failure — partial results preserved
func TestMedicalSafety_LLMFailureFallback(t *testing.T) {
	plan := makeAuditPlan("ehr.query_patient", "llm.diagnose")

	tr := newAuditToolRunner()
	tr.result["ehr.query_patient"] = map[string]any{"name": "Dave"}
	tr.err["llm.diagnose"] = fmt.Errorf("LLM service unavailable")

	fh := harness.NewFailureHandler(execution.DefaultRetryPolicy())
	h := harness.NewExecutionHarness(harness.DefaultHarnessConfig(), tr, nil, harness.HarnessDeps{})
	h.SetFailureHandler(fh)

	result, err := h.Run(context.Background(), plan, auditEC())
	if err == nil {
		t.Error("expected error from LLM failure")
	}

	if len(result.StepResults) == 0 {
		t.Error("no partial results preserved after LLM failure")
	}
	if result.StepResults[0].StepName != "ehr.query_patient" {
		t.Errorf("first step = %q, want ehr.query_patient", result.StepResults[0].StepName)
	}
}

// ========================================================================
// PART 3 — Debug Layer Integrity
// ========================================================================

// DL1: Every audit step must appear in reasoning tree
func TestDebugTree_Complete(t *testing.T) {
	plan := makeAuditPlan("step-a", "step-b", "step-c", "step-d")
	tr := newAuditToolRunner()
	h := harness.NewExecutionHarness(harness.DefaultHarnessConfig(), tr, nil, harness.HarnessDeps{})

	result, err := h.Run(context.Background(), plan, auditEC())
	if err != nil {
		t.Fatalf("execution error: %v", err)
	}

	trail := buildTrail(result)
	tree := reasoning.BuildReasoningTree(trail)

	toolCalls := 0
	for _, child := range tree.Children {
		if child.Type == reasoning.EventToolCall {
			toolCalls++
		}
	}

	if toolCalls != result.StepsExecuted {
		t.Errorf("COMPLETENESS: tree has %d tool_calls, execution had %d steps", toolCalls, result.StepsExecuted)
	}
}

// DL2: Step order must match execution order
func TestDebugTree_OrderCorrect(t *testing.T) {
	plan := makeAuditPlan("alpha", "beta", "gamma", "delta")
	tr := newAuditToolRunner()
	h := harness.NewExecutionHarness(harness.DefaultHarnessConfig(), tr, nil, harness.HarnessDeps{})

	result, err := h.Run(context.Background(), plan, auditEC())
	if err != nil {
		t.Fatalf("execution error: %v", err)
	}

	trail := buildTrail(result)
	tree := reasoning.BuildReasoningTree(trail)

	callIdx := 0
	for _, child := range tree.Children {
		if child.Type != reasoning.EventToolCall {
			continue
		}
		if callIdx >= len(result.StepResults) {
			t.Fatalf("more tree calls than execution steps")
		}
		execName := result.StepResults[callIdx].StepName
		expected := strings.ReplaceAll(execName, ".", " ")
		expected = strings.ReplaceAll(expected, "_", " ")
		if !strings.Contains(child.Summary, expected) {
			t.Errorf("ORDER MISMATCH: tree[%d]=%q vs exec[%d]=%q", callIdx, child.Summary, callIdx, execName)
		}
		callIdx++
	}
}

// DL3: No synthetic (fake) steps inserted
func TestDebugTree_NoSyntheticErrors(t *testing.T) {
	plan := makeAuditPlan("real-step")
	tr := newAuditToolRunner()
	h := harness.NewExecutionHarness(harness.DefaultHarnessConfig(), tr, nil, harness.HarnessDeps{})

	result, err := h.Run(context.Background(), plan, auditEC())
	if err != nil {
		t.Fatalf("execution error: %v", err)
	}

	trail := buildTrail(result)
	tree := reasoning.BuildReasoningTree(trail)

	for _, child := range tree.Children {
		switch child.Type {
		case reasoning.EventToolCall, reasoning.EventDecision:
			// valid top-level
		default:
			t.Errorf("SYNTHETIC: unexpected top-level type %q — %q", child.Type, child.Summary)
		}
		for _, gc := range child.Children {
			if gc.Type != reasoning.EventObservation {
				t.Errorf("SYNTHETIC: unexpected child type %q under %q", gc.Type, child.Summary)
			}
		}
	}
}

// ========================================================================
// PART 4 — Replay Consistency
// ========================================================================

// RP1: Replay → same reasoning
func TestReplay_Deterministic(t *testing.T) {
	plan := makeAuditPlan("tool-x", "tool-y", "tool-z")

	run := func() *reasoning.ReasoningEvent {
		tr := newAuditToolRunner()
		h := harness.NewExecutionHarness(harness.DefaultHarnessConfig(), tr, nil, harness.HarnessDeps{})
		result, err := h.Run(context.Background(), plan, auditEC())
		if err != nil {
			t.Fatalf("execution error: %v", err)
		}
		return reasoning.BuildReasoningTree(buildTrail(result))
	}

	tree1 := run()
	tree2 := run()

	b1, _ := json.Marshal(tree1)
	b2, _ := json.Marshal(tree2)

	if string(b1) != string(b2) {
		t.Errorf("REPLAY: reasoning trees differ\nrun1: %s\nrun2: %s", b1, b2)
	}
}

// RP2: Partial re-execution — single step consistent
func TestReplay_PartialReexecution(t *testing.T) {
	// Full 3-step
	plan1 := makeAuditPlan("step-a", "step-b", "step-c")
	tr1 := newAuditToolRunner()
	h1 := harness.NewExecutionHarness(harness.DefaultHarnessConfig(), tr1, nil, harness.HarnessDeps{})
	full, err := h1.Run(context.Background(), plan1, auditEC())
	if err != nil {
		t.Fatalf("full run error: %v", err)
	}

	// Partial: step-b only
	plan2 := makeAuditPlan("step-b")
	tr2 := newAuditToolRunner()
	h2 := harness.NewExecutionHarness(harness.DefaultHarnessConfig(), tr2, nil, harness.HarnessDeps{})
	partial, err := h2.Run(context.Background(), plan2, auditEC())
	if err != nil {
		t.Fatalf("partial run error: %v", err)
	}

	if full.StepResults[1].StepName != partial.StepResults[0].StepName {
		t.Errorf("name mismatch: full=%q partial=%q", full.StepResults[1].StepName, partial.StepResults[0].StepName)
	}
	if full.StepResults[1].Output != partial.StepResults[0].Output {
		t.Errorf("output mismatch: full=%v partial=%v", full.StepResults[1].Output, partial.StepResults[0].Output)
	}
}

// ========================================================================
// PART 5 — Stress + Edge Cases
// ========================================================================

// SE1: 50 parallel runs — no data races
func TestExecution_ConcurrentDebug(t *testing.T) {
	const numRuns = 50
	errCh := make(chan error, numRuns)

	var wg sync.WaitGroup
	for i := 0; i < numRuns; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			plan := makeAuditPlan(fmt.Sprintf("t%d-a", idx), fmt.Sprintf("t%d-b", idx))
			tr := newAuditToolRunner()
			h := harness.NewExecutionHarness(harness.DefaultHarnessConfig(), tr, nil, harness.HarnessDeps{})
			result, err := h.Run(context.Background(), plan, auditEC())
			if err != nil {
				errCh <- fmt.Errorf("run %d: %w", idx, err)
				return
			}
			if result.StepsExecuted != 2 {
				errCh <- fmt.Errorf("run %d: steps=%d", idx, result.StepsExecuted)
				return
			}
			trail := buildTrail(result)
			tree := reasoning.BuildReasoningTree(trail)
			if tree == nil {
				errCh <- fmt.Errorf("run %d: nil tree", idx)
			}
		}(i)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		t.Errorf("concurrent error: %v", err)
	}
}

// SE2: Large plan — 55 steps
func TestExecution_LargePlan(t *testing.T) {
	const numSteps = 55
	steps := make([]execution.ExecutionStep, numSteps)
	for i := 0; i < numSteps; i++ {
		steps[i] = execution.ExecutionStep{
			Name:      fmt.Sprintf("step-%03d", i),
			Type:      execution.StepTypeTool,
			Arguments: map[string]any{},
		}
	}
	plan := &execution.ExecutionPlan{Steps: steps}
	if err := plan.Validate(); err != nil {
		t.Fatalf("plan validate: %v", err)
	}

	tr := newAuditToolRunner()
	cfg := harness.DefaultHarnessConfig()
	cfg.MaxSteps = 60

	h := harness.NewExecutionHarness(cfg, tr, nil, harness.HarnessDeps{})
	result, err := h.Run(context.Background(), plan, auditEC())
	if err != nil {
		t.Fatalf("large plan error: %v", err)
	}

	if result.StepsExecuted != numSteps {
		t.Errorf("large plan: %d steps, want %d", result.StepsExecuted, numSteps)
	}

	trail := buildTrail(result)
	tree := reasoning.BuildReasoningTree(trail)
	toolCalls := 0
	for _, child := range tree.Children {
		if child.Type == reasoning.EventToolCall {
			toolCalls++
		}
	}
	if toolCalls != numSteps {
		t.Errorf("large plan tree: %d calls, want %d", toolCalls, numSteps)
	}
}

// SE3: Exact timeout boundary — session runtime cuts off mid-plan
func TestExecution_TimeoutBoundary(t *testing.T) {
	cfg := harness.DefaultHarnessConfig()
	cfg.MaxSessionRuntime = 25 * time.Millisecond

	// Each tool takes 20ms — first fits, second won't start in time
	slowTr := &slowAuditRunner{delay: 20 * time.Millisecond}
	plan := makeAuditPlan("slow-a", "slow-b", "slow-c")
	h := harness.NewExecutionHarness(cfg, slowTr, nil, harness.HarnessDeps{})

	ec := execution.NewExecutionContext(context.Background(), "req", "asst", "sess", "tenant", "user")
	ec.StartedAt = time.Now()

	result, _ := h.Run(context.Background(), plan, ec)

	if result.StepsExecuted == 0 {
		t.Error("timeout boundary: no steps executed")
	}
	if result.StepsExecuted == 3 {
		t.Error("timeout boundary: all 3 steps completed despite short session runtime")
	}
	// Must have stopped — not goal_achieved
	if result.StopCondition.Reason == harness.StopReasonGoalAchieved && result.StepsExecuted == 3 {
		t.Error("claimed goal_achieved but all steps ran with tight timeout")
	}
}

// ========================================================================
// PART 6 — Final Report
// ========================================================================

// FR1: Comprehensive audit report
func TestFinalAudit_Report(t *testing.T) {
	type check struct {
		Name   string
		Passed bool
		Detail string
	}
	var checks []check

	// 1. Execution completeness
	plan := makeAuditPlan("a", "b", "c")
	tr := newAuditToolRunner()
	tr.result["a"] = "1"
	tr.result["b"] = "2"
	tr.result["c"] = "3"
	h := harness.NewExecutionHarness(harness.DefaultHarnessConfig(), tr, nil, harness.HarnessDeps{})
	r, err := h.Run(context.Background(), plan, auditEC())
	if err != nil {
		t.Fatalf("execution: %v", err)
	}
	checks = append(checks, check{
		"execution_completeness",
		r.StepsExecuted == 3 && r.StopCondition.Reason == harness.StopReasonGoalAchieved,
		fmt.Sprintf("%d steps, reason=%s", r.StepsExecuted, r.StopCondition.Reason),
	})

	// 2. Reasoning alignment
	trail := buildTrail(r)
	tree := reasoning.BuildReasoningTree(trail)
	tc := 0
	for _, child := range tree.Children {
		if child.Type == reasoning.EventToolCall {
			tc++
		}
	}
	aligned := tc == r.StepsExecuted
	checks = append(checks, check{"reasoning_alignment", aligned,
		fmt.Sprintf("tree=%d exec=%d", tc, r.StepsExecuted)})

	// 3. Determinism (3 runs)
	var hashes [][32]byte
	for i := 0; i < 3; i++ {
		d, _ := json.Marshal(reasoning.BuildReasoningTree(trail))
		hashes = append(hashes, sha256.Sum256(d))
	}
	det := hashes[0] == hashes[1] && hashes[1] == hashes[2]
	checks = append(checks, check{"determinism", det,
		fmt.Sprintf("3-run hash match: %v", det)})

	// 4. No hallucinated steps
	treeIDs := map[string]bool{}
	for _, child := range tree.Children {
		if child.Type == reasoning.EventToolCall {
			treeIDs[child.StepID] = true
		}
	}
	execIDs := map[string]bool{}
	for _, sr := range r.StepResults {
		execIDs[sr.StepID] = true
	}
	noH := true
	for id := range treeIDs {
		if !execIDs[id] {
			noH = false
		}
	}
	checks = append(checks, check{"no_hallucinated_steps", noH,
		fmt.Sprintf("tree=%d exec=%d hallucinated=%v", len(treeIDs), len(execIDs), !noH)})

	// 5. Debug integrity
	integrity := true
	for _, child := range tree.Children {
		if child.Type != reasoning.EventToolCall && child.Type != reasoning.EventDecision {
			integrity = false
		}
	}
	checks = append(checks, check{"debug_integrity", integrity,
		fmt.Sprintf("all node types valid: %v", integrity)})

	// 6. Text rendering
	text := reasoning.RenderReasoningText(tree)
	textOK := strings.Contains(text, "Step 1:") && strings.Contains(text, "Decision:")
	checks = append(checks, check{"text_rendering", textOK,
		fmt.Sprintf("contains steps and decision: %v", textOK)})

	// Report
	passed := 0
	for _, c := range checks {
		if c.Passed {
			passed++
		}
	}
	rate := float64(passed) / float64(len(checks)) * 100

	t.Logf("=== PRODUCTION AUDIT REPORT ===")
	t.Logf("Pass Rate: %.0f%% (%d/%d)", rate, passed, len(checks))
	for _, c := range checks {
		s := "PASS"
		if !c.Passed {
			s = "FAIL"
		}
		t.Logf("  [%s] %s — %s", s, c.Name, c.Detail)
	}
	t.Logf("Determinism Score: %.0f%%", map[bool]float64{true: 100, false: 0}[det])
	t.Logf("Reasoning-Execution Alignment: %s", map[bool]string{true: "100%", false: "MISMATCH"}[aligned])
	t.Logf("=== END REPORT ===")

	if passed != len(checks) {
		t.Errorf("AUDIT FAILED: %d/%d checks passed", passed, len(checks))
	}
}
