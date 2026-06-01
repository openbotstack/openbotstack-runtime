package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"log/slog"

	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-runtime/toolrunner"
)

// StepExecutor handles execution of individual tool and skill steps.
type StepExecutor struct {
	toolRunner    toolrunner.ToolRunner
	skillExecutor execution.SkillExecutor
	runners       map[string]toolrunner.ToolRunner // prefix → runner
}

// StepExecutorDeps captures optional runners.
type StepExecutorDeps struct {
	MCPRunner     toolrunner.ToolRunner
	BuiltinRunner toolrunner.ToolRunner
}

// NewStepExecutor creates a step executor.
func NewStepExecutor(toolRunner toolrunner.ToolRunner, skillExecutor execution.SkillExecutor, deps StepExecutorDeps) *StepExecutor {
	runners := make(map[string]toolrunner.ToolRunner)
	if deps.MCPRunner != nil {
		runners["mcp."] = deps.MCPRunner
	}
	if deps.BuiltinRunner != nil {
		runners["builtin."] = deps.BuiltinRunner
	}
	return &StepExecutor{
		toolRunner:    toolRunner,
		skillExecutor: skillExecutor,
		runners:       runners,
	}
}

// Execute dispatches a single step to the correct runner based on step type and name prefix.
func (se *StepExecutor) Execute(
	ctx context.Context,
	step *execution.ExecutionStep,
	ec *execution.ExecutionContext,
	prevResults map[string]any,
	stepTimeout time.Duration,
) (*execution.StepResult, error) {
	switch step.Type {
	case execution.StepTypeTool:
		return se.ExecuteTool(ctx, step, ec, prevResults, stepTimeout)
	case execution.StepTypeSkill:
		return se.ExecuteSkill(ctx, step, ec, prevResults, stepTimeout)
	default:
		return &execution.StepResult{
			StepID:   step.StepID,
			StepName: step.Name,
			Type:     string(step.Type),
			Error:    fmt.Errorf("unknown step type: %s", step.Type),
		}, fmt.Errorf("unknown step type: %s", step.Type)
	}
}

// lookupRunner finds a runner by step name prefix.
func (se *StepExecutor) lookupRunner(stepName string) toolrunner.ToolRunner {
	for prefix, runner := range se.runners {
		if strings.HasPrefix(stepName, prefix) {
			return runner
		}
	}
	return nil
}

// ExecuteTool runs a single tool step.
func (se *StepExecutor) ExecuteTool(
	ctx context.Context,
	step *execution.ExecutionStep,
	ec *execution.ExecutionContext,
	prevResults map[string]any,
	stepTimeout time.Duration,
) (*execution.StepResult, error) {
	step = step.Clone()
	if n := step.CoerceStringNumbers(); n > 0 {
		slog.DebugContext(ctx, "step_executor: coerced argument types", "step", step.Name, "count", n)
	}
	step.ResolveArguments(prevResults)

	start := time.Now()

	// Route to prefix-matched runner first, then default toolRunner.
	if runner := se.lookupRunner(step.Name); runner != nil {
		return se.executeWithRunner(ctx, runner, step, ec, stepTimeout, start)
	}

	if se.toolRunner == nil {
		return &execution.StepResult{
			StepID:   step.StepID,
			StepName: step.Name,
			Type:     string(step.Type),
			Error:    fmt.Errorf("no tool runner configured"),
			Duration: time.Since(start),
		}, fmt.Errorf("tool step %q skipped: no tool runner configured", step.Name)
	}
	return se.executeWithRunner(ctx, se.toolRunner, step, ec, stepTimeout, start)
}

