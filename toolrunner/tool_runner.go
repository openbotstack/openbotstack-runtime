package toolrunner

import (
	"context"

	"github.com/openbotstack/openbotstack-core/execution"
)

// ToolRunner executes external tools.
type ToolRunner interface {
	// Execute runs a tool with the given input.
	Execute(ctx context.Context, name string, input map[string]any, ec *execution.ExecutionContext) (*execution.StepResult, error)
}

// RegistryToolRunner implements ToolRunner by invoking tools from a registry.
type RegistryToolRunner struct {
	client *RegistryClient
}

// NewRegistryToolRunner creates a new tool runner.
func NewRegistryToolRunner(client *RegistryClient) *RegistryToolRunner {
	return &RegistryToolRunner{client: client}
}

// Execute implements ToolRunner.
func (r *RegistryToolRunner) Execute(ctx context.Context, name string, input map[string]any, ec *execution.ExecutionContext) (*execution.StepResult, error) {
	tc := NewToolContext(ctx, ec)
	
	// Delegate to registry client to invoke the tool
	output, err := r.client.Invoke(tc, name, input)
	
	result := &execution.StepResult{
		StepName: name,
		Type:     string(execution.StepTypeTool),
		Output:   output,
		Duration: tc.Duration(),
	}
	
	if err != nil {
		result.Error = err
		return result, err
	}
	
	return result, nil
}
