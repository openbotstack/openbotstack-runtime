package harness

import (
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
)

func TestBuildExecutionTrace_ConvertsStepResults(t *testing.T) {
	result := &HarnessResult{
		PlanID: "plan-1234",
		StepResults: []execution.StepResult{
			{StepID: "s1", StepName: "data.query", Type: "tool", Output: "result1", Duration: 100 * time.Millisecond},
			{StepID: "s2", StepName: "summarize", Type: "skill", Output: "summary", Duration: 200 * time.Millisecond},
		},
		StepInputs: map[string]map[string]any{
			"s1": {"query": "test"},
			"s2": {"text": "input"},
		},
		TurnData: make(map[string][]TurnResult),
		Metrics: HarnessMetrics{
			TotalSteps:     2,
			TotalToolCalls: 2,
			TotalRuntime:   300 * time.Millisecond,
		},
		StopCondition: StopCondition{Stopped: true, Reason: StopReasonGoalAchieved},
		Duration:      300 * time.Millisecond,
	}

	trace := BuildExecutionTrace(result, "exec-abc", "tenant-1")

	if trace.ExecutionID != "exec-abc" {
		t.Errorf("ExecutionID = %q, want %q", trace.ExecutionID, "exec-abc")
	}
	if trace.PlanID != "plan-1234" {
		t.Errorf("PlanID = %q, want %q", trace.PlanID, "plan-1234")
	}
	if trace.TenantID != "tenant-1" {
		t.Errorf("TenantID = %q, want %q", trace.TenantID, "tenant-1")
	}
	if len(trace.Steps) != 2 {
		t.Fatalf("len(Steps) = %d, want 2", len(trace.Steps))
	}
	if trace.Steps[0].StepName != "data.query" {
		t.Errorf("Steps[0].StepName = %q, want %q", trace.Steps[0].StepName, "data.query")
	}
	if trace.Steps[0].Input["query"] != "test" {
		t.Errorf("Steps[0].Input = %v, want query=test", trace.Steps[0].Input)
	}
	if trace.Steps[0].DurationMs != 100 {
		t.Errorf("Steps[0].DurationMs = %d, want 100", trace.Steps[0].DurationMs)
	}
	if trace.Steps[0].Status != "completed" {
		t.Errorf("Steps[0].Status = %q, want completed", trace.Steps[0].Status)
	}
	if trace.Metrics.TotalSteps != 2 {
		t.Errorf("Metrics.TotalSteps = %d, want 2", trace.Metrics.TotalSteps)
	}
	if trace.Metrics.TotalToolCalls != 2 {
		t.Errorf("Metrics.TotalToolCalls = %d, want 2", trace.Metrics.TotalToolCalls)
	}
	if trace.DurationMs != 300 {
		t.Errorf("DurationMs = %d, want 300", trace.DurationMs)
	}
	if trace.StopReason != "goal_achieved" {
		t.Errorf("StopReason = %q, want goal_achieved", trace.StopReason)
	}
}

func TestBuildExecutionTrace_FailedStep(t *testing.T) {
	result := &HarnessResult{
		PlanID: "plan-1",
		StepResults: []execution.StepResult{
			{StepID: "s1", StepName: "fetch", Type: "tool", Error: errSome("connection refused"), Duration: 50 * time.Millisecond},
		},
		StepInputs: map[string]map[string]any{},
		TurnData:   make(map[string][]TurnResult),
		Metrics:    HarnessMetrics{TotalSteps: 1},
		Duration:   50 * time.Millisecond,
	}

	trace := BuildExecutionTrace(result, "exec-fail", "t1")

	if trace.Steps[0].Status != "failed" {
		t.Errorf("Status = %q, want failed", trace.Steps[0].Status)
	}
	if trace.Steps[0].Error != "connection refused" {
		t.Errorf("Error = %q, want connection refused", trace.Steps[0].Error)
	}
}

func TestBuildExecutionTrace_LLMStepWithTurns(t *testing.T) {
	turnResults := []TurnResult{
		{
			TurnNumber: 1,
			PlanText:   "I need to query the database",
			Actions: []TurnAction{
				{StepName: "db.query", StepType: "tool", Input: map[string]any{"sql": "SELECT 1"}, Output: "ok", DurationMs: 30},
			},
			StopReason: StopReasonGoalAchieved,
			Duration:   100 * time.Millisecond,
		},
		{
			TurnNumber: 2,
			PlanText:   "The query succeeded",
			Actions:    []TurnAction{},
			StopReason: StopReasonMaxTurns,
			Duration:   50 * time.Millisecond,
		},
	}

	result := &HarnessResult{
		PlanID: "plan-1",
		StepResults: []execution.StepResult{
			{StepID: "s1", StepName: "reason", Type: "llm", Output: "final answer", Duration: 150 * time.Millisecond},
		},
		StepInputs: map[string]map[string]any{},
		TurnData: map[string][]TurnResult{
			"s1": turnResults,
		},
		Metrics: HarnessMetrics{TotalSteps: 1, TotalLLMTurns: 2},
		Duration: 150 * time.Millisecond,
	}

	trace := BuildExecutionTrace(result, "exec-llm", "t1")

	if len(trace.Steps) != 1 {
		t.Fatalf("len(Steps) = %d, want 1", len(trace.Steps))
	}
	step := trace.Steps[0]
	if step.StepType != "llm" {
		t.Errorf("StepType = %q, want llm", step.StepType)
	}
	if len(step.Turns) != 2 {
		t.Fatalf("len(Turns) = %d, want 2", len(step.Turns))
	}
	if step.Turns[0].PlanText != "I need to query the database" {
		t.Errorf("Turns[0].PlanText = %q", step.Turns[0].PlanText)
	}
	if len(step.Turns[0].Actions) != 1 {
		t.Errorf("Turns[0].Actions len = %d, want 1", len(step.Turns[0].Actions))
	}
	if step.Turns[0].Actions[0].StepName != "db.query" {
		t.Errorf("Actions[0].StepName = %q", step.Turns[0].Actions[0].StepName)
	}
	if step.Turns[0].Actions[0].DurationMs != 30 {
		t.Errorf("Actions[0].DurationMs = %d, want 30", step.Turns[0].Actions[0].DurationMs)
	}
	if step.Turns[1].StopReason != "max_turns" {
		t.Errorf("Turns[1].StopReason = %q, want max_turns", step.Turns[1].StopReason)
	}
}

type errTest string

func (e errTest) Error() string { return string(e) }

func errSome(s string) error { return errTest(s) }
