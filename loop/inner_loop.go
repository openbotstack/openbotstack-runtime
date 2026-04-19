package loop

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/openbotstack/openbotstack-core/assistant"
	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
	"github.com/openbotstack/openbotstack-runtime/toolrunner"
)

// InnerLoop executes a single task through iterative reasoning turns.
type InnerLoop interface {
	Run(ctx context.Context, task TaskInput, ec *execution.ExecutionContext) (*TaskResult, error)
}

// DefaultInnerLoop implements the reasoning turn loop.
type DefaultInnerLoop struct {
	config         InnerLoopConfig
	planner        planner.ExecutionPlanner
	toolRunner     toolrunner.ToolRunner
	skillExecutor  execution.SkillExecutor
	compactor      ContextCompactor
	stopEvaluator  *InnerStopEvaluator
	logger         execution.ExecutionLogger
	currentState   InnerState
}

// NewDefaultInnerLoop creates a new DefaultInnerLoop.
func NewDefaultInnerLoop(
	config InnerLoopConfig,
	plannerExec planner.ExecutionPlanner,
	toolRunner toolrunner.ToolRunner,
	compactor ContextCompactor,
	logger execution.ExecutionLogger,
) *DefaultInnerLoop {
	return &DefaultInnerLoop{
		config:        config,
		planner:       plannerExec,
		toolRunner:    toolRunner,
		compactor:     compactor,
		stopEvaluator: NewInnerStopEvaluator(config),
		logger:        logger,
	}
}

// State returns the current inner loop state for observability.
func (l *DefaultInnerLoop) State() InnerState {
	return l.currentState
}

// SetSkillExecutor configures the executor for skill steps.
// If not set, skill steps in plans will be skipped with a warning.
func (l *DefaultInnerLoop) SetSkillExecutor(se execution.SkillExecutor) {
	l.skillExecutor = se
}

