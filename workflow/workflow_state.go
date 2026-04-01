package workflow

import (
	"context"
	"sync"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
)

// WorkflowStatus represents the current state of a workflow.
type WorkflowStatus string

const (
	StatusPending   WorkflowStatus = "pending"
	StatusRunning   WorkflowStatus = "running"
	StatusCompleted WorkflowStatus = "completed"
	StatusFailed    WorkflowStatus = "failed"
	StatusCancelled WorkflowStatus = "cancelled"
)

// WorkflowState tracks the lifecycle and execution data of a workflow.
type WorkflowState struct {
	mu sync.RWMutex
	
	ID          string
	AssistantID string
	Status      WorkflowStatus
	
	StartTime   time.Time
	EndTime     time.Time
	
	Context     *execution.ExecutionContext
	Plan        *execution.ExecutionPlan
	
	Error       error
}

// NewWorkflowState creates a new state tracker for a workflow.
func NewWorkflowState(id, assistantID string, plan *execution.ExecutionPlan) *WorkflowState {
	return &WorkflowState{
		ID:          id,
		AssistantID: assistantID,
		Status:      StatusPending,
		Plan:        plan,
		Context:     execution.NewExecutionContext(context.Background(), id, assistantID, "", "admin", "system"), // Default metadata
	}
}

// Start marks the workflow as running.
func (s *WorkflowState) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = StatusRunning
	s.StartTime = time.Now()
}

// Complete marks the workflow as successful.
func (s *WorkflowState) Complete() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = StatusCompleted
	s.EndTime = time.Now()
}

// Fail marks the workflow as failed with an error.
func (s *WorkflowState) Fail(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Status = StatusFailed
	s.Error = err
	s.EndTime = time.Now()
}
