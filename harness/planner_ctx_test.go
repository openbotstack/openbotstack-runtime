package harness

import (
	"context"
	"testing"

	"github.com/openbotstack/openbotstack-core/execution"
	"github.com/openbotstack/openbotstack-core/planner"
)

func TestExecutionContext_PlannerContext_Explicit(t *testing.T) {
	pCtx := &planner.PlannerContext{
		AssistantID: "test-assistant",
		UserRequest: "hello",
	}

	ec := execution.NewExecutionContext(context.Background(), "req", "asst", "sess", "t1", "u1")
	ec.SetPlannerContext(pCtx)

	retrieved := ec.PlannerContext()
	if retrieved == nil {
		t.Fatal("PlannerContext should not be nil")
	}

	// No type assertion needed — PlannerContext() returns *planning.PlannerContext directly.
	if retrieved.AssistantID != "test-assistant" {
		t.Errorf("AssistantID = %q, want %q", retrieved.AssistantID, "test-assistant")
	}
}

func TestExecutionContext_PlannerContext_NilWhenNotSet(t *testing.T) {
	ec := execution.NewExecutionContext(context.Background(), "req", "asst", "sess", "t1", "u1")
	retrieved := ec.PlannerContext()
	if retrieved != nil {
		t.Error("PlannerContext should be nil when not set")
	}
}
