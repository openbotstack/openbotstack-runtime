package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Planner uses an LLM to analyze user intent and select appropriate skills.
//
// The Planner is the ONLY component that decides which skill to invoke.
// It produces a structured ExecutionPlan that the Executor will run.
type Planner interface {
	// Plan analyzes user intent and produces an execution plan.
	// Returns an ExecutionPlan specifying which skill to call and with what arguments.
	Plan(ctx context.Context, req PlanRequest) (*ExecutionPlan, error)
}

// LLMClient defines the interface for LLM interactions.
type LLMClient interface {
	// Generate sends a prompt and returns the completion.
	Generate(ctx context.Context, prompt string) (string, error)
}

// LLMPlanner implements Planner using an LLM for skill selection.
type LLMPlanner struct {
	client LLMClient
}

// NewLLMPlanner creates a new LLM-based planner.
func NewLLMPlanner(client LLMClient) *LLMPlanner {
	return &LLMPlanner{client: client}
}

// Plan implements Planner.
func (p *LLMPlanner) Plan(ctx context.Context, req PlanRequest) (*ExecutionPlan, error) {
	if len(req.AvailableSkills) == 0 {
		return nil, ErrNoSkillsAvailable
	}

	// Build the prompt for the LLM
	prompt := p.buildPrompt(req)

	// Call the LLM
	response, err := p.client.Generate(ctx, prompt)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPlanningFailed, err)
	}

	// Parse the LLM response into an ExecutionPlan
	plan, err := p.parseResponse(response)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to parse LLM response: %v", ErrPlanningFailed, err)
	}

	if err := plan.Validate(); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrPlanningFailed, err)
	}

	return plan, nil
}

// buildPrompt constructs the LLM prompt for skill selection.
func (p *LLMPlanner) buildPrompt(req PlanRequest) string {
	var sb strings.Builder

	sb.WriteString("You are an AI assistant that selects the most appropriate skill to handle a user request.\n\n")

	sb.WriteString("Available skills:\n")
	for _, skill := range req.AvailableSkills {
		_, _ = fmt.Fprintf(&sb, "- %s (%s): %s\n", skill.ID, skill.Name, skill.Description)
		if skill.InputSchema != nil {
			schemaJSON, _ := json.Marshal(skill.InputSchema)
			_, _ = fmt.Fprintf(&sb, "  Input schema: %s\n", string(schemaJSON))
		}
	}

	sb.WriteString("\nUser message: ")
	sb.WriteString(req.UserMessage)
	sb.WriteString("\n\n")

	sb.WriteString(`Respond with a JSON object containing:
1. "skill_id": the ID of the skill to use
2. "arguments": a JSON object with the skill's input arguments
3. "reasoning": brief explanation of why this skill was chosen

Example response:
{"skill_id": "core/summarize", "arguments": {"text": "...", "max_length": 200}, "reasoning": "User wants to summarize text"}

Respond ONLY with the JSON object, no other text.`)

	return sb.String()
}

// parseResponse extracts an ExecutionPlan from the LLM response.
func (p *LLMPlanner) parseResponse(response string) (*ExecutionPlan, error) {
	// Clean up the response - LLMs sometimes wrap JSON in markdown
	response = strings.TrimSpace(response)
	response = strings.TrimPrefix(response, "```json")
	response = strings.TrimPrefix(response, "```")
	response = strings.TrimSuffix(response, "```")
	response = strings.TrimSpace(response)

	var plan ExecutionPlan
	if err := json.Unmarshal([]byte(response), &plan); err != nil {
		return nil, fmt.Errorf("invalid JSON: %w (response: %s)", err, response)
	}

	return &plan, nil
}
