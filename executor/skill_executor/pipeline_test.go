package skill_executor_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/control/skills"
	"github.com/openbotstack/openbotstack-core/execution"
	registry "github.com/openbotstack/openbotstack-core/registry/skills"
	skilloperator "github.com/openbotstack/openbotstack-runtime/executor/skill_executor"
)

const skillsDir = "../../skills"

// System default skills that must always be present.
var systemSkills = []string{"summarize", "extract_structured_data", "classify"}

// --- Manifest → Registry Pipeline ---

func TestPipeline_ManifestParse_SystemDefaults(t *testing.T) {
	for _, dir := range systemSkills {
		t.Run(dir, func(t *testing.T) {
			manifestPath := filepath.Join(skillsDir, dir, "manifest.yaml")
			data, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatalf("read manifest: %v", err)
			}
			m, err := registry.ParseManifest(data)
			if err != nil {
				t.Fatalf("parse manifest: %v", err)
			}
			if err := m.Validate(); err != nil {
				t.Fatalf("validate manifest: %v", err)
			}
			if m.Execution.Mode == "" {
				t.Error("execution.mode should not be empty")
			}
		})
	}
}

func TestPipeline_ManifestToRegistry(t *testing.T) {
	reg := registry.NewInMemoryRegistry()
	for _, dir := range systemSkills {
		skillDir := filepath.Join(skillsDir, dir)
		skillID := registry.DeriveSkillID(skillDir)

		smd, err := registry.ParseSkillMD(skillDir)
		if err != nil {
			t.Fatalf("parse SKILL.md %s: %v", dir, err)
		}

		var m *registry.SkillManifest
		manifestPath := filepath.Join(skillDir, "manifest.yaml")
		if data, err := os.ReadFile(manifestPath); err == nil {
			m, err = registry.ParseManifest(data)
			if err != nil {
				t.Fatalf("parse manifest %s: %v", dir, err)
			}
		}

		name := dir
		if smd != nil && smd.Name != "" {
			name = smd.Name
		}
		var desc string
		if smd != nil {
			desc = smd.Description
		}

		s := &manifestSkill{id: skillID, name: name, description: desc}
		if m != nil {
			s.inputSchema = m.InputSchema
			s.outputSchema = m.OutputSchema
		}
		if err := reg.Register(s); err != nil {
			t.Fatalf("register %s: %v", skillID, err)
		}
	}

	ids := reg.List()
	if len(ids) != len(systemSkills) {
		t.Errorf("expected %d skills, got %d", len(systemSkills), len(ids))
	}

	for _, dir := range systemSkills {
		skillDir := filepath.Join(skillsDir, dir)
		skillID := registry.DeriveSkillID(skillDir)
		if _, err := reg.Get(skillID); err != nil {
			t.Errorf("get %s: %v", skillID, err)
		}
	}
}

// --- Declarative Skill Pipeline ---

func TestPipeline_DeclarativeSkill_Summarize(t *testing.T) {
	e := skilloperator.NewDefaultExecutor()
	e.SetTextGenerator(&pipelineMockTextGen{response: "Summary: key point 1, key point 2"})

	s := &manifestSkill{
		id:            "summarize",
		name:          "Text Summarizer",
		description:   "Summarizes text into concise bullet points",
		executionMode: "declarative",
	}
	if err := e.LoadSkill(context.Background(), s); err != nil {
		t.Fatalf("load: %v", err)
	}

	input, _ := json.Marshal(map[string]string{"text": "Long text about AI..."})
	result, err := e.Execute(context.Background(), execution.ExecutionRequest{
		SkillID: "summarize",
		Input:   input,
	})
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if result.Status != execution.StatusSuccess {
		t.Fatalf("status = %q, error = %q", result.Status, result.Error)
	}
	if len(result.Output) == 0 {
		t.Error("expected non-empty output from declarative skill")
	}
}

func TestPipeline_DeclarativeSkill_SystemDefaultLoaded(t *testing.T) {
	skillDir := filepath.Join(skillsDir, "summarize")
	smd, err := registry.ParseSkillMD(skillDir)
	if err != nil {
		t.Fatalf("ParseSkillMD: %v", err)
	}
	if smd == nil {
		t.Fatal("summarize SKILL.md should exist")
	}
	if smd.Name == "" {
		t.Error("summarize SKILL.md should have a name")
	}
	if smd.Description == "" {
		t.Error("summarize SKILL.md should have a description")
	}

	manifestPath := filepath.Join(skillDir, "manifest.yaml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	m, err := registry.ParseManifest(data)
	if err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if m.Execution.Mode != "declarative" {
		t.Errorf("summarize mode = %q, want declarative", m.Execution.Mode)
	}
}

// --- Helpers ---

type manifestSkill struct {
	id            string
	name          string
	description   string
	inputSchema   *skills.JSONSchema
	outputSchema  *skills.JSONSchema
	executionMode string
	prompt        string
}

func (s *manifestSkill) ID() string                      { return s.id }
func (s *manifestSkill) Name() string                    { return s.name }
func (s *manifestSkill) Description() string             { return s.description }
func (s *manifestSkill) Timeout() time.Duration          { return 30 * time.Second }
func (s *manifestSkill) InputSchema() *skills.JSONSchema  { return s.inputSchema }
func (s *manifestSkill) OutputSchema() *skills.JSONSchema { return s.outputSchema }
func (s *manifestSkill) RequiredPermissions() []string   { return nil }
func (s *manifestSkill) ExecutionMode() string           { return s.executionMode }
func (s *manifestSkill) Prompt() string                  { return s.prompt }
func (s *manifestSkill) Validate() error                 { return nil }

type pipelineMockTextGen struct {
	response string
	err      error
}

func (m *pipelineMockTextGen) GenerateText(_ context.Context, _ string) (string, error) {
	return m.response, m.err
}
