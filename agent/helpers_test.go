package agent

import (
	"testing"
	"time"

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

	// Verify the canonical skillToDescriptor function works via capability.SkillToDescriptor.
	desc := skillToDescriptor("test", &mockSkillForHelper{id: "h1", name: "Helper", desc: "test skill"})
	if desc.ID != "h1" {
		t.Errorf("skillToDescriptor ID = %q, want %q", desc.ID, "h1")
	}
	if desc.Name != "Helper" {
		t.Errorf("skillToDescriptor Name = %q, want %q", desc.Name, "Helper")
	}
}

func TestSkillDescriptorDirectPassThrough(t *testing.T) {
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

// mockSkillForHelper is a minimal Skill implementation for testing skillToDescriptor.
type mockSkillForHelper struct {
	id, name, desc string
}

func (m *mockSkillForHelper) ID() string          { return m.id }
func (m *mockSkillForHelper) Name() string        { return m.name }
func (m *mockSkillForHelper) Description() string { return m.desc }
func (m *mockSkillForHelper) InputSchema() *skills.JSONSchema {
	return &skills.JSONSchema{Type: "object"}
}
func (m *mockSkillForHelper) OutputSchema() *skills.JSONSchema       { return nil }
func (m *mockSkillForHelper) RequiredPermissions() []string          { return nil }
func (m *mockSkillForHelper) Timeout() time.Duration                 { return 0 }
func (m *mockSkillForHelper) Validate() error                        { return nil }