// executeWithRunner handles tool execution via any ToolRunner with timeout management.
func (se *StepExecutor) executeWithRunner(
	ctx context.Context,
	runner toolrunner.ToolRunner,
	step *execution.ExecutionStep,
	ec *execution.ExecutionContext,
	stepTimeout time.Duration,
	start time.Time,
) (*execution.StepResult, error) {
	var stepCtx context.Context
	var cancel context.CancelFunc
	if stepTimeout > 0 {
		stepCtx, cancel = context.WithTimeout(ctx, stepTimeout)
	} else {
		stepCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	toolRes, err := runner.Execute(stepCtx, step.Name, step.Arguments, ec)
	duration := time.Since(start)

	if err != nil {
		if ctx.Err() != nil {
			return &execution.StepResult{
				StepID: step.StepID, StepName: step.Name, Type: string(step.Type),
				Error: ctx.Err(), Duration: duration,
			}, ctx.Err()
		}
		return &execution.StepResult{
			StepID: step.StepID, StepName: step.Name, Type: string(step.Type),
			Error: err, Duration: duration,
		}, err
	}

	var output any
	if toolRes != nil {
		output = toolRes.Output
	}

	return &execution.StepResult{
		StepID: step.StepID, StepName: step.Name, Type: string(step.Type),
		Output: output, Duration: duration,
	}, nil
}

// ExecuteSkill runs a single skill step.
func (se *StepExecutor) ExecuteSkill(
	ctx context.Context,
	step *execution.ExecutionStep,
	ec *execution.ExecutionContext,
	prevResults map[string]any,
	stepTimeout time.Duration,
) (*execution.StepResult, error) {
	// Prefix-routed steps (mcp.*, builtin.*) are handled as tool steps.
	if se.lookupRunner(step.Name) != nil {
		return se.ExecuteTool(ctx, step, ec, prevResults, stepTimeout)
	}

	step = step.Clone()
	if n := step.CoerceStringNumbers(); n > 0 {
		slog.DebugContext(ctx, "step_executor: coerced argument types", "step", step.Name, "count", n)
	}
	step.ResolveArguments(prevResults)

	start := time.Now()

	if se.skillExecutor == nil {
		return &execution.StepResult{
			StepID: step.StepID, StepName: step.Name, Type: string(step.Type),
			Error: fmt.Errorf("no skill executor configured"), Duration: time.Since(start),
		}, fmt.Errorf("skill step %q skipped: no skill executor configured", step.Name)
	}

	inputBytes, err := step.ArgumentsJSON()
	if err != nil {
		return &execution.StepResult{
			StepID: step.StepID, StepName: step.Name, Type: string(step.Type),
			Error: fmt.Errorf("serialize arguments: %w", err), Duration: time.Since(start),
		}, fmt.Errorf("skill step %q: serialize arguments: %w", step.Name, err)
	}

	timeout := stepTimeout
	if step.Timeout > 0 {
		timeout = time.Duration(step.Timeout) * time.Millisecond
	}

	req := execution.ExecutionRequest{
		SkillID:   step.Name,
		Input:     inputBytes,
		Timeout:   timeout,
		TenantID:  ec.TenantID,
		UserID:    ec.UserID,
		RequestID: ec.RequestID,
	}

	if ec.ProgressFn != nil {
		req.TokenFn = func(token string) {
			ec.ProgressFn("token", token, 0, "")
		}
	}

	result, err := se.skillExecutor.Execute(ctx, req)
	duration := time.Since(start)

	if err != nil {
		if ctx.Err() != nil {
			return &execution.StepResult{
				StepID: step.StepID, StepName: step.Name, Type: string(step.Type),
				Error: ctx.Err(), Duration: duration,
			}, ctx.Err()
		}
		return &execution.StepResult{
			StepID: step.StepID, StepName: step.Name, Type: string(step.Type),
			Error: err, Duration: duration,
		}, err
	}

	var output any
	if result != nil {
		output = string(result.Output)
	}

	return &execution.StepResult{
		StepID: step.StepID, StepName: step.Name, Type: string(step.Type),
		Output: output, Duration: duration,
	}, nil
}

// ExecuteFallback runs a fallback tool after retries are exhausted.
func (se *StepExecutor) ExecuteFallback(
	ctx context.Context,
	fallbackTool string,
	arguments map[string]any,
	ec *execution.ExecutionContext,
	prevResults map[string]any,
) (*execution.StepResult, error) {
	fallbackStep := &execution.ExecutionStep{
		StepID:    "fallback-" + fallbackTool,
		Name:      fallbackTool,
		Type:      execution.StepTypeTool,
		Arguments: arguments,
	}
	return se.ExecuteTool(ctx, fallbackStep, ec, prevResults, 0)
}

// ArgumentsToMap converts step arguments to a map for template resolution.
func ArgumentsToMap(result *execution.StepResult) map[string]any {
	if result == nil {
		return nil
	}
	if str, ok := result.Output.(string); ok && str != "" {
		var m map[string]any
		if err := json.Unmarshal([]byte(str), &m); err == nil {
			return m
		}
	}
	return map[string]any{result.StepName: result.Output}
}
