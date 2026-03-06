// Package agent provides the Agent orchestration layer for OpenBotStack runtime.
//
// The Agent is responsible for:
//   - Receiving user messages
//   - Delegating to a Planner for LLM-based skill selection
//   - Forwarding structured ExecutionPlans to the Executor
//
// The Agent does NOT:
//   - Directly select skills (that's the Planner's job)
//   - Execute skills (that's the Executor's job)
//   - Handle HTTP concerns (that's the Router's job)
package agent

import (
	"encoding/json"
	"errors"

	"github.com/openbotstack/openbotstack-core/skill"
)

// Common errors for the agent package.
var (
	// ErrNilPlan is returned when an execution plan is nil.
	ErrNilPlan = errors.New("agent: execution plan is nil")

	// ErrEmptySkillID is returned when plan has no skill ID.
	ErrEmptySkillID = errors.New("agent: execution plan has empty skill ID")

	// ErrPlanningFailed is returned when the planner fails to produce a plan.
	ErrPlanningFailed = errors.New("agent: planning failed")

	// ErrNoSkillsAvailable is returned when no skills are registered.
	ErrNoSkillsAvailable = errors.New("agent: no skills available for planning")
)

// ExecutionPlan is the structured output from LLM-based planning.
// It specifies which skill to invoke and with what arguments.
type ExecutionPlan struct {
	// SkillID is the identifier of the skill to execute.
	// Format: "namespace/name" (e.g., "core/summarize")
	SkillID string `json:"skill_id"`

	// Arguments are the structured inputs for the skill.
	// Must conform to the skill's InputSchema.
	Arguments map[string]any `json:"arguments"`

	// Reasoning explains why this skill was selected (for audit/debug).
	Reasoning string `json:"reasoning,omitempty"`
}

// Validate checks if the execution plan is valid.
func (p *ExecutionPlan) Validate() error {
	if p == nil {
		return ErrNilPlan
	}
	if p.SkillID == "" {
		return ErrEmptySkillID
	}
	return nil
}

// ArgumentsJSON returns the arguments serialized as JSON bytes.
func (p *ExecutionPlan) ArgumentsJSON() ([]byte, error) {
	if p.Arguments == nil {
		return []byte("{}"), nil
	}
	return json.Marshal(p.Arguments)
}

// SkillDescriptor describes a skill for LLM context building.
// This is passed to the Planner so the LLM knows which skills are available.
type SkillDescriptor struct {
	ID          string            `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	InputSchema *skill.JSONSchema `json:"input_schema,omitempty"`
}

// SkillDescriptorFromSkill converts a skill.Skill to a SkillDescriptor.
func SkillDescriptorFromSkill(s skill.Skill) SkillDescriptor {
	return SkillDescriptor{
		ID:          s.ID(),
		Name:        s.Name(),
		Description: s.Description(),
		InputSchema: s.InputSchema(),
	}
}

// MessageRequest represents input to the Agent.
type MessageRequest struct {
	TenantID  string `json:"tenant_id"`
	UserID    string `json:"user_id"`
	SessionID string `json:"session_id"`
	Message   string `json:"message"`
}

// MessageResponse represents output from the Agent.
type MessageResponse struct {
	SessionID string         `json:"session_id"`
	Message   string         `json:"message"`
	SkillUsed string         `json:"skill_used,omitempty"`
	Plan      *ExecutionPlan `json:"plan,omitempty"`
}

// Message represents a single chat message in conversation history.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// PlanRequest contains context for the Planner.
type PlanRequest struct {
	// UserMessage is the current user input.
	UserMessage string

	// AvailableSkills describes skills the LLM can choose from.
	AvailableSkills []SkillDescriptor

	// ConversationHistory provides context from prior messages.
	ConversationHistory []Message
}
