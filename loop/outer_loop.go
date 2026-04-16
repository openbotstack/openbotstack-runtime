package loop

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
)

// OuterLoop orchestrates tasks using the Dual-Loop architecture.
type OuterLoop interface {
	Run(ctx context.Context, tasks []TaskInput, ec *execution.ExecutionContext) (*WorkflowResult, error)
}

// DefaultOuterLoop implements the workflow/task orchestration loop.
type DefaultOuterLoop struct {
	config        OuterLoopConfig
	innerLoop     InnerLoop
	checkpoint    Checkpoint
	policy        PolicyCheckpoint
	stopEvaluator *OuterStopEvaluator
	logger        execution.ExecutionLogger
}

// NewDefaultOuterLoop creates a new DefaultOuterLoop.
func NewDefaultOuterLoop(
	config OuterLoopConfig,
	innerLoop InnerLoop,
	checkpoint Checkpoint,
	policy PolicyCheckpoint,
	logger execution.ExecutionLogger,
) *DefaultOuterLoop {
	return &DefaultOuterLoop{
		config:        config,
		innerLoop:     innerLoop,
		checkpoint:    checkpoint,
		policy:        policy,
		stopEvaluator: NewOuterStopEvaluator(config),
		logger:        logger,
	}
}

// Run executes the task sequence via the outer loop state machine.
func (l *DefaultOuterLoop) Run(ctx context.Context, tasks []TaskInput, ec *execution.ExecutionContext) (*WorkflowResult, error) {
	if ec == nil {
		return nil, fmt.Errorf("outer_loop: ExecutionContext cannot be nil")
	}

	startTime := time.Now()
	result := &WorkflowResult{
		TaskResults: make([]*TaskResult, 0),
		Metrics: LoopMetrics{
			// LoopMetrics doesn't have TotalTasks or StartTime
		},
	}

	for taskIdx, task := range tasks {
		// Priority 1: Check context cancellation
		if ctx.Err() != nil {
			result.StopCondition = StopCondition{Stopped: true, Reason: StopReasonContextCanceled}
			result.Metrics.TotalRuntime = time.Since(startTime)
			return result, ctx.Err()
		}

		// STATE: TASK_SELECT (already provided by loop iterator)
		if l.logger != nil {
			if err := l.logger.LogStep(ctx, execution.ExecutionLogRecord{
				StepName:  fmt.Sprintf("task_%d_start", taskIdx),
				StepType:  "workflow_step",
				Status:    "running",
				Timestamp: time.Now(),
			}); err != nil {
				slog.Warn("audit log failed", "error", err)
			}
		}

		// Evaluate policy before execution if configured
		if l.policy != nil {
			if err := l.policy.Check(ctx, taskIdx, &result.Metrics); err != nil {
				result.StopCondition = StopCondition{Stopped: true, Reason: StopReasonError}
				result.Metrics.TotalRuntime = time.Since(startTime)
				return result, fmt.Errorf("policy checkpoint failed: %w", err)
			}
		}

		// STATE: TASK_EXECUTE
		innerRes, err := l.innerLoop.Run(ctx, task, ec)
		if err != nil {
			// Inner loop error is generally fatal unhandled error in the engine
			result.StopCondition = StopCondition{Stopped: true, Reason: StopReasonError}
			result.Metrics.TotalRuntime = time.Since(startTime)
			return result, fmt.Errorf("inner loop failed: %w", err)
		}

		// Record metrics
		result.TaskResults = append(result.TaskResults, innerRes)
		result.Metrics.WorkflowSteps++
		result.Metrics.TotalTurns += innerRes.TurnCount
		result.Metrics.TotalToolCalls += innerRes.ToolCallsUsed

		// Defect 2 Fix: Check if inner loop hit a safety limit.
		// If it hit MaxTurns/MaxToolCalls/MaxRuntime, we should halt the entire workflow
		// to prevent cascading failures in dependent tasks.
		if innerRes.StopReason != StopReasonPlannerStopped {
			result.StopCondition = StopCondition{
				Stopped: true,
				Reason:  innerRes.StopReason,
				Detail:  fmt.Sprintf("task %d stopped due to %s", taskIdx, innerRes.StopReason),
			}
			result.Metrics.TotalRuntime = time.Since(startTime)
			return result, nil
		}

		if l.logger != nil {
			if err := l.logger.LogStep(ctx, execution.ExecutionLogRecord{
				StepName:  fmt.Sprintf("task_%d_end", taskIdx),
				StepType:  "workflow_step",
				Status:    "success",
				Timestamp: time.Now(),
			}); err != nil {
				slog.Warn("audit log failed", "error", err)
			}
		}

		// STATE: CHECKPOINT
		if l.checkpoint != nil {
			if err := l.checkpoint.Save(ctx, taskIdx, innerRes, &result.Metrics); err != nil {
				result.StopCondition = StopCondition{Stopped: true, Reason: StopReasonError}
				result.Metrics.TotalRuntime = time.Since(startTime)
				return result, fmt.Errorf("checkpoint save failed: %w", err)
			}
		}

		// Evaluate stop conditions after task completion
		stopCond := l.stopEvaluator.Evaluate(result.Metrics.WorkflowSteps, startTime, ctx)
		if stopCond.Stopped {
			result.StopCondition = stopCond
			result.Metrics.TotalRuntime = time.Since(startTime)
			
			if stopCond.Reason == StopReasonContextCanceled {
				return result, ctx.Err()
			}
			return result, nil
		}

		// STATE: NEXT_TASK
		// Proceed to next iteration. Context compacting happens inside the inner loop turn-by-turn.
	}

	// STATE: DONE (Goal Achieved since we finished all tasks)
	result.StopCondition = StopCondition{Stopped: true, Reason: StopReasonGoalAchieved}
	result.Metrics.TotalRuntime = time.Since(startTime)
	return result, nil
}
