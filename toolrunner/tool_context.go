package toolrunner

import (
	"context"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
)

// ToolContext provides the environment for a tool to execute.
type ToolContext struct {
	context.Context
	
	TenantID  string
	UserID    string
	RequestID string
	
	StartTime time.Time
}

// NewToolContext creates a new tool context from an execution context.
func NewToolContext(ctx context.Context, ec *execution.ExecutionContext) *ToolContext {
	return &ToolContext{
		Context:   ctx,
		TenantID:  ec.TenantID,
		UserID:    ec.UserID,
		RequestID: ec.RequestID,
		StartTime: time.Now(),
	}
}

// ExecutionContext returns the underlying context.
func (c *ToolContext) ExecutionContext() context.Context {
	return c.Context
}

// Duration returns the time elapsed since the context was created.
func (c *ToolContext) Duration() time.Duration {
	return time.Since(c.StartTime)
}
