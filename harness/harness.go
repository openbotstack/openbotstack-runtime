package harness

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
	builtintools "github.com/openbotstack/openbotstack-runtime/tools/builtin"
	"github.com/openbotstack/openbotstack-runtime/toolrunner"
)

// HarnessDeps captures all optional harness dependencies.
// Pass to NewExecutionHarness to construct a fully-configured harness.
// All fields are optional (nil = feature disabled).
type HarnessDeps struct {
	ReasoningLoop   ReasoningLoop
	MCPRunner       toolrunner.ToolRunner
	BuiltinRunner   *builtintools.BuiltinToolRunner
	HookManager     *HookManager
	FailureHandler  *FailureHandler
	PermChecker     *PermissionChecker
	ApprovalGateway execution.ApprovalGateway
	ProgressCB      ProgressCallback
}

// ExecutionHarness orchestrates plan execution sequentially.
// It is a pure executor: it does NOT hold a planner and cannot generate plans.
// All planning decisions must be made before calling Run().
type ExecutionHarness struct {
	config          HarnessConfig
	stepExecutor    *StepExecutor
	reasoningLoop   ReasoningLoop
	hookManager     *HookManager
	failureHandler  *FailureHandler
	permChecker     *PermissionChecker
	approvalGateway execution.ApprovalGateway
	currentState    atomic.Value
	progressCB      ProgressCallback
	progressMu      sync.RWMutex
}

// NewExecutionHarness creates a new execution harness.
// The harness is a pure executor — it does NOT hold a planner.
func NewExecutionHarness(
	config HarnessConfig,
	toolRunner toolrunner.ToolRunner,
	skillExecutor execution.SkillExecutor,
	deps HarnessDeps,
) *ExecutionHarness {
	h := &ExecutionHarness{
		config:          config,
		stepExecutor:    NewStepExecutor(toolRunner, skillExecutor, StepExecutorDeps{MCPRunner: deps.MCPRunner, BuiltinRunner: deps.BuiltinRunner}),
		reasoningLoop:   deps.ReasoningLoop,
		hookManager:     deps.HookManager,
		failureHandler:  deps.FailureHandler,
		permChecker:     deps.PermChecker,
		approvalGateway: deps.ApprovalGateway,
	}
	if deps.ProgressCB != nil {
		h.progressCB = deps.ProgressCB
	}
	return h
}

// SetReasoningLoop configures the reasoning loop for LLM-type steps.
// Deprecated: Use HarnessDeps in NewExecutionHarness.
func (h *ExecutionHarness) SetReasoningLoop(rl ReasoningLoop) { h.reasoningLoop = rl }

// SetMCPRunner configures the MCP tool runner on the harness's step executor.
// Deprecated: Use HarnessDeps in NewExecutionHarness.
func (h *ExecutionHarness) SetMCPRunner(runner toolrunner.ToolRunner) {
	h.stepExecutor.SetMCPRunner(runner)
}

// SetBuiltinRunner configures the built-in tool runner on the harness's step executor.
// Deprecated: Use HarnessDeps in NewExecutionHarness.
func (h *ExecutionHarness) SetBuiltinRunner(runner *builtintools.BuiltinToolRunner) {
	h.stepExecutor.SetBuiltinRunner(runner)
}

// SetHookManager configures the hook manager.
// Deprecated: Use HarnessDeps in NewExecutionHarness.
func (h *ExecutionHarness) SetHookManager(hm *HookManager) { h.hookManager = hm }

// SetFailureHandler configures the failure handler.
// Deprecated: Use HarnessDeps in NewExecutionHarness.
func (h *ExecutionHarness) SetFailureHandler(fh *FailureHandler) { h.failureHandler = fh }

// SetPermissionChecker configures the permission checker.
// Deprecated: Use HarnessDeps in NewExecutionHarness.
func (h *ExecutionHarness) SetPermissionChecker(pc *PermissionChecker) { h.permChecker = pc }

