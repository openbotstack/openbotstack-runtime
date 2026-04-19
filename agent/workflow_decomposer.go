package agent

import (
	"fmt"
	"time"

	"github.com/openbotstack/openbotstack-core/assistant"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
	"github.com/openbotstack/openbotstack-runtime/loop"
)

// Workflow describes a multi-step process. This is a local mirror of
// workflows.Workflow to avoid a direct dependency on the apps repository.
// Callers in main.go can adapt workflows.Workflow to this interface.
type Workflow interface {
	ID() string
	Name() string
	Steps(input map[string]any) ([]execution.ExecutionStep, error)
	Timeout() time.Duration
}

// WorkflowResolver matches user messages to registered workflows.
type WorkflowResolver interface {
	// Resolve tries to match a user message to a registered workflow.
	// Returns nil workflow if no match (caller should use single-task path).
	Resolve(message string) (Workflow, map[string]any, error)
}

// DecomposeToTasks converts a Workflow into a slice of TaskInput for the outer loop.
// Each workflow step becomes a separate task with its own PlannerContext.
func DecomposeToTasks(w Workflow, input map[string]any, baseCtx *planner.PlannerContext) ([]loop.TaskInput, error) {
	steps, err := w.Steps(input)
	if err != nil {
		return nil, fmt.Errorf("workflow %s steps failed: %w", w.ID(), err)
	}
	if len(steps) == 0 {
		return nil, fmt.Errorf("workflow %s produced zero steps", w.ID())
	}

	tasks := make([]loop.TaskInput, len(steps))
	for i, step := range steps {
		tasks[i] = loop.TaskInput{
			TaskDescription: fmt.Sprintf("Execute %s '%s' (workflow: %s)", step.Type, step.Name, w.Name()),
			PlannerContext:  deepCopyPlannerContext(baseCtx, step),
		}
	}
	return tasks, nil
}

// deepCopyPlannerContext creates an independent PlannerContext for a single task.
// MemoryContext is deep-copied to prevent cross-task pollution when inner loops
// append observations.
func deepCopyPlannerContext(base *planner.PlannerContext, step execution.ExecutionStep) *planner.PlannerContext {
	memCopy := make([]assistant.SearchResult, len(base.MemoryContext))
	copy(memCopy, base.MemoryContext)

	return &planner.PlannerContext{
		AssistantID:   base.AssistantID,
		Soul:          base.Soul,
		MemoryContext: memCopy,
		Skills:        base.Skills, // read-only, shallow copy is safe
		UserRequest:   fmt.Sprintf("Execute %s '%s' with args: %v", step.Type, step.Name, step.Arguments),
	}
}
