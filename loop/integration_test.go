package loop_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/assistant"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
	"github.com/openbotstack/openbotstack-runtime/loop"
)

// =============================================================================
// Mocks for Integration
// =============================================================================

type integrationPlanner struct {
	turnLimit int
	turns     int
}

func (p *integrationPlanner) Plan(ctx context.Context, pc *planner.PlannerContext) (*execution.ExecutionPlan, error) {
	if p.turns >= p.turnLimit {
		// reset for the next task
		p.turns = 0
		return &execution.ExecutionPlan{Steps: []execution.ExecutionStep{}}, nil // stop
	}
	p.turns++
	return &execution.ExecutionPlan{
		Steps: []execution.ExecutionStep{
			{Type: execution.StepTypeTool, Name: fmt.Sprintf("tool_%d", p.turns), Arguments: map[string]any{"arg": "val"}},
		},
	}, nil
}

type integrationToolRunner struct {
	calls int
}

func (r *integrationToolRunner) Execute(ctx context.Context, toolID string, parameters map[string]any, ec *execution.ExecutionContext) (*execution.StepResult, error) {
	r.calls++
	return &execution.StepResult{Output: fmt.Sprintf("result_%d", r.calls)}, nil
}

type spyCheckpoint struct {
	savedTaskResults []loop.TaskResult
}

func (c *spyCheckpoint) Save(ctx context.Context, taskIndex int, taskResult *loop.TaskResult, metrics *loop.LoopMetrics) error {
	c.savedTaskResults = append(c.savedTaskResults, *taskResult)
	return nil
}

type spyLogger struct {
	events []execution.ExecutionLogRecord
}

func (l *spyLogger) LogStep(ctx context.Context, record execution.ExecutionLogRecord) error {
	l.events = append(l.events, record)
	return nil
}
func (l *spyLogger) LogPlanStart(ctx context.Context, requestID, assistantID string, plan execution.ExecutionPlan) error {
	return nil
}
func (l *spyLogger) LogPlanEnd(ctx context.Context, requestID, assistantID string, err error) error {
	return nil
}

// =============================================================================
// Integration Tests
// =============================================================================

// TestDualLoop_Integration verifies the entire Dual-Loop architecture works together.
func TestDualLoop_Integration(t *testing.T) {
	// Setup core components
	plannerMock := &integrationPlanner{turnLimit: 2} // 2 turns per task
	toolRunnerMock := &integrationToolRunner{}
	loggerSpy := &spyLogger{}
	compactor := loop.NewDefaultContextCompactor(2)
	checkpointSpy := &spyCheckpoint{}

	// Setup Inner Loop
	innerConfig := loop.DefaultInnerConfig()
	innerLoop := loop.NewDefaultInnerLoop(innerConfig, plannerMock, toolRunnerMock, compactor, loggerSpy)

	// Setup Outer Loop
	outerConfig := loop.DefaultOuterConfig()
	outerLoop := loop.NewDefaultOuterLoop(outerConfig, innerLoop, checkpointSpy, nil, loggerSpy)

	// Runtime Context
	ctx := context.Background()
	ec := execution.NewExecutionContext(ctx, "req-1", "asst-1", "sess-1", "tenant-1", "user-1")

	// Tasks
	tasks := []loop.TaskInput{
		{
			TaskDescription: "task A",
			PlannerContext: &planner.PlannerContext{
				MemoryContext: []assistant.SearchResult{},
			},
		},
		{
			TaskDescription: "task B",
			PlannerContext: &planner.PlannerContext{
				MemoryContext: []assistant.SearchResult{},
			},
		},
	}

	// EXECUTE FULL WORKFLOW
	result, err := outerLoop.Run(ctx, tasks, ec)

	// =========================================================================
	// ASSERTIONS
	// =========================================================================

	if err != nil {
		t.Fatalf("dual loop execution failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected result, got nil")
	}

	// 1. Workflow Stop Condition
	if result.StopCondition.Reason != loop.StopReasonGoalAchieved {
		t.Errorf("expected workflow to finish with GoalAchieved, got %s", result.StopCondition.Reason)
	}

	// 2. Metrics validation
	if result.Metrics.WorkflowSteps != 2 {
		t.Errorf("expected 2 workflow steps, got %d", result.Metrics.WorkflowSteps)
	}
	if result.Metrics.TotalTurns != 6 {
		// Each task: turn 1 (tool) + turn 2 (tool) + turn 3 (stop) = 3 turns
		// Total: 2 tasks * 3 turns = 6 turns
		t.Errorf("expected 6 total turns, got %d", result.Metrics.TotalTurns)
	}
	if result.Metrics.TotalToolCalls != 4 {
		// Total tools: 2 tasks * 2 tools = 4 tools
		t.Errorf("expected 4 total tool calls, got %d", result.Metrics.TotalToolCalls)
	}

	// 3. Task Result Validation
	if len(result.TaskResults) != 2 {
		t.Fatalf("expected 2 task results, got %d", len(result.TaskResults))
	}

	// First Task Details
	task1 := result.TaskResults[0]
	if task1.TurnCount != 3 {
		t.Errorf("expected task 1 to have 3 turns, got %d", task1.TurnCount)
	}
	if task1.StopReason != loop.StopReasonPlannerStopped {
		t.Errorf("expected task 1 to stop due to planner, got %s", task1.StopReason)
	}
	// Context compactor applied: max 2 turns retained out of 3.
	if len(task1.TurnResults) != 2 {
		t.Errorf("expected task 1 to have 2 turn results after compaction, got %d", len(task1.TurnResults))
	}

	// Second Task Details
	task2 := result.TaskResults[1]
	if task2.TurnCount != 3 {
		t.Errorf("expected task 2 to have 3 turns, got %d", task2.TurnCount)
	}
	if task2.StopReason != loop.StopReasonPlannerStopped {
		t.Errorf("expected task 2 to stop due to planner, got %s", task2.StopReason)
	}

	// 4. Checkpoint Validations
	if len(checkpointSpy.savedTaskResults) != 2 {
		t.Errorf("expected checkpoint to be called for 2 tasks, got %d calls", len(checkpointSpy.savedTaskResults))
	}

	// 5. Context Builder Updates
	// The planner context should have accumulated turn results from the tools as string chunks.
	task1MemoryLen := len(tasks[0].PlannerContext.MemoryContext)
	if task1MemoryLen != 2 { // It only appends for turns that HAD observations
		t.Errorf("expected task 1 PlannerContext to have 2 memories appended, got %d", task1MemoryLen)
	}

	// 6. Logging Validations
	var workflowStarts, toolStarts int
	for _, event := range loggerSpy.events {
		if event.StepType == "workflow_step" && event.Status == "running" {
			workflowStarts++
		}
		if event.StepType == "tool" && event.Status == "running" {
			toolStarts++
		}
	}

	if workflowStarts != 2 {
		t.Errorf("expected 2 workflow start logs, got %d", workflowStarts)
	}
	if toolStarts != 4 {
		t.Errorf("expected 4 tool start logs, got %d", toolStarts)
	}
}

