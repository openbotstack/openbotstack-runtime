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