// SetApprovalGateway configures the approval gateway for critical-risk step approval.
// Deprecated: Use HarnessDeps in NewExecutionHarness.
func (h *ExecutionHarness) SetApprovalGateway(gw execution.ApprovalGateway) {
	h.approvalGateway = gw
}

// SetProgressCallback configures a progress callback.
// Deprecated: Use HarnessDeps in NewExecutionHarness.
func (h *ExecutionHarness) SetProgressCallback(cb ProgressCallback) {
	h.progressMu.Lock()
	defer h.progressMu.Unlock()
	h.progressCB = cb
}

// State returns the current harness state for observability.
func (h *ExecutionHarness) State() HarnessState {
	if v := h.currentState.Load(); v != nil {
		return v.(HarnessState)
	}
	return HarnessInit
}

func (h *ExecutionHarness) setState(s HarnessState) {
	h.currentState.Store(s)
}

// Run executes a plan's steps sequentially within the given execution context.
func (h *ExecutionHarness) Run(ctx context.Context, plan *execution.ExecutionPlan, ec *execution.ExecutionContext) (*HarnessResult, error) {
	if ec == nil {
		return nil, fmt.Errorf("harness: ExecutionContext cannot be nil")
	}
	if plan == nil || !plan.IsFrozen() {
		return nil, fmt.Errorf("harness: plan must be validated and frozen before execution")
	}

	startTime := time.Now()
	result := &HarnessResult{
		PlanID:      fmt.Sprintf("plan-%d", startTime.UnixMilli()),
		StepResults: make([]execution.StepResult, 0),
	}

	sessionDeadline := ec.StartedAt.Add(h.config.MaxSessionRuntime)

	for i, step := range plan.Steps {
		h.setState(HarnessHookPre)

		if stop := h.checkBounds(ctx, i, sessionDeadline); stop != nil {
			result.StopCondition = *stop
			break
		}
		if stop, hookErr := h.runPreStep(ctx, step, i, plan, ec); hookErr != nil {
			return nil, hookErr
		} else if stop != nil {
			result.StopCondition = *stop
			break
		}

		h.setState(HarnessStepExec)
		h.emitProgress(ec, ProgressEvent{Type: "step_start", Content: step.Name})

		prevResults := h.buildPrevResults(result.StepResults)
		stepTimeout := time.Duration(step.Timeout) * time.Millisecond

		stepResult, execErr := h.dispatchStep(ctx, step, i, plan, ec, prevResults, stepTimeout, result)
		failedStepResult := stepResult // preserve original result for fail-fast append

		if execErr != nil && h.failureHandler != nil {
			h.setState(HarnessRetry)
			var handleErr error
			stepResult, handleErr = h.handleStepFailure(ctx, step, execErr, prevResults, ec, stepTimeout)
			if handleErr != nil {
				if failedStepResult != nil {
					result.StepResults = append(result.StepResults, *failedStepResult)
				}
				result.StepsExecuted = len(result.StepResults)
				result.StopCondition = StopCondition{Stopped: true, Reason: StopReasonFailFast}
				result.Duration = time.Since(startTime)
				return result, handleErr
			}
		}

		if stepResult != nil {
			result.StepResults = append(result.StepResults, *stepResult)
			result.Metrics.TotalSteps++
		}

		h.setState(HarnessHookPost)
		h.runPostStep(ctx, step, i, plan, ec, stepResult)
		h.emitProgress(ec, ProgressEvent{Type: "step_complete", Content: step.Name})
	}

	h.setState(HarnessDone)

	if h.hookManager != nil {
		h.hookManager.OnStop(ctx, &execution.HookContext{
			Plan: plan,
			EC:   ec,
		})
	}

	result.StepsExecuted = len(result.StepResults)
	result.Duration = time.Since(startTime)

	if !result.StopCondition.Stopped {
		result.StopCondition = StopCondition{Stopped: true, Reason: StopReasonGoalAchieved}
	}

	return result, nil
}

