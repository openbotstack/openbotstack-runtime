package harness

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-runtime/toolrunner"
)

// Compile-time interface compliance checks.
var _ toolrunner.StepDispatcher = (*StepExecutor)(nil)

// StepExecutor handles execution of individual tool and skill steps.
// It implements toolrunner.StepDispatcher and uses a StepHandler registry
// for pluggable step routing instead of string prefix matching.
type StepExecutor struct {
	toolRunner    toolrunner.ToolRunner
	skillExecutor execution.SkillExecutor
	handlers      []toolrunner.StepHandler // ordered list; first CanHandle wins
}

// StepExecutorDeps captures optional handlers/runners.
// For backwards compatibility, MCPRunner and BuiltinRunner are converted
// to prefix-based handlers internally.
type StepExecutorDeps struct {
	MCPRunner     toolrunner.ToolRunner
	BuiltinRunner toolrunner.ToolRunner
	// ExtraHandlers allows registering custom StepHandlers beyond the
	// default MCP and builtin prefix handlers.
	ExtraHandlers []toolrunner.StepHandler
}

// NewStepExecutor creates a step executor with prefix-based handlers derived
// from the legacy MCPRunner/BuiltinRunner deps.
func NewStepExecutor(toolRunner toolrunner.ToolRunner, skillExecutor execution.SkillExecutor, deps StepExecutorDeps) *StepExecutor {
	var handlers []toolrunner.StepHandler
	if deps.MCPRunner != nil {
		handlers = append(handlers, &prefixHandler{prefix: "mcp.", runner: deps.MCPRunner})
	}
	if deps.BuiltinRunner != nil {
		handlers = append(handlers, &prefixHandler{prefix: "builtin.", runner: deps.BuiltinRunner})
	}
	handlers = append(handlers, deps.ExtraHandlers...)
	return &StepExecutor{
		toolRunner:    toolRunner,
		skillExecutor: skillExecutor,
		handlers:      handlers,
	}
}

// Dispatch implements toolrunner.StepDispatcher.
func (se *StepExecutor) Dispatch(
	ctx context.Context,
	step *execution.ExecutionStep,
	ec *execution.ExecutionContext,
	prevResults map[string]any,
	stepTimeout time.Duration,
) (*execution.StepResult, error) {
	return se.Execute(ctx, step, ec, prevResults, stepTimeout)
}

// DispatchFallback implements toolrunner.StepDispatcher.
func (se *StepExecutor) DispatchFallback(
	ctx context.Context,
	fallbackTool string,
	arguments map[string]any,
	ec *execution.ExecutionContext,
	prevResults map[string]any,
) (*execution.StepResult, error) {
	return se.ExecuteFallback(ctx, fallbackTool, arguments, ec, prevResults)
}

// Execute dispatches a single step to the correct runner based on step type and handler registry.
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

