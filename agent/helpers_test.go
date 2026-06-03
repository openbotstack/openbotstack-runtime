package agent

import (
	"testing"
	"time"

	aitypes "github.com/openbotstack/openbotstack-core/ai/types"
	"github.com/openbotstack/openbotstack-core/planner"
	"github.com/openbotstack/openbotstack-core/registry/skills"
)

func TestSkillDescriptorTypeIdentity(t *testing.T) {
	// Verify that aitypes.SkillDescriptor is the canonical type used directly
	// by both agent and planner packages (no aliases needed).
	var sd aitypes.SkillDescriptor = aitypes.SkillDescriptor{
		ID:          "core/test",
		Name:        "Test",
		Description: "A test skill",
		InputSchema: &aitypes.JSONSchema{Type: "object"},
	}

	_ = sd

	// Slice of aitypes.SkillDescriptor must be assignable to planner.PlannerContext.Skills
	skillSlice := []aitypes.SkillDescriptor{sd}
	pCtx := &planner.PlannerContext{
		Skills: skillSlice,
	}
	if len(pCtx.Skills) != 1 {
		t.Errorf("expected 1 item, got %d", len(pCtx.Skills))
	}
	if pCtx.Skills[0].ID != "core/test" {
		t.Errorf("ID = %q, want %q", pCtx.Skills[0].ID, "core/test")
	}

	// Verify the canonical skillToDescriptor function works via skills.GetDescriptor.
	desc := skillToDescriptor("test", &mockSkillForHelper{id: "h1", name: "Helper", desc: "test skill"})
	if desc.ID != "h1" {
		t.Errorf("skillToDescriptor ID = %q, want %q", desc.ID, "h1")
	}
	if desc.Name != "Helper" {
		t.Errorf("skillToDescriptor Name = %q, want %q", desc.Name, "Helper")
	}
}

func TestSkillDescriptorDirectPassThrough(t *testing.T) {
	descs := []aitypes.SkillDescriptor{
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

// TestSkillToDescriptorUsesGetDescriptor verifies that the unified skillToDescriptor
// path goes through skills.GetDescriptor (which checks DescriptorProvider).
func TestSkillToDescriptorUsesGetDescriptor(t *testing.T) {
	// A skill that implements DescriptorProvider
	s := &descriptorProviderSkill{
		id:   "custom/skill",
		name: "Custom",
		desc: "Custom skill",
		descriptor: aitypes.SkillDescriptor{
			ID:          "custom/skill",
			Name:        "Custom",
			Description: "Custom skill",
			Kind:        "skill",
			SourceID:    "custom/skill",
		},
	}
	got := skillToDescriptor("custom/skill", s)
	if got.Kind != "skill" {
		t.Errorf("Kind = %q, want %q", got.Kind, "skill")
	}
	if got.SourceID != "custom/skill" {
		t.Errorf("SourceID = %q, want %q", got.SourceID, "custom/skill")
	}
}

// descriptorProviderSkill implements both skills.Skill and skills.DescriptorProvider.
type descriptorProviderSkill struct {
	id, name, desc string
	descriptor     aitypes.SkillDescriptor
}

func (s *descriptorProviderSkill) ID() string                        { return s.id }
func (s *descriptorProviderSkill) Name() string                      { return s.name }
func (s *descriptorProviderSkill) Description() string               { return s.desc }
func (s *descriptorProviderSkill) InputSchema() *aitypes.JSONSchema  { return nil }
func (s *descriptorProviderSkill) OutputSchema() *aitypes.JSONSchema { return nil }
func (s *descriptorProviderSkill) RequiredPermissions() []string     { return nil }
func (s *descriptorProviderSkill) Timeout() time.Duration            { return 0 }
func (s *descriptorProviderSkill) Validate() error                   { return nil }
func (s *descriptorProviderSkill) Descriptor() aitypes.SkillDescriptor {
	return s.descriptor
}

// Verify interface compliance
var _ skills.DescriptorProvider = (*descriptorProviderSkill)(nil)

// mockSkillForHelper is a minimal Skill implementation for testing skillToDescriptor.
type mockSkillForHelper struct {
	id, name, desc string
}

func (m *mockSkillForHelper) ID() string          { return m.id }
func (m *mockSkillForHelper) Name() string        { return m.name }
func (m *mockSkillForHelper) Description() string { return m.desc }
func (m *mockSkillForHelper) InputSchema() *aitypes.JSONSchema {
	return &aitypes.JSONSchema{Type: "object"}
}
func (m *mockSkillForHelper) OutputSchema() *aitypes.JSONSchema       { return nil }
func (m *mockSkillForHelper) RequiredPermissions() []string          { return nil }
func (m *mockSkillForHelper) Timeout() time.Duration                 { return 0 }
func (m *mockSkillForHelper) Validate() error                        { return nil }