// checkBounds verifies session-level limits. Returns nil if execution should continue.
func (h *ExecutionHarness) checkBounds(ctx context.Context, stepIndex int, sessionDeadline time.Time) *StopCondition {
	if time.Now().After(sessionDeadline) {
		return &StopCondition{
			Stopped: true, Reason: StopReasonMaxSessionRuntime,
			Detail: "session runtime exceeded",
		}
	}
	if stepIndex >= h.config.MaxSteps {
		return &StopCondition{
			Stopped: true, Reason: StopReasonMaxSteps,
			Detail: fmt.Sprintf("max steps (%d) reached", h.config.MaxSteps),
		}
	}
	if ctx.Err() != nil {
		return &StopCondition{Stopped: true, Reason: StopReasonContextCanceled}
	}
	return nil
}

// runPreStep runs pre-step hooks, approval checks, and permission checks.
// Returns (nil, nil) if the step should proceed.
// Returns (stop, nil) if execution should stop with a condition.
// Returns (nil, err) if execution should return an error immediately.
func (h *ExecutionHarness) runPreStep(ctx context.Context, step execution.ExecutionStep, stepIndex int, plan *execution.ExecutionPlan, ec *execution.ExecutionContext) (*StopCondition, error) {
	if h.hookManager != nil {
		hookCtx := &execution.HookContext{
			Step:      snapshotStepPtr(step),
			StepIndex: stepIndex,
			Plan:      plan,
			EC:        ec,
		}
		hookResult, err := h.hookManager.PreStepExecute(ctx, hookCtx)
		if err != nil {
			return nil, fmt.Errorf("pre-step hook error for step %q: %w", step.Name, err)
		}
		if hookResult != nil && hookResult.Deny {
			return &StopCondition{
				Stopped: true, Reason: StopReasonHookDenied,
				Detail: fmt.Sprintf("step %q denied by hook: %s", step.Name, hookResult.Reason),
			}, nil
		}
	}

	if step.RiskLevel == "critical" && h.approvalGateway != nil {
		_, stop := h.waitForApproval(ctx, step, ec)
		if stop.Stopped {
			return &stop, nil
		}
	}

	if h.permChecker != nil {
		attrs := map[string]string{}
		if step.RiskLevel != "" {
			attrs["risk_level"] = step.RiskLevel
		}
		if err := h.permChecker.Check(ctx, step.Name, ec.TenantID, attrs); err != nil {
			return &StopCondition{
				Stopped: true, Reason: StopReasonHookDenied,
				Detail: fmt.Sprintf("step %q denied: %v", step.Name, err),
			}, nil
		}
	}

	return nil, nil
}

// dispatchStep routes to the correct step executor based on step type.
func (h *ExecutionHarness) dispatchStep(
	ctx context.Context,
	step execution.ExecutionStep,
	stepIndex int,
	plan *execution.ExecutionPlan,
	ec *execution.ExecutionContext,
	prevResults map[string]any,
	stepTimeout time.Duration,
	result *HarnessResult,
) (*execution.StepResult, error) {
	switch step.Type {
	case execution.StepTypeTool:
		return h.executeToolStep(ctx, step, stepIndex, plan, ec, prevResults, stepTimeout)
	case execution.StepTypeSkill:
		return h.executeSkillStep(ctx, step, stepIndex, plan, ec, prevResults, stepTimeout)
	case execution.StepTypeLLM:
		return h.executeLLMStep(ctx, step, ec, result)
	default:
		err := fmt.Errorf("unknown step type: %s", step.Type)
		return &execution.StepResult{
			StepID: step.StepID, StepName: step.Name, Type: string(step.Type),
			Error: err,
		}, err
	}
}

