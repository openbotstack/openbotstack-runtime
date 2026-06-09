package agent

import (
	"context"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
	"github.com/openbotstack/openbotstack-runtime/harness"

	coreagent "github.com/openbotstack/openbotstack-core/control/agent"
)

// Phase interfaces decompose the HarnessAgent's HandleMessage pipeline
// into independently testable stages. Each interface has a single method
// following the single-responsibility principle.

// SkillGatherer collects available skill/tool descriptors.
type SkillGatherer interface {
	GatherSkills(ctx context.Context) ([]aitypes.SkillDescriptor, error)
}

// ContextAssembler builds the PlannerContext from request and skill descriptors.
type ContextAssembler interface {
	AssembleContext(ctx context.Context, req coreagent.MessageRequest, descs []aitypes.SkillDescriptor) (*planner.PlannerContext, error)
}

// TaskResolver determines execution tasks via workflow decomposition.
type TaskResolver interface {
	ResolveTasks(ctx context.Context, req coreagent.MessageRequest, pCtx *planner.PlannerContext) []harness.TaskInput
}

// ExecutionContextPreparer builds the ExecutionContext for a request.
type ExecutionContextPreparer interface {
	PrepareExecutionContext(ctx context.Context, execID string, req coreagent.MessageRequest) *execution.ExecutionContext
}

// TaskExecuter runs plan+execute for each task.
type TaskExecuter interface {
	ExecuteTasks(ctx context.Context, tasks []harness.TaskInput, ec *execution.ExecutionContext) (*harness.HarnessResult, string, string, error)
}

// Finalizer stores reasoning traces and conversation messages.
type Finalizer interface {
	Finalize(ctx context.Context, execID, tenantID string, result *harness.HarnessResult, req coreagent.MessageRequest, resp *coreagent.MessageResponse)
}
