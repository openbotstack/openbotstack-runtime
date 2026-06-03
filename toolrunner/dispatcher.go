package toolrunner

import (
	"context"
	"time"

	"github.com/openbotstack/openbotstack-core/execution"
)

// StepDispatcher dispatches execution steps to the appropriate handler.
// It provides a unified interface for step execution that decouples
// the executor from concrete harness implementations.
type StepDispatcher interface {
	// Dispatch executes a single step and returns the result.
	Dispatch(
		ctx context.Context,
		step *execution.ExecutionStep,
		ec *execution.ExecutionContext,
		prevResults map[string]any,
		stepTimeout time.Duration,
	) (*execution.StepResult, error)

	// DispatchFallback executes a fallback tool when retries are exhausted.
	DispatchFallback(
		ctx context.Context,
		fallbackTool string,
		arguments map[string]any,
		ec *execution.ExecutionContext,
		prevResults map[string]any,
	) (*execution.StepResult, error)
}

// StepHandler determines whether it can handle a step and executes it.
// Handlers are registered in a StepHandlerRegistry to replace prefix-based
// routing with pluggable handler chains.
type StepHandler interface {
	// CanHandle returns true if this handler should process the given step.
	CanHandle(step *execution.ExecutionStep) bool

	// Handle executes the step and returns the result.
	Handle(
		ctx context.Context,
		step *execution.ExecutionStep,
		ec *execution.ExecutionContext,
		prevResults map[string]any,
		stepTimeout time.Duration,
	) (*execution.StepResult, error)
}