// executeToolStep handles tool-type steps with pre/post tool hooks.
func (h *ExecutionHarness) executeToolStep(
	ctx context.Context,
	step execution.ExecutionStep,
	stepIndex int,
	plan *execution.ExecutionPlan,
	ec *execution.ExecutionContext,
	prevResults map[string]any,
	stepTimeout time.Duration,
) (*execution.StepResult, error) {
	if h.hookManager != nil {
		hookCtx := &execution.HookContext{Step: snapshotStepPtr(step), StepIndex: stepIndex, Plan: plan, EC: ec}
		hookResult, hookErr := h.hookManager.PreToolUse(ctx, hookCtx)
		if hookErr != nil {
			return &execution.StepResult{StepID: step.StepID, StepName: step.Name, Type: string(step.Type), Error: hookErr}, hookErr
		}
		if hookResult != nil && hookResult.Deny {
			err := fmt.Errorf("tool %q denied by hook: %s", step.Name, hookResult.Reason)
			return &execution.StepResult{StepID: step.StepID, StepName: step.Name, Type: string(step.Type), Error: err}, err
		}
	}
	stepResult, execErr := h.stepExecutor.ExecuteTool(ctx, &step, ec, prevResults, stepTimeout)
	if h.hookManager != nil {
		hookCtx := &execution.HookContext{Step: snapshotStepPtr(step), StepIndex: stepIndex, Plan: plan, EC: ec, ToolOutput: stepResult}
		if err := h.hookManager.PostToolUse(ctx, hookCtx); err != nil {
			warnLog(ctx, ec, "post-tool hook error", "step", step.Name, "error", err)
		}
	}
	return stepResult, execErr
}

// executeSkillStep handles skill-type steps with pre/post tool hooks.
func (h *ExecutionHarness) executeSkillStep(
	ctx context.Context,
	step execution.ExecutionStep,
	stepIndex int,
	plan *execution.ExecutionPlan,
	ec *execution.ExecutionContext,
	prevResults map[string]any,
	stepTimeout time.Duration,
) (*execution.StepResult, error) {
	if h.hookManager != nil {
		hookCtx := &execution.HookContext{Step: snapshotStepPtr(step), StepIndex: stepIndex, Plan: plan, EC: ec}
		hookResult, hookErr := h.hookManager.PreToolUse(ctx, hookCtx)
		if hookErr != nil {
			return &execution.StepResult{StepID: step.StepID, StepName: step.Name, Type: string(step.Type), Error: hookErr}, hookErr
		}
		if hookResult != nil && hookResult.Deny {
			err := fmt.Errorf("skill %q denied by hook: %s", step.Name, hookResult.Reason)
			return &execution.StepResult{StepID: step.StepID, StepName: step.Name, Type: string(step.Type), Error: err}, err
		}
	}
	stepResult, execErr := h.stepExecutor.ExecuteSkill(ctx, &step, ec, prevResults, stepTimeout)
	if h.hookManager != nil {
		hookCtx := &execution.HookContext{Step: snapshotStepPtr(step), StepIndex: stepIndex, Plan: plan, EC: ec, ToolOutput: stepResult}
		if err := h.hookManager.PostToolUse(ctx, hookCtx); err != nil {
			warnLog(ctx, ec, "post-tool hook error", "step", step.Name, "error", err)
		}
	}
	return stepResult, execErr
}

// executeLLMStep handles LLM-type steps via the reasoning loop.
func (h *ExecutionHarness) executeLLMStep(ctx context.Context, step execution.ExecutionStep, ec *execution.ExecutionContext, result *HarnessResult) (*execution.StepResult, error) {
	if h.reasoningLoop == nil {
		err := fmt.Errorf("step %q is LLM type but no reasoning loop configured", step.Name)
		return &execution.StepResult{
			StepID: step.StepID, StepName: step.Name, Type: string(step.Type),
			Error: err, Duration: 0,
		}, err
	}
	pCtx := &planner.PlannerContext{UserRequest: step.ExpectedOutput}
	rlResult, rlErr := h.reasoningLoop.Run(ctx, &step, pCtx, ec)
	if rlErr != nil {
		return &execution.StepResult{
			StepID: step.StepID, StepName: step.Name, Type: string(step.Type),
			Error: rlErr,
		}, rlErr
	}
	result.Metrics.TotalLLMTurns += rlResult.TurnCount
	return &execution.StepResult{
		StepID: step.StepID, StepName: step.Name, Type: string(step.Type),
		Output: rlResult.Output, Duration: rlResult.Duration,
	}, nil
}

