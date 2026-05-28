package harness

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/openbotstack/openbotstack-core/assistant"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
)

// emitLoopProgress safely emits a progress event via the execution context.
func emitLoopProgress(ec *execution.ExecutionContext, eventType, content string) {
	if ec != nil && ec.ProgressFn != nil {
		ec.ProgressFn(eventType, content, 0, "")
	}
}

// formatStepDesc formats a step name with its arguments for display.
func formatStepDesc(step *execution.ExecutionStep) string {
	if len(step.Arguments) == 0 {
		return step.Name
	}
	parts := make([]string, 0, len(step.Arguments))
	for k, v := range step.Arguments {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return step.Name + "(" + strings.Join(parts, ", ") + ")"
}

// ReasoningLoop executes iterative LLM reasoning for a single LLM-type step.
// It is bounded, non-nesting, and request-scoped.
type ReasoningLoop interface {
	Run(ctx context.Context, step *execution.ExecutionStep, pCtx *planner.PlannerContext, ec *execution.ExecutionContext) (*ReasoningResult, error)
}

// DefaultReasoningLoop implements the bounded reasoning loop.
type DefaultReasoningLoop struct {
	config       ReasoningLoopConfig
	planner      planner.ExecutionPlanner
	stepExecutor *StepExecutor
	compactor    ContextCompactor
}

// NewDefaultReasoningLoop creates a new reasoning loop.
func NewDefaultReasoningLoop(
	config ReasoningLoopConfig,
	plannerExec planner.ExecutionPlanner,
	stepExecutor *StepExecutor,
	compactor ContextCompactor,
) *DefaultReasoningLoop {
	return &DefaultReasoningLoop{
		config:       config,
		planner:      plannerExec,
		stepExecutor: stepExecutor,
		compactor:    compactor,
	}
}

// Run executes the iterative reasoning loop until a stop condition is met.
// Max 5 turns, no nesting, request-scoped.
func (rl *DefaultReasoningLoop) Run(ctx context.Context, step *execution.ExecutionStep, pCtx *planner.PlannerContext, ec *execution.ExecutionContext) (*ReasoningResult, error) {
	startTime := time.Now()

	if rl.config.MaxTurns <= 0 || rl.config.MaxToolCalls <= 0 {
		return nil, fmt.Errorf("reasoning_loop: invalid config (MaxTurns=%d, MaxToolCalls=%d)", rl.config.MaxTurns, rl.config.MaxToolCalls)
	}

	var turnResults []TurnResult
	toolCallsUsed := 0
	var lastOutput any
	var lastPlanSignature string

	for turnsElapsed := 1; turnsElapsed <= rl.config.MaxTurns; turnsElapsed++ {
		// Safety valve: hard limit at 2x MaxTurns
		if turnsElapsed > 2*rl.config.MaxTurns {
			errorLog(ctx, ec, "reasoning loop safety valve triggered", "turns", turnsElapsed)
			break
		}

		if ctx.Err() != nil {
			return &ReasoningResult{
				Output: lastOutput, TurnCount: turnsElapsed - 1,
				ToolCalls: toolCallsUsed, StopReason: StopReasonContextCanceled,
				Duration: time.Since(startTime),
			}, ctx.Err()
		}

		turnStart := time.Now()
		turnResult := TurnResult{TurnNumber: turnsElapsed}

		// PLAN phase
		remaining := rl.config.MaxTurnRuntime - time.Since(turnStart)
		if remaining <= 0 {
			turnResult.StopReason = StopReasonMaxRuntime
			turnResults = append(turnResults, turnResult)
			break
		}

		planCtx, planCancel := context.WithTimeout(ctx, remaining)
		plan, err := rl.planner.Plan(planCtx, pCtx)
		planCancel()

		if err != nil {
			if ctx.Err() != nil {
				return &ReasoningResult{
					Output: lastOutput, TurnCount: turnsElapsed,
					ToolCalls: toolCallsUsed, StopReason: StopReasonContextCanceled,
					Duration: time.Since(startTime),
				}, ctx.Err()
			}
			// No skills available means the loop cannot make progress — stop immediately.
			if errors.Is(err, planner.ErrNoSkillsAvailable) {
				turnResult.StopReason = StopReasonPlannerStopped
				turnResult.Observations = append(turnResult.Observations, "no skills available for reasoning")
				turnResult.Duration = time.Since(turnStart)
				turnResults = append(turnResults, turnResult)
				break
			}
			turnResult.StopReason = StopReasonError
			turnResult.Observations = append(turnResult.Observations, fmt.Sprintf("planning error: %v", err))
			turnResult.Duration = time.Since(turnStart)
			turnResults = append(turnResults, turnResult)
			continue
		}

		if plan == nil || len(plan.Steps) == 0 {
			turnResult.StopReason = StopReasonPlannerStopped
			turnResult.Duration = time.Since(turnStart)
			turnResults = append(turnResults, turnResult)
			break
		}

		// Freeze plan if not already frozen
		if !plan.IsFrozen() {
			if err := plan.Validate(); err != nil {
				warnLog(ctx, ec, "reasoning loop: plan validation failed, executing valid steps only", "error", err)
			}
		}

		turnResult.PlanText = plan.Reasoning

		// Repeat plan detection
		if rl.config.RepeatPlanStop && plan.Reasoning == lastPlanSignature && lastPlanSignature != "" {
			debugLog(ctx, ec, "reasoning loop: repeated plan detected, stopping", "turn", turnsElapsed)
			turnResult.StopReason = StopReasonPlannerStopped
			turnResult.Duration = time.Since(turnStart)
			turnResults = append(turnResults, turnResult)
			break
		}
		lastPlanSignature = plan.Reasoning

		// ACT phase: execute each step in the plan
		stepResults := make(map[string]any)
		for _, planStep := range plan.Steps {
			if ctx.Err() != nil {
				turnResult.StopReason = StopReasonContextCanceled
				break
			}
			if time.Since(turnStart) >= rl.config.MaxTurnRuntime {
				turnResult.StopReason = StopReasonMaxRuntime
				break
			}

			// Step timeout
			var stepTimeout time.Duration
			if planStep.Timeout > 0 {
				stepTimeout = time.Duration(planStep.Timeout) * time.Millisecond
			} else {
				remaining := rl.config.MaxTurnRuntime - time.Since(turnStart)
				if remaining <= 0 {
					turnResult.StopReason = StopReasonMaxRuntime
					break
				}
				stepTimeout = remaining
			}

			var res *execution.StepResult
			var execErr error

			switch planStep.Type {
			case execution.StepTypeTool:
				if toolCallsUsed >= rl.config.MaxToolCalls {
					warnLog(ctx, ec, "reasoning loop: tool call budget exhausted", "used", toolCallsUsed, "max", rl.config.MaxToolCalls)
					turnResult.Actions = append(turnResult.Actions, TurnAction{
						StepName: planStep.Name,
						StepType: string(planStep.Type),
						Input:    planStep.Arguments,
						Error:    "skipped: tool call budget exhausted",
					})
					turnResult.ActionsExecuted = append(turnResult.ActionsExecuted, planStep.Name)
					break
				}
				toolCallsUsed++
				emitLoopProgress(ec, "tool_call", formatStepDesc(&planStep))
				res, execErr = rl.stepExecutor.ExecuteTool(ctx, &planStep, ec, stepResults, stepTimeout)

			case execution.StepTypeSkill:
				emitLoopProgress(ec, "tool_call", formatStepDesc(&planStep))
				res, execErr = rl.stepExecutor.ExecuteSkill(ctx, &planStep, ec, stepResults, stepTimeout)

			case execution.StepTypeLLM:
				// Nested LLM steps are not allowed
				warnLog(ctx, ec, "reasoning loop: nested LLM step not allowed", "step", planStep.Name)
				turnResult.Observations = append(turnResult.Observations,
					fmt.Sprintf("step %q skipped: nested LLM steps not allowed in reasoning loop", planStep.Name))
				continue

			default:
				warnLog(ctx, ec, "reasoning loop: unknown step type", "type", planStep.Type)
				continue
			}

			// Record structured action data for visualization.
			if planStep.Type != execution.StepTypeLLM {
				action := TurnAction{
					StepName: planStep.Name,
					StepType: string(planStep.Type),
					Input:    planStep.Arguments,
				}
				if res != nil {
					action.Output = res.Output
					action.DurationMs = int(res.Duration.Milliseconds())
				}
				if execErr != nil {
					action.Error = execErr.Error()
				}
				turnResult.Actions = append(turnResult.Actions, action)
				turnResult.ActionsExecuted = append(turnResult.ActionsExecuted, planStep.Name)
			}

			if execErr != nil {
				if ctx.Err() != nil {
					turnResult.StopReason = StopReasonContextCanceled
					break
				}
				turnResult.Observations = append(turnResult.Observations, fmt.Sprintf("step %q failed: %v", planStep.Name, execErr))
				continue
			}

			if res != nil {
				stepResults[planStep.Name] = res.Output
				if res.Output != nil {
					lastOutput = res.Output
					turnResult.Observations = append(turnResult.Observations, fmt.Sprintf("%v", res.Output))
				}
			}
		}

		turnResult.ToolCallsUsed = toolCallsUsed
		turnResult.Duration = time.Since(turnStart)

		// No productive work: all steps were skipped or produced no output — stop.
		if len(turnResult.Actions) == 0 && turnResult.StopReason == "" {
			turnResult.StopReason = StopReasonPlannerStopped
		}

		// Check stop conditions
		if turnResult.StopReason == "" {
			if turnsElapsed >= rl.config.MaxTurns {
				turnResult.StopReason = StopReasonMaxTurns
			} else if toolCallsUsed >= rl.config.MaxToolCalls {
				turnResult.StopReason = StopReasonMaxToolCalls
			}
		}

		turnResults = append(turnResults, turnResult)

		if turnResult.StopReason != "" {
			break
		}

		// Update PlannerContext with observations for next turn.
			// Observations are wrapped in XML markers to prevent tool output
			// from being interpreted as planner instructions.
			if pCtx != nil && len(turnResult.Observations) > 0 {
				obsStr := fmt.Sprintf("<observation turn=\"%d\">\n%v\n</observation>", turnsElapsed, turnResult.Observations)
				pCtx.MemoryContext = append(pCtx.MemoryContext, assistant.SearchResult{
					Content: []byte(obsStr),
					Score:   1.0,
				})
			}

		// Context compaction between turns
		if rl.compactor != nil && len(turnResults) > 2 {
			compacted, err := rl.compactor.Compact(ctx, turnResults)
			if err != nil {
				warnLog(ctx, ec, "reasoning loop: compaction failed", "error", err)
			} else {
				turnResults = compacted
			}
		}
	}

	// Determine final stop reason
	var stopReason StopReason
	if len(turnResults) > 0 {
		stopReason = turnResults[len(turnResults)-1].StopReason
	}
	if stopReason == "" {
		stopReason = StopReasonGoalAchieved
	}

	return &ReasoningResult{
		Output:      lastOutput,
		TurnCount:   len(turnResults),
		ToolCalls:   toolCallsUsed,
		StopReason:  stopReason,
		Duration:    time.Since(startTime),
		TurnResults: turnResults,
	}, nil
}
