package harness

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	"github.com/openbotstack/openbotstack-core/audit"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/execution/template"
	"github.com/openbotstack/openbotstack-core/planner"
	"github.com/openbotstack/openbotstack-runtime/logging/execution_logs"
	"github.com/openbotstack/openbotstack-runtime/toolrunner"
)

// PlannerContext is now passed explicitly via ExecutionContext.SetPlannerContext().
// See ADR-018 and execution.ExecutionContext for the explicit seam.

// LLMGenerator generates a direct LLM text response (no planning).
// Used for "respond" steps where the planner already decided a direct LLM reply is needed.
type LLMGenerator func(ctx context.Context, systemPrompt, userMessage string, history []aitypes.Message) (string, error)

// HarnessDeps captures all optional harness dependencies.
// Pass to NewExecutionHarness to construct a fully-configured harness.
// All fields are optional (nil = feature disabled).
type HarnessDeps struct {
	ReasoningLoop   ReasoningLoop
	LLMGenerator    LLMGenerator
	AuditLogger     execution_logs.AuditLogger
	MCPRunner       toolrunner.ToolRunner
	BuiltinRunner   toolrunner.ToolRunner
	HookManager     *HookManager
	FailureHandler  *FailureHandler
	PermChecker     *PermissionChecker
	ApprovalGateway execution.ApprovalGateway
	ProgressCB      ProgressCallback
	Replanner       planner.Replanner
}

// ExecutionHarness orchestrates plan execution sequentially.
// It is a pure executor: it does NOT hold a planner and cannot generate plans.
// All planning decisions must be made before calling Run().
type ExecutionHarness struct {
	config          HarnessConfig
	stepExecutor    *StepExecutor
	reasoningLoop   ReasoningLoop
	llmGenerator    LLMGenerator
	auditLogger     execution_logs.AuditLogger
	hookManager     *HookManager
	failureHandler  *FailureHandler
	permChecker     *PermissionChecker
	approvalGateway execution.ApprovalGateway
	replanner       planner.Replanner
	replanConfig    ReplanConfig
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
		llmGenerator:    deps.LLMGenerator,
		auditLogger:     deps.AuditLogger,
		hookManager:     deps.HookManager,
		failureHandler:  deps.FailureHandler,
		permChecker:     deps.PermChecker,
		approvalGateway: deps.ApprovalGateway,
		replanner:       deps.Replanner,
		replanConfig:    DefaultReplanConfig(),
	}
	if deps.ProgressCB != nil {
		h.progressCB = deps.ProgressCB
	}
	return h
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
		PlanID:      plan.ID,
		StepResults: make([]execution.StepResult, 0),
		TurnData:    make(map[string][]TurnResult),
		StepInputs:  make(map[string]map[string]any),
		PlanIDs:     []string{plan.ID},
	}

	sessionDeadline := ec.StartedAt.Add(h.config.MaxSessionRuntime)

	// Copy plan steps into a mutable active slice for replan support.
	activeSteps := make([]execution.ExecutionStep, len(plan.Steps))
	copy(activeSteps, plan.Steps)
	replanCount := 0

	for i := 0; i < len(activeSteps); i++ {
		step := activeSteps[i]
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
		if stepTimeout == 0 {
			stepTimeout = h.config.DefaultStepTimeout
		}
		if stepTimeout == 0 {
			stepTimeout = 120 * time.Second
		}

		// Resolve templates in step arguments so the trace shows actual values.
		if step.Arguments != nil {
			resolved := make(map[string]any, len(step.Arguments))
			for k, v := range step.Arguments {
				if s, ok := v.(string); ok {
					resolved[k] = template.Resolve(s, prevResults)
				} else {
					resolved[k] = v
				}
			}
			result.StepInputs[step.StepID] = resolved
		}

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
			// Failure handler recovered the step — clear execErr so replan is not triggered.
			execErr = nil
		}

		// Controlled Replan: after failure handler, check if replanning should occur.
		stepStillFailed := stepResult != nil && stepResult.Error != nil
		explicitSignal := hasExplicitReplanSignal(stepResult)
		if h.replanner != nil && (execErr != nil || stepStillFailed || explicitSignal) {
			checkResult := ShouldReplan(stepResult, execErr, replanCount, h.replanConfig, true)
			if checkResult.ShouldReplan {
				h.setState(HarnessReplan)
				newPlan, replanErr := h.attemptReplan(ctx, plan, step, stepResult, execErr, result, ec, checkResult)
				if replanErr == nil && newPlan != nil && len(newPlan.Steps) > 0 {
					// Append the failed step result for audit trail before replacing.
					if stepResult != nil {
						result.StepResults = append(result.StepResults, *stepResult)
						result.Metrics.TotalSteps++
						if stepResult.Type == string(execution.StepTypeTool) || stepResult.Type == string(execution.StepTypeSkill) {
							result.Metrics.TotalToolCalls++
						}
						h.emitStepAudit(ctx, step, stepResult, ec)
					}
					// Replace remaining steps (from i+1 onwards) with new plan steps.
					executed := activeSteps[:i+1]
					activeSteps = make([]execution.ExecutionStep, len(executed)+len(newPlan.Steps))
					copy(activeSteps, executed)
					copy(activeSteps[len(executed):], newPlan.Steps)
					replanCount++
					result.ReplanCount = replanCount
					result.PlanIDs = append(result.PlanIDs, newPlan.ID)
					h.emitReplanAudit(ctx, plan, newPlan, step, checkResult, ec)
					h.emitProgress(ec, ProgressEvent{Type: "step_replanned", Content: newPlan.ID})
					// Continue loop — next iteration will execute first step of new plan.
					continue
				}
				// Replan failed; fall through to normal error/result handling.
			}
		}

		if stepResult != nil {
			result.StepResults = append(result.StepResults, *stepResult)
			result.Metrics.TotalSteps++
			if stepResult.Type == string(execution.StepTypeTool) || stepResult.Type == string(execution.StepTypeSkill) {
				result.Metrics.TotalToolCalls++
			}
			h.emitStepAudit(ctx, step, stepResult, ec)
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
	result.Metrics.TotalRuntime = result.Duration

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
	case execution.StepTypeTool, execution.StepTypeSkill:
		return h.executeStep(ctx, step, stepIndex, plan, ec, prevResults, stepTimeout)
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

// executeStep handles tool and skill steps with unified pre/post hook logic.
func (h *ExecutionHarness) executeStep(
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
			err := fmt.Errorf("%s %q denied by hook: %s", step.Type, step.Name, hookResult.Reason)
			return &execution.StepResult{StepID: step.StepID, StepName: step.Name, Type: string(step.Type), Error: err}, err
		}
	}
	stepResult, execErr := h.stepExecutor.Execute(ctx, &step, ec, prevResults, stepTimeout)
	if h.hookManager != nil {
		var outputCopy any
		if stepResult != nil {
			cp := *stepResult
			outputCopy = &cp
		}
		hookCtx := &execution.HookContext{Step: snapshotStepPtr(step), StepIndex: stepIndex, Plan: plan, EC: ec, ToolOutput: outputCopy}
		if err := h.hookManager.PostToolUse(ctx, hookCtx); err != nil {
			warnLog(ctx, ec, "post-tool hook error", "step", step.Name, "error", err)
		}
	}
	return stepResult, execErr
}