// handleStepFailure delegates to the failure handler for retry/fallback.
func (h *ExecutionHarness) handleStepFailure(
	ctx context.Context,
	step execution.ExecutionStep,
	execErr error,
	prevResults map[string]any,
	ec *execution.ExecutionContext,
	stepTimeout time.Duration,
) (*execution.StepResult, error) {
	fallbackResult, handleErr := h.failureHandler.Handle(ctx, &step, execErr, func() (*execution.StepResult, error) {
		switch step.Type {
		case execution.StepTypeTool:
			return h.stepExecutor.ExecuteTool(ctx, &step, ec, prevResults, stepTimeout)
		case execution.StepTypeSkill:
			return h.stepExecutor.ExecuteSkill(ctx, &step, ec, prevResults, stepTimeout)
		default:
			return nil, execErr
		}
	})

	if handleErr != nil {
		return nil, handleErr
	}

	if fallbackResult != nil && fallbackResult.Fallback {
		h.setState(HarnessFallback)
		fallbackTool := h.failureHandler.FallbackToolFor(step.StepID)
		if fbResult, fbErr := h.stepExecutor.ExecuteFallback(ctx, fallbackTool, step.Arguments, ec, prevResults); fbErr == nil {
			fbResult.Fallback = true
			return fbResult, nil
		}
	}

	return fallbackResult, nil
}

// runPostStep runs post-step hooks and emits progress.
func (h *ExecutionHarness) runPostStep(
	ctx context.Context,
	step execution.ExecutionStep,
	stepIndex int,
	plan *execution.ExecutionPlan,
	ec *execution.ExecutionContext,
	stepResult *execution.StepResult,
) {
	if h.hookManager != nil {
		hookCtx := &execution.HookContext{
			Step:      snapshotStepPtr(step),
			StepIndex: stepIndex,
			Plan:      plan,
			EC:        ec,
			ToolOutput: stepResult,
		}
		if err := h.hookManager.PostStepExecute(ctx, hookCtx); err != nil {
			warnLog(ctx, ec, "post-step hook error", "step", step.Name, "error", err)
		}
	}
}

// buildPrevResults creates a map of previous step outputs for template interpolation.
func (h *ExecutionHarness) buildPrevResults(stepResults []execution.StepResult) map[string]any {
	prevResults := make(map[string]any)
	for _, sr := range stepResults {
		if sr.Output != nil {
			prevResults[sr.StepName] = sr.Output
		}
	}
	return prevResults
}

// PlanAndRun creates a plan via the given planner and executes it on this harness.
// This is a convenience function for callers that have a planner and want plan+execute
// in one call. The planner belongs to the caller (Control Plane), not the harness.
func PlanAndRun(ctx context.Context, pl planner.ExecutionPlanner, h *ExecutionHarness, task TaskInput, ec *execution.ExecutionContext) (*HarnessResult, error) {
	if pl == nil {
		return nil, fmt.Errorf("harness.PlanAndRun: planner is nil")
	}
	if task.PlannerContext == nil {
		task.PlannerContext = &planner.PlannerContext{
			UserRequest: task.TaskDescription,
		}
	}
	if task.PlannerContext.UserRequest == "" {
		task.PlannerContext.UserRequest = task.TaskDescription
	}

	plan, err := pl.Plan(ctx, task.PlannerContext)
	if err != nil {
		return nil, fmt.Errorf("harness.PlanAndRun: planning failed: %w", err)
	}
	if plan == nil || len(plan.Steps) == 0 {
		return &HarnessResult{
			StopCondition: StopCondition{Stopped: true, Reason: StopReasonPlannerStopped},
		}, nil
	}

	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("harness.PlanAndRun: plan validation failed: %w", err)
	}

	return h.Run(ctx, plan, ec)
}

