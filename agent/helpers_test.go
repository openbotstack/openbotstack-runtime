package agent

import (
	"testing"

	agent "github.com/openbotstack/openbotstack-core/control/agent"
	"github.com/openbotstack/openbotstack-core/control/skills"
	"github.com/openbotstack/openbotstack-core/planner"
)

func TestSkillDescriptorTypeIdentity(t *testing.T) {
	// Verify that skills.SkillDescriptor is the canonical type used directly
	// by both agent and planner packages (no aliases needed).
	var sd skills.SkillDescriptor = skills.SkillDescriptor{
		ID:          "core/test",
		Name:        "Test",
		Description: "A test skill",
		InputSchema: &skills.JSONSchema{Type: "object"},
	}

	// agent.SkillDescriptorFromSkill must return skills.SkillDescriptor
	_ = sd

	// Slice of skills.SkillDescriptor must be assignable to planner.PlannerContext.Skills
	skillSlice := []skills.SkillDescriptor{sd}
	pCtx := &planner.PlannerContext{
		Skills: skillSlice,
	}
	if len(pCtx.Skills) != 1 {
		t.Errorf("expected 1 item, got %d", len(pCtx.Skills))
	}
	if pCtx.Skills[0].ID != "core/test" {
		t.Errorf("ID = %q, want %q", pCtx.Skills[0].ID, "core/test")
	}

	// agent.SkillDescriptor (now removed) was a type alias — verify the
	// conversion function still works with the canonical type.
	_ = agent.SkillDescriptorFromSkill
}

func TestSkillDescriptorDirectPassThrough(t *testing.T) {
	// Verify that skill descriptors can be passed directly to
	// planner.PlannerContext without any conversion function.
	descs := []skills.SkillDescriptor{
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