// Run executes the iterative reasoning loop until a stop condition is met.
func (l *DefaultInnerLoop) Run(ctx context.Context, task TaskInput, ec *execution.ExecutionContext) (*TaskResult, error) {
	if ec == nil {
		return nil, fmt.Errorf("inner_loop: ExecutionContext cannot be nil")
	}

	startTime := time.Now()
	result := &TaskResult{
		TurnResults: make([]TurnResult, 0),
	}

	baseMemoryCount := 0
	if task.PlannerContext != nil {
		baseMemoryCount = len(task.PlannerContext.MemoryContext)
	}

	var turnsElapsed int
	var toolCallsUsed int

	for {
		// Priority 1: Check context cancellation at the start of loop iteration
		l.currentState = InnerTurnInit
			if ctx.Err() != nil {
				l.currentState = InnerDone
				result.StopReason = StopReasonContextCanceled
				result.Error = ctx.Err()
				result.Duration = time.Since(startTime)
				return result, ctx.Err()
			}

		turnStart := time.Now()
		turnsElapsed++

		turnResult := TurnResult{
			TurnNumber:      turnsElapsed,
			ActionsExecuted: make([]string, 0),
			Observations:    make([]string, 0),
		}

		// STATE: PLAN
		l.currentState = InnerPlan
		plan, err := l.planner.Plan(ctx, task.PlannerContext)
		if err != nil {
			result.StopReason = StopReasonError
			result.Error = err
			result.Duration = time.Since(startTime)
			l.currentState = InnerDone
			return result, err
		}

		plannerStopped := len(plan.Steps) == 0
		if !plannerStopped {
			turnResult.PlanText = fmt.Sprintf("Planned %d steps", len(plan.Steps))
		}

		// STATE: ACT
		l.currentState = InnerAct
		turnToolCalls := 0
		var actErr error
		for _, step := range plan.Steps {
			if ctx.Err() != nil {
				// Defect 3 Fix: Don't lose partial turn data
				turnResult.StopReason = StopReasonContextCanceled
				result.TurnResults = append(result.TurnResults, turnResult)
				result.TurnCount = turnsElapsed
				result.ToolCallsUsed = toolCallsUsed
				result.Duration = time.Since(startTime)
				return result, ctx.Err()
			}

			if step.Type == execution.StepTypeTool {
				if l.toolRunner == nil {
					slog.Warn("tool step skipped: no tool runner configured", "tool", step.Name)
					continue
				}
				toolCallsUsed++
				turnToolCalls++

				// Log tool execution start
				if l.logger != nil {
					if err := l.logger.LogStep(ctx, execution.ExecutionLogRecord{
						StepName:  step.Name,
						StepType:  string(step.Type),
						Status:    "running",
						Timestamp: time.Now(),
					}); err != nil {
						slog.Warn("audit log failed", "error", err)
					}
				}

				toolRes, err := l.toolRunner.Execute(ctx, step.Name, step.Arguments, ec)
				if err != nil {
					// Log tool execution error
					if l.logger != nil {
						if logErr := l.logger.LogStep(ctx, execution.ExecutionLogRecord{
							StepName:  step.Name,
							StepType:  string(step.Type),
							Status:    "failed",
							Error:     err.Error(),
							Timestamp: time.Now(),
						}); logErr != nil {
							slog.Warn("audit log failed", "error", logErr)
						}
					}
					actErr = fmt.Errorf("tool execution failed: %w", err)
					break // Stop executing current plan on tool failure
				}

				// Log tool execution success
				if l.logger != nil {
					if logErr := l.logger.LogStep(ctx, execution.ExecutionLogRecord{
						StepName:  step.Name,
						StepType:  string(step.Type),
						Status:    "success",
						Timestamp: time.Now(),
					}); logErr != nil {
						slog.Warn("audit log failed", "error", logErr)
					}
				}

				turnResult.ActionsExecuted = append(turnResult.ActionsExecuted, step.Name)
				turnResult.Observations = append(turnResult.Observations, fmt.Sprintf("%v", toolRes))
			} else if step.Type == execution.StepTypeSkill {
				if l.skillExecutor == nil {
					slog.Warn("skill step skipped: no skill executor configured", "skill", step.Name)
					continue
				}

				inputBytes, _ := step.ArgumentsJSON()
				skillRes, err := l.skillExecutor.Execute(ctx, execution.ExecutionRequest{
					SkillID: step.Name,
					Input:   inputBytes,
				})
				if err != nil {
					if l.logger != nil {
						if logErr := l.logger.LogStep(ctx, execution.ExecutionLogRecord{
							StepName:  step.Name,
							StepType:  string(step.Type),
							Status:    "failed",
							Error:     err.Error(),
							Timestamp: time.Now(),
						}); logErr != nil {
							slog.Warn("audit log failed", "error", logErr)
						}
					}
					actErr = fmt.Errorf("skill execution failed: %w", err)
					break
				}

				if l.logger != nil {
					if logErr := l.logger.LogStep(ctx, execution.ExecutionLogRecord{
						StepName:  step.Name,
						StepType:  string(step.Type),
						Status:    "success",
						Timestamp: time.Now(),
					}); logErr != nil {
						slog.Warn("audit log failed", "error", logErr)
					}
				}

				turnResult.ActionsExecuted = append(turnResult.ActionsExecuted, step.Name)
				if skillRes != nil {
					turnResult.Observations = append(turnResult.Observations, string(skillRes.Output))
				}
			} else {
				actErr = fmt.Errorf("unknown step type %q for step %q", step.Type, step.Name)
				break
			}
		}

		turnResult.ToolCallsUsed = turnToolCalls
		turnResult.Duration = time.Since(turnStart)

		// Check for ACT errors (we bubble them up strictly for now)
		if actErr != nil {
			result.StopReason = StopReasonError
			result.Error = actErr
			result.TurnResults = append(result.TurnResults, turnResult)
			result.TurnCount = turnsElapsed
			result.ToolCallsUsed = toolCallsUsed
			result.Duration = time.Since(startTime)
			l.currentState = InnerDone
			return result, actErr
		}

		// STATE: OBSERVE & CHECK_STOP
		l.currentState = InnerObserve
		// Update planner context with new observations before the next turn
		// For V1, we simply append observations to the MemoryContext string
		if len(turnResult.Observations) > 0 && task.PlannerContext != nil {
			obsStr := fmt.Sprintf("Turn %d observations: %v", turnsElapsed, turnResult.Observations)
			task.PlannerContext.MemoryContext = append(task.PlannerContext.MemoryContext, assistant.SearchResult{
				Content: []byte(obsStr),
				Score:   1.0,
			})
		}

		// Evaluate stop conditions
		l.currentState = InnerCheckStop
		stopCond := l.stopEvaluator.Evaluate(turnsElapsed, toolCallsUsed, startTime, plannerStopped, ctx)
		turnResult.StopReason = stopCond.Reason
		result.TurnResults = append(result.TurnResults, turnResult)

		if stopCond.Stopped {
			l.currentState = InnerDone
			result.StopReason = stopCond.Reason
			result.TurnCount = turnsElapsed
			result.ToolCallsUsed = toolCallsUsed
			result.Duration = time.Since(startTime)
			
			// Optional: compact the final result before returning
			if l.compactor != nil {
				if compacted, err := l.compactor.Compact(ctx, result.TurnResults); err == nil {
					result.TurnResults = compacted
				}
			}

			// If context is canceled deeply during evaluator Check
			if stopCond.Reason == StopReasonContextCanceled {
				result.Error = ctx.Err()
				return result, ctx.Err()
			}
			return result, nil
		}

		// STATE: NEXT_TURN (Compact context)
		l.currentState = InnerNextTurn
		if l.compactor != nil {
			compacted, err := l.compactor.Compact(ctx, result.TurnResults)
			if err != nil {
				// Non-fatal, just log and continue with uncompacted context
				if l.logger != nil {
					if logErr := l.logger.LogStep(ctx, execution.ExecutionLogRecord{
						StepName:  "context_compaction",
						Status:    "failed",
						Error:     err.Error(),
						Timestamp: time.Now(),
					}); logErr != nil {
						slog.Warn("audit log failed", "error", logErr)
					}
				}
			} else {
				result.TurnResults = compacted

				// Defect 1 Fix: Synchronize MemoryContext with compacted turns
				if task.PlannerContext != nil {
					// Restore original long-term memory prefix
					task.PlannerContext.MemoryContext = task.PlannerContext.MemoryContext[:baseMemoryCount]
					// Re-inject observations only from the retained turns
					for _, tr := range compacted {
						if len(tr.Observations) > 0 {
							obsStr := fmt.Sprintf("Turn %d observations: %v", tr.TurnNumber, tr.Observations)
							task.PlannerContext.MemoryContext = append(task.PlannerContext.MemoryContext, assistant.SearchResult{
								Content: []byte(obsStr),
								Score:   1.0,
							})
						}
					}
				}
			}
		}
	}
}
