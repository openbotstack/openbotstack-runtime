package agent

import (
	"context"
	"encoding/json"
	"strings"
)

// MockPlanner is a test implementation of Planner.
// It uses simple heuristics instead of an LLM for deterministic testing.
type MockPlanner struct {
	// DefaultSkillID is returned when no heuristic matches.
	DefaultSkillID string

	// ForcedPlan overrides all logic and returns this plan if set.
	ForcedPlan *ExecutionPlan

	// ForcedError causes Plan to return this error if set.
	ForcedError error
}

// NewMockPlanner creates a mock planner with the given default skill.
func NewMockPlanner(defaultSkillID string) *MockPlanner {
	return &MockPlanner{DefaultSkillID: defaultSkillID}
}

// Plan implements Planner with simple keyword-based heuristics.
func (p *MockPlanner) Plan(ctx context.Context, req PlanRequest) (*ExecutionPlan, error) {
	if p.ForcedError != nil {
		return nil, p.ForcedError
	}

	if p.ForcedPlan != nil {
		return p.ForcedPlan, nil
	}

	if len(req.AvailableSkills) == 0 {
		return nil, ErrNoSkillsAvailable
	}

	// Simple keyword matching for tests (NOT for production)
	msg := strings.ToLower(req.UserMessage)
	var selectedSkill string
	arguments := make(map[string]any)

	for _, skill := range req.AvailableSkills {
		// Match by keyword in skill ID or description
		skillLower := strings.ToLower(skill.ID + " " + skill.Description)

		// Check for keyword overlap
		if strings.Contains(msg, "summarize") && strings.Contains(skillLower, "summarize") {
			selectedSkill = skill.ID
			arguments["text"] = req.UserMessage
			arguments["max_length"] = 200
			break
		}
		if strings.Contains(msg, "tax") && strings.Contains(skillLower, "tax") {
			selectedSkill = skill.ID
			arguments["query"] = req.UserMessage
			break
		}
		if strings.Contains(msg, "hello") && strings.Contains(skillLower, "hello") {
			selectedSkill = skill.ID
			break
		}
		if strings.Contains(msg, "sentiment") && strings.Contains(skillLower, "sentiment") {
			selectedSkill = skill.ID
			arguments["text"] = req.UserMessage
			break
		}
	}

	// Fallback to default or first available skill
	if selectedSkill == "" {
		if p.DefaultSkillID != "" {
			selectedSkill = p.DefaultSkillID
		} else {
			selectedSkill = req.AvailableSkills[0].ID
		}
		arguments["input"] = req.UserMessage
	}

	return &ExecutionPlan{
		SkillID:   selectedSkill,
		Arguments: arguments,
		Reasoning: "MockPlanner: selected based on keyword matching",
	}, nil
}

// MockLLMClient is a test implementation of LLMClient.
type MockLLMClient struct {
	// Responses maps prompts (or substrings) to responses.
	Responses map[string]string

	// DefaultResponse is returned when no match is found.
	DefaultResponse string

	// ForcedError causes Generate to return this error if set.
	ForcedError error
}

// NewMockLLMClient creates a mock LLM client.
func NewMockLLMClient() *MockLLMClient {
	return &MockLLMClient{
		Responses: make(map[string]string),
	}
}

// Generate implements LLMClient.
func (c *MockLLMClient) Generate(ctx context.Context, prompt string) (string, error) {
	if c.ForcedError != nil {
		return "", c.ForcedError
	}

	for key, response := range c.Responses {
		if strings.Contains(prompt, key) {
			return response, nil
		}
	}

	if c.DefaultResponse != "" {
		return c.DefaultResponse, nil
	}

	// Generate a default valid response
	plan := ExecutionPlan{
		SkillID:   "core/default",
		Arguments: map[string]any{"input": "test"},
		Reasoning: "mock response",
	}
	bytes, _ := json.Marshal(plan)
	return string(bytes), nil
}
