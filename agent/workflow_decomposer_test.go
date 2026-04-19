package agent

import (
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/assistant"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
)

func TestDecomposeToTasks_EmptySteps(t *testing.T) {
	wf := &stubWorkflow{steps: nil}
	baseCtx := &planner.PlannerContext{
		AssistantID: "asst1",
		Skills:      []planner.SkillDescriptor{{ID: "s1", Name: "S1"}},
	}

	_, err := DecomposeToTasks(wf, map[string]any{"msg": "hi"}, baseCtx)
	if err == nil {
		t.Fatal("expected error for empty steps")
	}
}

func TestDecomposeToTasks_SingleStep(t *testing.T) {
	wf := &stubWorkflow{
		steps: []execution.ExecutionStep{
			{Name: "core/skill1", Type: execution.StepTypeSkill, Arguments: map[string]any{"x": 1}},
		},
	}
	baseCtx := &planner.PlannerContext{
		AssistantID:   "asst1",
		MemoryContext: []assistant.SearchResult{{Content: []byte("mem1"), Score: 0.9}},
		Skills:        []planner.SkillDescriptor{{ID: "s1"}},
		UserRequest:   "original request",
	}

	tasks, err := DecomposeToTasks(wf, map[string]any{}, baseCtx)
	if err != nil {
		t.Fatalf("DecomposeToTasks failed: %v", err)
	}

	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	if tasks[0].TaskDescription == "" {
		t.Error("TaskDescription should not be empty")
	}
	if tasks[0].PlannerContext == nil {
		t.Fatal("PlannerContext should not be nil")
	}
	if tasks[0].PlannerContext.AssistantID != "asst1" {
		t.Errorf("AssistantID = %q, want %q", tasks[0].PlannerContext.AssistantID, "asst1")
	}
}

func TestDecomposeToTasks_MultipleSteps(t *testing.T) {
	wf := &stubWorkflow{
		steps: []execution.ExecutionStep{
			{Name: "tool-a", Type: execution.StepTypeTool, Arguments: map[string]any{}},
			{Name: "skill-b", Type: execution.StepTypeSkill, Arguments: map[string]any{}},
		},
	}
	baseCtx := &planner.PlannerContext{
		AssistantID:   "asst1",
		MemoryContext: []assistant.SearchResult{{Content: []byte("mem1"), Score: 0.9}},
	}

	tasks, err := DecomposeToTasks(wf, map[string]any{}, baseCtx)
	if err != nil {
		t.Fatalf("DecomposeToTasks failed: %v", err)
	}

	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
}

func TestDecomposeToTasks_MemoryContextIsolation(t *testing.T) {
	wf := &stubWorkflow{
		steps: []execution.ExecutionStep{
			{Name: "s1", Type: execution.StepTypeSkill, Arguments: map[string]any{}},
			{Name: "s2", Type: execution.StepTypeSkill, Arguments: map[string]any{}},
		},
	}
	baseCtx := &planner.PlannerContext{
		AssistantID:   "asst1",
		MemoryContext: []assistant.SearchResult{{Content: []byte("base")}},
	}

	tasks, err := DecomposeToTasks(wf, map[string]any{}, baseCtx)
	if err != nil {
		t.Fatalf("DecomposeToTasks failed: %v", err)
	}

	// Verify deep copy: modifying one task's MemoryContext should not affect others
	tasks[0].PlannerContext.MemoryContext = append(tasks[0].PlannerContext.MemoryContext, assistant.SearchResult{Content: []byte("polluted")})

	if len(tasks[1].PlannerContext.MemoryContext) != 1 {
		t.Errorf("task 2 MemoryContext len = %d, want 1 (not polluted)", len(tasks[1].PlannerContext.MemoryContext))
	}
}

func TestDecomposeToTasks_SkillsPreserved(t *testing.T) {
	wf := &stubWorkflow{
		steps: []execution.ExecutionStep{
			{Name: "s1", Type: execution.StepTypeSkill, Arguments: map[string]any{}},
		},
	}
	baseCtx := &planner.PlannerContext{
		Skills: []planner.SkillDescriptor{
			{ID: "skill-1", Name: "Skill 1"},
			{ID: "skill-2", Name: "Skill 2"},
		},
	}

	tasks, err := DecomposeToTasks(wf, map[string]any{}, baseCtx)
	if err != nil {
		t.Fatalf("DecomposeToTasks failed: %v", err)
	}

	if len(tasks[0].PlannerContext.Skills) != 2 {
		t.Errorf("Skills len = %d, want 2", len(tasks[0].PlannerContext.Skills))
	}
}

// stubWorkflow implements Workflow for testing.
type stubWorkflow struct {
	steps []execution.ExecutionStep
}

func (s *stubWorkflow) ID() string                            { return "test-workflow" }
func (s *stubWorkflow) Name() string                          { return "Test Workflow" }
func (s *stubWorkflow) Steps(input map[string]any) ([]execution.ExecutionStep, error) {
	return s.steps, nil
}
func (s *stubWorkflow) Timeout() time.Duration { return 30 * time.Second }