func (h *ExecutionHarness) emitProgress(ec *execution.ExecutionContext, event ProgressEvent) {
	// Per-request callback (SSE streaming) takes precedence over shared callback.
	// This avoids race conditions on the shared progressCB field under concurrent requests.
	if ec != nil && ec.ProgressFn != nil {
		ec.ProgressFn(event.Type, event.Content, event.Turn, event.Tool)
		return
	}
	h.progressMu.RLock()
	cb := h.progressCB
	h.progressMu.RUnlock()
	if cb != nil {
		cb(event)
	}
}

// waitForApproval requests human approval for a critical step and polls until resolved or timed out.
func (h *ExecutionHarness) waitForApproval(ctx context.Context, step execution.ExecutionStep, ec *execution.ExecutionContext) (*execution.ApprovalRequest, StopCondition) {
	req := &execution.ApprovalRequest{
		StepName:    step.Name,
		StepID:      step.StepID,
		ExecutionID: ec.RequestID,
		TenantID:    ec.TenantID,
		RiskLevel:   step.RiskLevel,
		Reason:      "critical risk level requires human approval",
	}

	approval, err := h.approvalGateway.RequestApproval(ctx, req)
	if err != nil {
		return nil, StopCondition{
			Stopped: true,
			Reason:  StopReasonApprovalRequired,
			Detail:  fmt.Sprintf("step %q approval request failed: %v", step.Name, err),
		}
	}

	h.emitProgress(ec, ProgressEvent{
		Type:    "approval_required",
		Content: step.Name,
	})

	const pollInterval = 500 * time.Millisecond
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			reason := StopReasonApprovalTimeout
			if ctx.Err() == context.Canceled {
				reason = StopReasonContextCanceled
			}
			return nil, StopCondition{
				Stopped: true,
				Reason:  reason,
				Detail:  fmt.Sprintf("step %q approval cancelled: %v", step.Name, ctx.Err()),
			}
		case <-ticker.C:
			updated, getErr := h.approvalGateway.GetApproval(approval.ID)
			if getErr != nil {
				return nil, StopCondition{
					Stopped: true,
					Reason:  StopReasonApprovalRequired,
					Detail:  fmt.Sprintf("step %q approval lost from store: %v", step.Name, getErr),
				}
			}
			switch updated.Status {
			case execution.ApprovalApproved:
				h.emitProgress(ec, ProgressEvent{
					Type:    "approval_granted",
					Content: step.Name,
				})
				return updated, StopCondition{}
			case execution.ApprovalDenied:
				return nil, StopCondition{
					Stopped: true,
					Reason:  StopReasonApprovalDenied,
					Detail:  fmt.Sprintf("step %q approval denied: %s", step.Name, updated.DenyReason),
				}
			case execution.ApprovalExpired:
				return nil, StopCondition{
					Stopped: true,
					Reason:  StopReasonApprovalExpired,
					Detail:  fmt.Sprintf("step %q approval expired", step.Name),
				}
			}
		}
	}
}

// snapshotStep creates a shallow copy of an ExecutionStep with a cloned Arguments map.
// snapshotStepPtr wraps it and returns a pointer. Both prevent hooks from mutating
// the frozen plan's step data.
func snapshotStepPtr(s execution.ExecutionStep) *execution.ExecutionStep {
	cp := snapshotStep(s)
	return &cp
}

func snapshotStep(s execution.ExecutionStep) execution.ExecutionStep {
	cp := s
	if s.Arguments != nil {
		cp.Arguments = make(map[string]any, len(s.Arguments))
		for k, v := range s.Arguments {
			cp.Arguments[k] = v
		}
	}
	return cp
}
