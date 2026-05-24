package agent

import (
	"testing"

	agent "github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-core/control/skills"
	"github.com/openbotstack/openbotstack-core/planner"
)

func TestSkillDescriptorTypeIdentity(t *testing.T) {
	// Verify that agent.SkillDescriptor, planner.SkillDescriptor, and
	// skills.SkillDescriptor are the same type (type aliases).
	// If this compiles, the aliases are correct.
	var sd skills.SkillDescriptor = skills.SkillDescriptor{
		ID:          "core/test",
		Name:        "Test",
		Description: "A test skill",
		InputSchema: &skills.JSONSchema{Type: "object"},
	}

	// agent.SkillDescriptor must be assignable from skills.SkillDescriptor
	var _ agent.SkillDescriptor = sd

	// planner.SkillDescriptor must be assignable from skills.SkillDescriptor
	var _ planner.SkillDescriptor = sd

	// Slice of agent.SkillDescriptor must be assignable to planner.SkillDescriptor slice
	agentSlice := []agent.SkillDescriptor{sd}
	var plannerSlice []planner.SkillDescriptor = agentSlice
	if len(plannerSlice) != 1 {
		t.Errorf("expected 1 item, got %d", len(plannerSlice))
	}
	if plannerSlice[0].ID != "core/test" {
		t.Errorf("ID = %q, want %q", plannerSlice[0].ID, "core/test")
	}
}

func TestSkillDescriptorDirectPassThrough(t *testing.T) {
	// Verify that agent skill descriptors can be passed directly to
	// planner.PlannerContext without any conversion function.
	descs := []agent.SkillDescriptor{
		{ID: "s1", Name: "Skill1", Description: "First"},
		{ID: "s2", Name: "Skill2", Description: "Second"},
	}

	pCtx := &planner.PlannerContext{
		Skills: descs,
	}

	if len(pCtx.Skills) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(pCtx.Skills))
	}
	if pCtx.Skills[0].ID != "s1" {
		t.Errorf("Skills[0].ID = %q, want %q", pCtx.Skills[0].ID, "s1")
	}
	if pCtx.Skills[1].Name != "Skill2" {
		t.Errorf("Skills[1].Name = %q, want %q", pCtx.Skills[1].Name, "Skill2")
	}
}
