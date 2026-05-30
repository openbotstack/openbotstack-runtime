package harness

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"log/slog"

	"github.com/openbotstack/openbotstack-core/execution"
	builtintools "github.com/openbotstack/openbotstack-runtime/tools/builtin"
	"github.com/openbotstack/openbotstack-runtime/toolrunner"
)

// StepExecutor handles execution of individual tool and skill steps.
// Extracted from the InnerLoop ACT phase into a standalone, testable unit.
type StepExecutor struct {
	toolRunner    toolrunner.ToolRunner
	mcpRunner     toolrunner.ToolRunner
	skillExecutor execution.SkillExecutor
	builtinRunner *builtintools.BuiltinToolRunner
}

// StepExecutorDeps captures optional runners.
type StepExecutorDeps struct {
	MCPRunner     toolrunner.ToolRunner
	BuiltinRunner *builtintools.BuiltinToolRunner
}

// NewStepExecutor creates a step executor.
func NewStepExecutor(toolRunner toolrunner.ToolRunner, skillExecutor execution.SkillExecutor, deps StepExecutorDeps) *StepExecutor {
	return &StepExecutor{
		toolRunner:    toolRunner,
		skillExecutor: skillExecutor,
		mcpRunner:     deps.MCPRunner,
		builtinRunner: deps.BuiltinRunner,
	}
}

// routedRunner identifies which runner handles a step based on name prefix.
type routedRunner int

const (
	runnerDefault routedRunner = iota
	runnerMCP
	runnerBuiltin
)

// Execute dispatches a single step to the correct runner based on step type and name prefix.
// This is the unified entry point that replaces direct calls to ExecuteTool/ExecuteSkill.
// It clones arguments before mutation, coerces string numbers, resolves templates, and
// routes to the appropriate runner.
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

// route determines which runner handles the step based on name prefix.
func (se *StepExecutor) route(stepName string) routedRunner {
	if strings.HasPrefix(stepName, "builtin.") && se.builtinRunner != nil {
		return runnerBuiltin
	}
	if strings.HasPrefix(stepName, "mcp.") && se.mcpRunner != nil {
		return runnerMCP
	}
	return runnerDefault
}

// ExecuteTool runs a single tool step.
func (se *StepExecutor) ExecuteTool(
	ctx context.Context,
	step *execution.ExecutionStep,
	ec *execution.ExecutionContext,
	prevResults map[string]any,
	stepTimeout time.Duration,
) (*execution.StepResult, error) {
	// Clone arguments before mutation to preserve the frozen plan's original values.
	// CoerceStringNumbers and ResolveArguments are template-resolution operations that
	// must not mutate the plan's canonical Arguments map.
	step = step.Clone()
	if n := step.CoerceStringNumbers(); n > 0 {
		slog.DebugContext(ctx, "step_executor: coerced argument types", "step", step.Name, "count", n)
	}
	step.ResolveArguments(prevResults)

	start := time.Now()

	switch se.route(step.Name) {
	case runnerBuiltin:
		// Permissions are gated by the ExecutionContext. When GrantedPermissions is set
		// (populated by the control plane from config/OBS_FILE_ALLOWED_DIRS), tools with
		// required permissions (read_file, write_file, web_fetch) are validated at the
		// runner level as defense-in-depth below the plan-level PermissionChecker.
		var result map[string]any
		var err error
		if ec != nil && len(ec.GrantedPermissions) > 0 {
			result, err = se.builtinRunner.RunWithPermissions(ctx, step.Name, step.Arguments, ec.GrantedPermissions)
		} else {
			result, err = se.builtinRunner.Run(ctx, step.Name, step.Arguments)
		}
		duration := time.Since(start)
		if err != nil {
			return &execution.StepResult{
				StepID:   step.StepID,
				StepName: step.Name,
				Type:     string(step.Type),
				Error:    err,
				Duration: duration,
			}, err
		}
		return &execution.StepResult{
			StepID:   step.StepID,
			StepName: step.Name,
			Type:     string(step.Type),
			Output:   result,
			Duration: duration,
		}, nil

	case runnerMCP:
		return se.executeWithToolRunner(ctx, se.mcpRunner, step, ec, stepTimeout, start)

	default:
		if se.toolRunner == nil {
			return &execution.StepResult{
				StepID:   step.StepID,
				StepName: step.Name,
				Type:     string(step.Type),
				Error:    fmt.Errorf("no tool runner configured"),
				Duration: time.Since(start),
			}, fmt.Errorf("tool step %q skipped: no tool runner configured", step.Name)
		}
		return se.executeWithToolRunner(ctx, se.toolRunner, step, ec, stepTimeout, start)
	}
}

