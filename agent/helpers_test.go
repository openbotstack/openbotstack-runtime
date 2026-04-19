package agent

import (
	"testing"

	agent "github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-core/control/skills"
	"github.com/openbotstack/openbotstack-core/planner"
)

func TestAgentSkillToPlannerSkill_Empty(t *testing.T) {
	result := agentSkillToPlannerSkill(nil)
	if result == nil {
		t.Fatal("expected non-nil slice")
	}
	if len(result) != 0 {
		t.Errorf("expected 0 items, got %d", len(result))
	}
}

func TestAgentSkillToPlannerSkill_Conversion(t *testing.T) {
	input := []agent.SkillDescriptor{
		{
			ID:          "core/summarize",
			Name:        "Summarize",
			Description: "Summarizes text",
			InputSchema: &skills.JSONSchema{
				Type: "object",
				Properties: map[string]*skills.JSONSchema{
					"text": {Type: "string"},
				},
			},
		},
		{
			ID:          "core/math-add",
			Name:        "Math Add",
			Description: "Adds two numbers",
		},
	}

	result := agentSkillToPlannerSkill(input)

	if len(result) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result))
	}

	// Verify first skill
	if result[0].ID != "core/summarize" {
		t.Errorf("ID = %q, want %q", result[0].ID, "core/summarize")
	}
	if result[0].Name != "Summarize" {
		t.Errorf("Name = %q, want %q", result[0].Name, "Summarize")
	}
	if result[0].Description != "Summarizes text" {
		t.Errorf("Description = %q, want %q", result[0].Description, "Summarizes text")
	}
	if result[0].InputSchema == nil {
		t.Error("InputSchema should not be nil")
	}
	if result[0].InputSchema.Type != "object" {
		t.Errorf("InputSchema.Type = %q, want %q", result[0].InputSchema.Type, "object")
	}

	// Verify second skill
	if result[1].ID != "core/math-add" {
		t.Errorf("ID = %q, want %q", result[1].ID, "core/math-add")
	}
	if result[1].InputSchema != nil {
		t.Error("InputSchema should be nil when not set")
	}
}

func TestAgentSkillToPlannerSkill_TypeCompatibility(t *testing.T) {
	// Verify the result type is planner.SkillDescriptor
	input := []agent.SkillDescriptor{
		{ID: "test", Name: "Test", Description: "A test"},
	}

	var _ []planner.SkillDescriptor = agentSkillToPlannerSkill(input)
}
