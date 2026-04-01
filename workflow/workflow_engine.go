package workflow

import (
	"context"
	"fmt"
	"sync"

	"github.com/google/uuid"
	"github.com/openbotstack/openbotstack-core/execution"
)

// WorkflowEngine manages the lifecycle of multiple execution workflows.
type WorkflowEngine struct {
	mu        sync.RWMutex
	workflows map[string]*WorkflowState
	executor  execution.SkillExecutor
	logger    execution.ExecutionLogger
}

// NewWorkflowEngine creates a new workflow engine.
func NewWorkflowEngine(executor execution.SkillExecutor, logger execution.ExecutionLogger) *WorkflowEngine {
	return &WorkflowEngine{
		workflows: make(map[string]*WorkflowState),
		executor:  executor,
		logger:    logger,
	}
}

// StartWorkflow initiates a new execution from a plan.
func (e *WorkflowEngine) StartWorkflow(ctx context.Context, assistantID string, plan *execution.ExecutionPlan) (string, error) {
	workflowID := uuid.New().String()
	
	state := NewWorkflowState(workflowID, assistantID, plan)
	
	e.mu.Lock()
	e.workflows[workflowID] = state
	e.mu.Unlock()
	
	runner := NewWorkflowRunner(e.executor, e.logger)
	
	// In the request-scoped ephemeral model, we execute synchronously 
	// but the engine tracks the state for observability/logs.
	err := runner.Run(ctx, state)
	if err != nil {
		return workflowID, fmt.Errorf("workflow execution failed: %w", err)
	}
	
	return workflowID, nil
}

// GetWorkflow retrieves the state of a workflow by ID.
func (e *WorkflowEngine) GetWorkflow(id string) (*WorkflowState, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	
	state, ok := e.workflows[id]
	return state, ok
}