// executeWithToolRunner handles the common pattern of running a tool via a ToolRunner
// interface with timeout context management and result mapping.
func (se *StepExecutor) executeWithToolRunner(
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
				StepID:   step.StepID,
				StepName: step.Name,
				Type:     string(step.Type),
				Error:    ctx.Err(),
				Duration: duration,
			}, ctx.Err()
		}
		return &execution.StepResult{
			StepID:   step.StepID,
			StepName: step.Name,
			Type:     string(step.Type),
			Error:    err,
			Duration: duration,
		}, err
	}

	var output any
	if toolRes != nil {
		output = toolRes.Output
	}

	return &execution.StepResult{
		StepID:   step.StepID,
		StepName: step.Name,
		Type:     string(step.Type),
		Output:   output,
		Duration: duration,
	}, nil
}

// ExecuteSkill runs a single skill step.
// If the step name has a recognized prefix ("mcp." or "builtin.") and the
// corresponding runner is configured, it is routed to ExecuteTool regardless
// of step type.
func (se *StepExecutor) ExecuteSkill(
	ctx context.Context,
	step *execution.ExecutionStep,
	ec *execution.ExecutionContext,
	prevResults map[string]any,
	stepTimeout time.Duration,
) (*execution.StepResult, error) {
	switch se.route(step.Name) {
	case runnerBuiltin, runnerMCP:
		return se.ExecuteTool(ctx, step, ec, prevResults, stepTimeout)
	default:
	}

	// Clone arguments before mutation (same rationale as ExecuteTool).
	step = step.Clone()
	if n := step.CoerceStringNumbers(); n > 0 {
		slog.DebugContext(ctx, "step_executor: coerced argument types", "step", step.Name, "count", n)
	}
	step.ResolveArguments(prevResults)

	start := time.Now()

	if se.skillExecutor == nil {
		return &execution.StepResult{
			StepID:   step.StepID,
			StepName: step.Name,
			Type:     string(step.Type),
			Error:    fmt.Errorf("no skill executor configured"),
			Duration: time.Since(start),
		}, fmt.Errorf("skill step %q skipped: no skill executor configured", step.Name)
	}

	inputBytes, err := step.ArgumentsJSON()
	if err != nil {
		return &execution.StepResult{
			StepID:   step.StepID,
			StepName: step.Name,
			Type:     string(step.Type),
			Error:    fmt.Errorf("serialize arguments: %w", err),
			Duration: time.Since(start),
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

	// Wire streaming token callback for declarative (LLM-based) skills
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
				StepID:   step.StepID,
				StepName: step.Name,
				Type:     string(step.Type),
				Error:    ctx.Err(),
				Duration: duration,
			}, ctx.Err()
		}
		return &execution.StepResult{
			StepID:   step.StepID,
			StepName: step.Name,
			Type:     string(step.Type),
			Error:    err,
			Duration: duration,
		}, err
	}

	var output any
	if result != nil {
		output = string(result.Output)
	}

	return &execution.StepResult{
		StepID:   step.StepID,
		StepName: step.Name,
		Type:     string(step.Type),
		Output:   output,
		Duration: duration,
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
// This is a convenience function for result passing between steps.
func ArgumentsToMap(result *execution.StepResult) map[string]any {
	if result == nil {
		return nil
	}
	// Try to parse output as JSON if it's a string
	if str, ok := result.Output.(string); ok && str != "" {
		var m map[string]any
		if err := json.Unmarshal([]byte(str), &m); err == nil {
			return m
		}
	}
	return map[string]any{result.StepName: result.Output}
}
