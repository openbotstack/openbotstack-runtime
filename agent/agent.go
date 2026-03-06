package agent

import (
	"context"
	"fmt"

	"github.com/openbotstack/openbotstack-core/runtime"
	"github.com/openbotstack/openbotstack-core/skill"
)

// Agent orchestrates the planning and execution of skills.
//
// The Agent lifecycle:
//  1. Receives MessageRequest from Router
//  2. Gathers available skills from registry
//  3. Delegates to Planner for skill selection (LLM call)
//  4. Receives ExecutionPlan from Planner
//  5. Forwards plan to Executor
//  6. Returns MessageResponse to Router
type Agent interface {
	// HandleMessage processes a user message and returns a response.
	HandleMessage(ctx context.Context, req MessageRequest) (*MessageResponse, error)
}

// SkillRegistry provides access to available skills.
type SkillRegistry interface {
	// List returns all registered skill IDs.
	List() []string

	// Get retrieves a skill by ID.
	Get(id string) (skill.Skill, error)
}

// PlanExecutor executes validated execution plans.
type PlanExecutor interface {
	// ExecuteFromPlan runs a skill based on the execution plan.
	ExecuteFromPlan(ctx context.Context, plan *ExecutionPlan, meta ExecutionMeta) (*runtime.ExecutionResult, error)
}

// ExecutionMeta contains metadata for execution tracking.
type ExecutionMeta struct {
	TenantID  string
	UserID    string
	SessionID string
	RequestID string
}

// DefaultAgent is the standard Agent implementation.
type DefaultAgent struct {
	planner  Planner
	executor PlanExecutor
	registry SkillRegistry
}

// NewDefaultAgent creates a new Agent with the given dependencies.
func NewDefaultAgent(planner Planner, executor PlanExecutor, registry SkillRegistry) *DefaultAgent {
	return &DefaultAgent{
		planner:  planner,
		executor: executor,
		registry: registry,
	}
}

// HandleMessage implements Agent.
func (a *DefaultAgent) HandleMessage(ctx context.Context, req MessageRequest) (*MessageResponse, error) {
	// Step 1: Gather available skills
	skillDescriptors, err := a.gatherSkillDescriptors()
	if err != nil {
		return nil, fmt.Errorf("agent: failed to gather skills: %w", err)
	}

	if len(skillDescriptors) == 0 {
		return nil, ErrNoSkillsAvailable
	}

	// Step 2: Plan via LLM
	planReq := PlanRequest{
		UserMessage:         req.Message,
		AvailableSkills:     skillDescriptors,
		ConversationHistory: nil, // TODO: inject from session
	}

	plan, err := a.planner.Plan(ctx, planReq)
	if err != nil {
		return nil, fmt.Errorf("agent: planning failed: %w", err)
	}

	// Step 3: Validate plan
	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("agent: invalid plan: %w", err)
	}

	// Step 4: Execute via Executor
	meta := ExecutionMeta{
		TenantID:  req.TenantID,
		UserID:    req.UserID,
		SessionID: req.SessionID,
	}

	result, err := a.executor.ExecuteFromPlan(ctx, plan, meta)
	if err != nil {
		return &MessageResponse{
			SessionID: req.SessionID,
			Message:   fmt.Sprintf("Error executing skill %s: %v", plan.SkillID, err),
			SkillUsed: plan.SkillID,
			Plan:      plan,
		}, err
	}

	// Step 5: Build response
	return &MessageResponse{
		SessionID: req.SessionID,
		Message:   string(result.Output),
		SkillUsed: plan.SkillID,
		Plan:      plan,
	}, nil
}

// gatherSkillDescriptors converts registered skills to descriptors for the planner.
func (a *DefaultAgent) gatherSkillDescriptors() ([]SkillDescriptor, error) {
	skillIDs := a.registry.List()
	descriptors := make([]SkillDescriptor, 0, len(skillIDs))

	for _, id := range skillIDs {
		s, err := a.registry.Get(id)
		if err != nil {
			continue // skip unavailable skills
		}
		descriptors = append(descriptors, SkillDescriptorFromSkill(s))
	}

	return descriptors, nil
}
