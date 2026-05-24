package harness

import (
	"context"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
)

type mockMCPToolRunner struct {
	called bool
	name   string
}

func (m *mockMCPToolRunner) Execute(_ context.Context, name string, _ map[string]any, _ *execution.ExecutionContext) (*execution.StepResult, error) {
	m.called = true
	m.name = name
	return &execution.StepResult{StepName: name, Type: "tool", Output: "mcp result"}, nil
}

func TestStepExecutor_MCPRouting(t *testing.T) {
	mcpRunner := &mockMCPToolRunner{}
	se := NewStepExecutor(nil, nil, StepExecutorDeps{})
	se.SetMCPRunner(mcpRunner)

	step := &execution.ExecutionStep{
		StepID:    "step-1",
		Name:      "mcp.server1.search",
		Type:      execution.StepTypeTool,
		Arguments: map[string]any{"query": "test"},
	}
	ec := &execution.ExecutionContext{RequestID: "req-1"}

	result, err := se.ExecuteTool(context.Background(), step, ec, nil, time.Second)
	if err != nil {
		t.Fatalf("ExecuteTool: %v", err)
	}
	if !mcpRunner.called {
		t.Error("MCP runner not called")
	}
	if mcpRunner.name != "mcp.server1.search" {
		t.Errorf("tool name = %q", mcpRunner.name)
	}
	if result.Output != "mcp result" {
		t.Errorf("output = %v", result.Output)
	}
}

func TestStepExecutor_NonMCPUseDefaultRunner(t *testing.T) {
	mcpRunner := &mockMCPToolRunner{}
	se := NewStepExecutor(nil, nil, StepExecutorDeps{})
	se.SetMCPRunner(mcpRunner)

	step := &execution.ExecutionStep{
		StepID:    "step-1",
		Name:      "regular_tool",
		Type:      execution.StepTypeTool,
		Arguments: map[string]any{},
	}
	ec := &execution.ExecutionContext{RequestID: "req-1"}

	_, err := se.ExecuteTool(context.Background(), step, ec, nil, time.Second)
	// Should use default runner (which is nil), so should error
	if err == nil {
		t.Error("expected error with nil default runner")
	}
	if mcpRunner.called {
		t.Error("MCP runner should not be called for non-mcp tools")
	}
}
