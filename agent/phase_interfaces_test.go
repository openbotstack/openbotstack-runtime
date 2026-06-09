package agent

import (
	"context"
	"testing"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	coreagent "github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
	"github.com/openbotstack/openbotstack-runtime/harness"
)

// TestPhaseInterfacesCompile verifies that all phase interfaces are defined
// and the concrete HarnessAgent satisfies them.
func TestPhaseInterfacesCompile(t *testing.T) {
	var _ SkillGatherer = (*HarnessAgent)(nil)
	var _ ContextAssembler = (*HarnessAgent)(nil)
	var _ TaskResolver = (*HarnessAgent)(nil)
	var _ ExecutionContextPreparer = (*HarnessAgent)(nil)
	var _ TaskExecuter = (*HarnessAgent)(nil)
	var _ Finalizer = (*HarnessAgent)(nil)
}

// TestSkillGathererContract verifies the SkillGatherer interface contract.
func TestSkillGathererContract(t *testing.T) {
	var gatherer SkillGatherer = &mockSkillGatherer{
		descs: []aitypes.SkillDescriptor{{ID: "test"}},
	}
	descs, err := gatherer.GatherSkills(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(descs) != 1 || descs[0].ID != "test" {
		t.Errorf("GatherSkills returned %v", descs)
	}
}

// TestContextAssemblerContract verifies the ContextAssembler interface contract.
func TestContextAssemblerContract(t *testing.T) {
	var assembler ContextAssembler = &mockContextAssembler{
		pCtx: &planner.PlannerContext{AssistantID: "test"},
	}
	pCtx, err := assembler.AssembleContext(context.Background(), coreagent.MessageRequest{}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pCtx.AssistantID != "test" {
		t.Errorf("AssembleContext returned AssistantID=%q", pCtx.AssistantID)
	}
}

// TestTaskResolverContract verifies the TaskResolver interface contract.
func TestTaskResolverContract(t *testing.T) {
	var resolver TaskResolver = &mockTaskResolver{
		tasks: []harness.TaskInput{{TaskDescription: "test"}},
	}
	tasks := resolver.ResolveTasks(context.Background(), coreagent.MessageRequest{}, nil)
	if len(tasks) != 1 {
		t.Errorf("ResolveTasks returned %d tasks", len(tasks))
	}
}

// TestFinalizerContract verifies the Finalizer interface contract.
func TestFinalizerContract(t *testing.T) {
	var finalizer Finalizer = &mockFinalizer{}
	finalizer.Finalize(context.Background(), "exec-1", "tenant-1", nil, coreagent.MessageRequest{}, &coreagent.MessageResponse{})
}

// TestExecutionContextPreparerContract verifies the interface contract.
func TestExecutionContextPreparerContract(t *testing.T) {
	var preparer ExecutionContextPreparer = &mockExecutionContextPreparer{
		ec: execution.NewExecutionContext(context.Background(), "req-1", "asst-1", "sess-1", "tenant-1", "user-1"),
	}
	ec := preparer.PrepareExecutionContext(context.Background(), "exec-1", coreagent.MessageRequest{})
	if ec == nil {
		t.Error("PrepareExecutionContext returned nil")
	}
}

// --- Mock implementations for interface contract tests ---

type mockSkillGatherer struct {
	descs []aitypes.SkillDescriptor
	err   error
}

func (m *mockSkillGatherer) GatherSkills(ctx context.Context) ([]aitypes.SkillDescriptor, error) {
	return m.descs, m.err
}

type mockContextAssembler struct {
	pCtx *planner.PlannerContext
	err  error
}

func (m *mockContextAssembler) AssembleContext(ctx context.Context, req coreagent.MessageRequest, descs []aitypes.SkillDescriptor) (*planner.PlannerContext, error) {
	return m.pCtx, m.err
}

type mockTaskResolver struct {
	tasks []harness.TaskInput
}

func (m *mockTaskResolver) ResolveTasks(ctx context.Context, req coreagent.MessageRequest, pCtx *planner.PlannerContext) []harness.TaskInput {
	return m.tasks
}

type mockExecutionContextPreparer struct {
	ec *execution.ExecutionContext
}

func (m *mockExecutionContextPreparer) PrepareExecutionContext(ctx context.Context, execID string, req coreagent.MessageRequest) *execution.ExecutionContext {
	return m.ec
}

type mockTaskExecuter struct {
	result  *harness.HarnessResult
	message string
	skill   string
	err     error
}

func (m *mockTaskExecuter) ExecuteTasks(ctx context.Context, tasks []harness.TaskInput, ec *execution.ExecutionContext) (*harness.HarnessResult, string, string, error) {
	return m.result, m.message, m.skill, m.err
}

type mockFinalizer struct{}

func (m *mockFinalizer) Finalize(ctx context.Context, execID, tenantID string, result *harness.HarnessResult, req coreagent.MessageRequest, resp *coreagent.MessageResponse) {
}