// TestDualLoop_Integration_MaxSessionTimeout checks if the outer loop bound takes precedence.
func TestDualLoop_Integration_MaxSessionTimeout(t *testing.T) {
	// Planner never stops voluntarily
	plannerMock := &integrationPlanner{turnLimit: 1}
	
	// Slow tool to trigger timeout quickly
	slowToolRunner := &integrationToolRunner{}

	innerConfig := loop.DefaultInnerConfig()
	innerLoop := loop.NewDefaultInnerLoop(innerConfig, plannerMock, slowToolRunner, &loop.NoOpCompactor{}, &spyLogger{})

	// outerConfig := loop.OuterLoopConfig{
	// 	MaxWorkflowSteps: 5,
	// 	MaxSessionRuntime: 10 * time.Millisecond,
	// }
	// outerLoop := loop.NewDefaultOuterLoop(outerConfig, innerLoop, &loop.NoOpCheckpoint{}, nil, &spyLogger{})

	ctx := context.Background()
	ec := execution.NewExecutionContext(ctx, "req", "asst", "sess", "ten", "user")

	tasks := []loop.TaskInput{
		{TaskDescription: "infinite task 1", PlannerContext: &planner.PlannerContext{}},
		{TaskDescription: "infinite task 2", PlannerContext: &planner.PlannerContext{}},
	}

	// Mocking time natively in this loop is tricky, so we rely on actual wall clock here.
	// We'll fake a long duration by overriding the config to 0 duration after 1 operation
	outerConfig2 := loop.OuterLoopConfig{
		MaxWorkflowSteps: 5,
		MaxSessionRuntime: 0 * time.Millisecond, // triggers instantly
	}
	outerLoop2 := loop.NewDefaultOuterLoop(outerConfig2, innerLoop, &loop.NoOpCheckpoint{}, nil, &spyLogger{})


	res, err := outerLoop2.Run(ctx, tasks, ec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	
	// Loop runs 1 inner task turn successfully and then stops at outer boundary.
	if res.StopCondition.Reason != loop.StopReasonMaxSessionRuntime {
		t.Errorf("expected MaxSessionRuntime, got %s", res.StopCondition.Reason)
	}
}