// lookupHandler finds the first handler that can handle the given step.
func (se *StepExecutor) lookupHandler(step *execution.ExecutionStep) toolrunner.StepHandler {
	for _, h := range se.handlers {
		if h.CanHandle(step) {
			return h
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
	cloned, err := step.Prepare(prevResults)
	if err != nil {
		return nil, err
	}

	start := time.Now()

	// Route to registered handler first (builtin.*, mcp.*).
	if handler := se.lookupHandler(cloned); handler != nil {
		return handler.Handle(ctx, cloned, ec, prevResults, stepTimeout)
	}

	// Try the default toolRunner.
	if se.toolRunner != nil {
		result, err := runToolWithTimeout(ctx, se.toolRunner, cloned, ec, stepTimeout)
		if result != nil {
			result.Duration = time.Since(start)
		}
		return result, err
	}

	return &execution.StepResult{
		StepID:   cloned.StepID,
		StepName: cloned.Name,
		Type:     string(cloned.Type),
		Error:    fmt.Errorf("no tool runner configured"),
		Duration: time.Since(start),
	}, fmt.Errorf("tool step %q skipped: no tool runner configured", cloned.Name)
}

// runToolWithTimeout handles tool execution via any ToolRunner with timeout management
// and standardized StepResult construction. Used by both the default runner path and
// prefixHandler to avoid duplicating timeout/error logic.
func runToolWithTimeout(
	ctx context.Context,
	runner toolrunner.ToolRunner,
	step *execution.ExecutionStep,
	ec *execution.ExecutionContext,
	stepTimeout time.Duration,
) (*execution.StepResult, error) {
	var stepCtx context.Context
	var cancel context.CancelFunc
	if stepTimeout > 0 {
		stepCtx, cancel = context.WithTimeout(ctx, stepTimeout)
	} else {
		stepCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	start := time.Now()
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
	cloned, err := step.Prepare(prevResults)
	if err != nil {
		return nil, err
	}

	start := time.Now()

	// Route to registered handler first (e.g. if a skill name happens to
	// match a registered prefix). Handlers handle their own timeout/execution.
	if handler := se.lookupHandler(cloned); handler != nil {
		result, err := handler.Handle(ctx, cloned, ec, prevResults, stepTimeout)
		if result != nil {
			result.Duration = time.Since(start)
		}
		return result, err
	}

	if se.skillExecutor == nil {
		return &execution.StepResult{
			StepID: cloned.StepID, StepName: cloned.Name, Type: string(cloned.Type),
			Error: fmt.Errorf("no skill executor configured"), Duration: time.Since(start),
		}, fmt.Errorf("skill step %q skipped: no skill executor configured", cloned.Name)
	}

	inputBytes, err := cloned.ArgumentsJSON()
	if err != nil {
		return &execution.StepResult{
			StepID: cloned.StepID, StepName: cloned.Name, Type: string(cloned.Type),
			Error: fmt.Errorf("serialize arguments: %w", err), Duration: time.Since(start),
		}, fmt.Errorf("skill step %q: serialize arguments: %w", cloned.Name, err)
	}

	timeout := stepTimeout
	if cloned.Timeout > 0 {
		timeout = time.Duration(cloned.Timeout) * time.Millisecond
	}

	req := execution.ExecutionRequest{
		SkillID:   cloned.Name,
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
				StepID: cloned.StepID, StepName: cloned.Name, Type: string(cloned.Type),
				Error: ctx.Err(), Duration: duration,
			}, ctx.Err()
		}
		return &execution.StepResult{
			StepID: cloned.StepID, StepName: cloned.Name, Type: string(cloned.Type),
			Error: err, Duration: duration,
		}, err
	}

	var output any
	if result != nil {
		output = string(result.Output)
	}

	return &execution.StepResult{
		StepID: cloned.StepID, StepName: cloned.Name, Type: string(cloned.Type),
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

// prefixHandler adapts a ToolRunner to the StepHandler interface using
// step name prefix matching. This replaces the previous string-prefix
// map lookup with a pluggable handler pattern.
type prefixHandler struct {
	prefix string
	runner toolrunner.ToolRunner
}

// CanHandle returns true if the step name starts with this handler's prefix.
func (h *prefixHandler) CanHandle(step *execution.ExecutionStep) bool {
	return strings.HasPrefix(step.Name, h.prefix)
}

// Handle delegates to the shared runToolWithTimeout helper. The step has
// already been prepared (cloned + coerced + resolved) by the caller —
// ExecuteTool/ExecuteSkill call Prepare before dispatching to a handler, and
// the only other entry point (StepExecutor.Dispatch, used by the legacy
// ExecutePlan path) passes nil prevResults so resolution is a no-op there.
// Re-resolving here was historically redundant; Prepare is now the single
// resolution point.
func (h *prefixHandler) Handle(
	ctx context.Context,
	step *execution.ExecutionStep,
	ec *execution.ExecutionContext,
	prevResults map[string]any,
	stepTimeout time.Duration,
) (*execution.StepResult, error) {
	return runToolWithTimeout(ctx, h.runner, step, ec, stepTimeout)
}