// executeLLMStep handles LLM-type steps.
// For "respond" steps, calls LLM directly. For complex reasoning, uses the ReasoningLoop.
func (h *ExecutionHarness) executeLLMStep(ctx context.Context, step execution.ExecutionStep, ec *execution.ExecutionContext, result *HarnessResult) (*execution.StepResult, error) {
	startTime := time.Now()

	// Resolve step result interpolation templates in arguments before use.
	prevResults := h.buildPrevResults(result.StepResults)
	step.ResolveArguments(prevResults)

	// Derive user request: prefer ExpectedOutput, fall back to arguments.prompt.
	userRequest := step.ExpectedOutput
	if userRequest == "" {
		if prompt, ok := step.Arguments["prompt"].(string); ok && prompt != "" {
			userRequest = prompt
		}
	}

	// Recover the original PlannerContext (with Skills, Soul, etc.)
	// that was set on ExecutionContext via PlanAndRun.
	var origCtx *planner.PlannerContext
	if ec.PlannerContext() != nil {
		var ok bool
		origCtx, ok = ec.PlannerContext().(*planner.PlannerContext)
		if !ok {
			slog.Warn("harness: planner context type mismatch",
				"type", fmt.Sprintf("%T", ec.PlannerContext()))
		}
	}

	// "respond" steps: direct LLM generation (no iterative planning loop needed).
	if step.Name == "respond" && h.llmGenerator != nil {
		systemPrompt := ""
		var history []aitypes.Message
		if origCtx != nil {
			systemPrompt = origCtx.Soul.SystemPrompt
			history = origCtx.ConversationHistory
		}
		response, err := h.llmGenerator(ctx, systemPrompt, userRequest, history)
		if err != nil {
			return &execution.StepResult{
				StepID: step.StepID, StepName: step.Name, Type: string(step.Type),
				Error: err, Duration: time.Since(startTime),
			}, err
		}
		return &execution.StepResult{
			StepID: step.StepID, StepName: step.Name, Type: string(step.Type),
			Output: response, Duration: time.Since(startTime),
		}, nil
	}

	// Complex LLM steps: delegate to ReasoningLoop for iterative tool-calling.
	if h.reasoningLoop == nil {
		err := fmt.Errorf("step %q is LLM type but no reasoning loop configured", step.Name)
		return &execution.StepResult{
			StepID: step.StepID, StepName: step.Name, Type: string(step.Type),
			Error: err, Duration: 0,
		}, err
	}

	pCtx := &planner.PlannerContext{UserRequest: userRequest}
	if origCtx != nil {
		pCtx = &planner.PlannerContext{
			UserRequest:         userRequest,
			Skills:              origCtx.Skills,
			Soul:                origCtx.Soul,
			AssistantID:         origCtx.AssistantID,
			MemoryContext:       origCtx.MemoryContext,
			ConversationHistory: origCtx.ConversationHistory,
		}
	}

	rlResult, rlErr := h.reasoningLoop.Run(ctx, &step, pCtx, ec)
	if rlErr != nil {
		return &execution.StepResult{
			StepID: step.StepID, StepName: step.Name, Type: string(step.Type),
			Error: rlErr,
		}, rlErr
	}
	result.Metrics.TotalLLMTurns += rlResult.TurnCount

	// Save turn data for trace visualization.
	if len(rlResult.TurnResults) > 0 {
		result.TurnData[step.StepID] = rlResult.TurnResults
	}

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
		return h.stepExecutor.Execute(ctx, &step, ec, prevResults, stepTimeout)
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
			Step:       snapshotStepPtr(step),
			StepIndex:  stepIndex,
			Plan:       plan,
			EC:         ec,
			ToolOutput: stepResult,
		}
		if err := h.hookManager.PostStepExecute(ctx, hookCtx); err != nil {
			warnLog(ctx, ec, "post-step hook error", "step", step.Name, "error", err)
		}
	}
}

