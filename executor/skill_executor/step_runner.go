package skill_executor

import (
	"context"
	"fmt"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-runtime/toolrunner"
)

// StepRunner executes a single step from an execution plan.
type StepRunner struct {
	executor *DefaultExecutor
	tools    toolrunner.ToolRunner
}

// NewStepRunner creates a new step runner.
func NewStepRunner(executor *DefaultExecutor, tools toolrunner.ToolRunner) *StepRunner {
	return &StepRunner{
		executor: executor,
		tools:    tools,
	}
}

// RunStep executes one step and records the result in the execution context.
func (sr *StepRunner) RunStep(ctx context.Context, ec *execution.ExecutionContext, step execution.ExecutionStep) (*execution.StepResult, error) {
	start := time.Now()

	var result execution.StepResult
	result.StepName = step.Name
	result.Type = string(step.Type)

	switch step.Type {
	case execution.StepTypeSkill:
		inputBytes, err := step.ArgumentsJSON()
		if err != nil {
			return nil, fmt.Errorf("serialize arguments: %w", err)
		}

		req := execution.ExecutionRequest{
			SkillID:   step.Name,
			Input:     inputBytes,
			TenantID:  ec.TenantID,
			UserID:    ec.UserID,
			RequestID: ec.RequestID,
		}

		execRes, err := sr.executor.Execute(ctx, req)
		if err != nil {
			result.Error = err
		} else if execRes.Status != execution.StatusSuccess {
			result.Error = fmt.Errorf("execution failed with status: %s (error: %s)", execRes.Status, execRes.Error)
		}

		if execRes != nil {
			result.Output = execRes.Output
			result.Duration = execRes.Duration
		}

	case execution.StepTypeTool:
		if sr.tools == nil {
			result.Error = fmt.Errorf("tool runner not configured")
			break
		}
		
		toolRes, err := sr.tools.Execute(ctx, step.Name, step.Arguments, ec)
		if err != nil {
			result.Error = err
		}
		if toolRes != nil {
			result.Output = toolRes.Output
			result.Duration = toolRes.Duration
		}

	default:
		result.Error = fmt.Errorf("unsupported step type: %s", step.Type)
	}

	result.Duration = time.Since(start)
	ec.AddResult(result)

	return &result, result.Error
}
