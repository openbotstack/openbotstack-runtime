package workflow

import (
	"context"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
)

// WorkflowRunner manages the execution of a single workflow.
type WorkflowRunner struct {
	executor execution.SkillExecutor
	logger   execution.ExecutionLogger
}

// NewWorkflowRunner creates a new workflow runner.
func NewWorkflowRunner(executor execution.SkillExecutor, logger execution.ExecutionLogger) *WorkflowRunner {
	return &WorkflowRunner{
		executor: executor,
		logger:   logger,
	}
}

// Run executes a full plan and updates the workflow state.
func (wr *WorkflowRunner) Run(ctx context.Context, state *WorkflowState) error {
	state.Start()
	
	if wr.logger != nil {
		_ = wr.logger.LogPlanStart(ctx, state.ID, state.AssistantID, *state.Plan)
	}
	
	// Execute the plan via the skill executor
	err := wr.executor.ExecutePlan(ctx, state.Plan, state.Context)
	
	if wr.logger != nil {
		// Log all results from the context
		results := state.Context.Results()
		for _, res := range results {
			status := "success"
			errStr := ""
			if res.Error != nil {
				status = "failed"
				errStr = res.Error.Error()
			}
			
			_ = wr.logger.LogStep(ctx, execution.ExecutionLogRecord{
				RequestID:   state.ID,
				AssistantID: state.AssistantID,
				StepName:    res.StepName,
				StepType:    res.Type,
				Status:      status,
				Output:      res.Output,
				Error:       errStr,
				Duration:    res.Duration,
				Timestamp:   time.Now(),
			})
		}
		
		_ = wr.logger.LogPlanEnd(ctx, state.ID, state.AssistantID, err)
	}

	if err != nil {
		state.Fail(err)
		return err
	}
	
	state.Complete()
	return nil
}