// buildPrevResults creates a map of previous step outputs for template interpolation.
// Registers both the full step name (e.g. "builtin.web_fetch") and a short alias
// (e.g. "web_fetch") so LLM-generated templates like {{web_fetch}} resolve correctly.
func (h *ExecutionHarness) buildPrevResults(stepResults []execution.StepResult) map[string]any {
	prevResults := make(map[string]any)
	for _, sr := range stepResults {
		if sr.Output != nil {
			prevResults[sr.StepName] = sr.Output
			// Register short aliases for prefixed step names.
			for _, prefix := range []string{"builtin.", "mcp."} {
				if strings.HasPrefix(sr.StepName, prefix) {
					short := strings.TrimPrefix(sr.StepName, prefix)
					if _, exists := prevResults[short]; !exists {
						prevResults[short] = sr.Output
					}
				}
			}
		}
	}
	return prevResults
}

// isSimpleRespondRequest checks if a user message is simple enough to skip planning.
// Short messages without tool-related keywords can go directly to LLM response.
func isSimpleRespondRequest(msg string) bool {
	if len(msg) > 100 {
		return false
	}
	lower := strings.ToLower(msg)
	toolKeywords := []string{"tool", "use ", "call ", "execute", "fetch", "search", "file",
		"http", "mcp.", "builtin.", "skill", "read ", "write "}
	for _, kw := range toolKeywords {
		if strings.Contains(lower, kw) {
			return false
		}
	}
	return true
}

// PlanAndRun creates a plan via the given planner and executes it on this harness.
// For simple requests (short, no tool keywords), skips the planner and creates
// a single "respond" step directly.
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

	var plan *execution.ExecutionPlan

	// Fast path: skip planner for simple respond requests
	if isSimpleRespondRequest(task.PlannerContext.UserRequest) && h.llmGenerator != nil {
		plan = &execution.ExecutionPlan{
			Steps: []execution.ExecutionStep{
				{Name: "respond", Type: execution.StepTypeLLM, Arguments: map[string]any{"prompt": task.PlannerContext.UserRequest}},
			},
		}
		if err := plan.Validate(); err != nil {
			return nil, fmt.Errorf("harness.PlanAndRun: fast-path plan validation: %w", err)
		}
		slog.DebugContext(ctx, "harness: fast path — skipped planner for simple request")
	} else {
		var err error
		plan, err = pl.Plan(ctx, task.PlannerContext)
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
	}

	// Set PlannerContext on ExecutionContext so LLM steps can access
	// the original Skills, Soul, and MemoryContext.
	ec.SetPlannerContext(task.PlannerContext)

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

// emitStepAudit records a step-level audit event with full step context.
func (h *ExecutionHarness) emitStepAudit(ctx context.Context, step execution.ExecutionStep, sr *execution.StepResult, ec *execution.ExecutionContext) {
	if h.auditLogger == nil || ec == nil {
		return
	}

	status := "completed"
	var errStr string
	if sr.Error != nil {
		status = "error"
		errStr = sr.Error.Error()
	}

	event := audit.AuditEvent{
		ID:        step.StepID,
		TenantID:  ec.TenantID,
		UserID:    ec.UserID,
		RequestID: ec.RequestID,
		Source:    audit.SourceExecutor,
		Action:    "harness.step",
		Resource:  step.Name,
		Outcome:   status,
		Duration:  sr.Duration,
		Timestamp: time.Now().UTC(),
		StepID:    step.StepID,
		StepName:  step.Name,
		StepType:  sr.Type,
		Status:    status,
		Error:     errStr,
		TraceID:   ec.RequestID,
		ToolInput: step.Arguments,
	}
	if sr.Output != nil {
		event.ToolOutput = sr.Output
	}

	if err := h.auditLogger.Log(ctx, event); err != nil {
		slog.WarnContext(ctx, "harness: failed to emit step audit", "step", step.Name, "error", err)
	}
}
