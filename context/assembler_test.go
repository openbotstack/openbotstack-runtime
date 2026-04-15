package context

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	corecontext "github.com/openbotstack/openbotstack-core/context"
	"github.com/openbotstack/openbotstack-core/control/skills"
	"github.com/openbotstack/openbotstack-core/memory/abstraction"
	skillregistry "github.com/openbotstack/openbotstack-core/registry/skills"
)

func TestAssemble_BasicPrompt(t *testing.T) {
	assembler := NewRuntimeContextAssembler(nil, nil)

	result, err := assembler.Assemble(
		context.Background(),
		corecontext.AssistantContext{
			ProfileID:        "test",
			BaseSystemPrompt: "You are a helpful assistant.",
			Persona: corecontext.Persona{
				Tone:      "professional",
				Verbosity: "medium",
			},
		},
		corecontext.UserRequest{Message: "Hello"},
		nil,
	)

	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}
	if result.SystemPrompt == "" {
		t.Error("expected non-empty system prompt")
	}
	if !strings.Contains(result.SystemPrompt, "You are a helpful assistant.") {
		t.Errorf("system prompt missing base prompt: %q", result.SystemPrompt)
	}
	if !strings.Contains(result.SystemPrompt, "Tone: professional") {
		t.Errorf("system prompt missing tone: %q", result.SystemPrompt)
	}
}

func TestAssemble_WithConversationHistory(t *testing.T) {
	assembler := NewRuntimeContextAssembler(nil, nil)

	history := []skills.Message{
		{Role: "user", Content: "What is Go?"},
		{Role: "assistant", Content: "Go is a programming language."},
	}

	result, err := assembler.Assemble(
		context.Background(),
		corecontext.AssistantContext{ProfileID: "test"},
		corecontext.UserRequest{Message: "Tell me more"},
		history,
	)

	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}
	if len(result.Messages) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(result.Messages))
	}
	if result.Messages[0].Content != "What is Go?" {
		t.Errorf("unexpected first message: %q", result.Messages[0].Content)
	}
}

func TestAssemble_WithSkillTools(t *testing.T) {
	registry := &mockRegistry{
		skills: map[string]mockSkill{
			"search": {
				id:          "search",
				name:        "Search",
				description: "Search the web",
				inputSchema: &skills.JSONSchema{Type: "object"},
			},
		},
	}

	assembler := NewRuntimeContextAssembler(registry, nil)

	result, err := assembler.Assemble(
		context.Background(),
		corecontext.AssistantContext{
			ProfileID:       "test",
			EnabledSkillIDs: []string{"search"},
		},
		corecontext.UserRequest{Message: "Search for cats"},
		nil,
	)

	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}
	if len(result.AvailableTools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(result.AvailableTools))
	}
	if result.AvailableTools[0].Name != "search" {
		t.Errorf("unexpected tool name: %q", result.AvailableTools[0].Name)
	}
}

func TestAssemble_EmptySkillIDs(t *testing.T) {
	assembler := NewRuntimeContextAssembler(nil, nil)

	result, err := assembler.Assemble(
		context.Background(),
		corecontext.AssistantContext{ProfileID: "test"},
		corecontext.UserRequest{Message: "Hello"},
		nil,
	)

	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}
	if len(result.AvailableTools) != 0 {
		t.Errorf("expected 0 tools, got %d", len(result.AvailableTools))
	}
}

func TestAssemble_MemoryRetrievalBestEffort(t *testing.T) {
	memoryManager := &errorMemoryManager{}
	assembler := NewRuntimeContextAssembler(nil, memoryManager)

	result, err := assembler.Assemble(
		context.Background(),
		corecontext.AssistantContext{ProfileID: "test"},
		corecontext.UserRequest{Message: "Hello"},
		nil,
	)

	if err != nil {
		t.Fatalf("Assemble should not fail when memory retrieval fails: %v", err)
	}
	if len(result.RelevantMemories) != 0 {
		t.Errorf("expected 0 memories on error, got %d", len(result.RelevantMemories))
	}
}

func TestAssemble_MemoryRetrievalSuccess(t *testing.T) {
	memoryManager := &successMemoryManager{
		entries: []abstraction.MemoryEntry{
			{Content: "User prefers dark mode"},
			{Content: "User is a developer"},
		},
	}
	assembler := NewRuntimeContextAssembler(nil, memoryManager)

	result, err := assembler.Assemble(
		context.Background(),
		corecontext.AssistantContext{ProfileID: "test"},
		corecontext.UserRequest{Message: "settings"},
		nil,
	)

	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}
	if len(result.RelevantMemories) != 2 {
		t.Fatalf("expected 2 relevant memories, got %d", len(result.RelevantMemories))
	}
	// Verify memory injected as first system message
	if len(result.Messages) < 1 {
		t.Fatal("expected at least 1 message")
	}
	if result.Messages[0].Role != "system" {
		t.Errorf("expected first message role 'system', got %q", result.Messages[0].Role)
	}
	if !strings.Contains(result.Messages[0].Content, "User prefers dark mode") {
		t.Errorf("memory not in system message: %q", result.Messages[0].Content)
	}
}

func TestAssemble_MemoryAndHistory(t *testing.T) {
	memoryManager := &successMemoryManager{
		entries: []abstraction.MemoryEntry{
			{Content: "Remembered fact"},
		},
	}
	assembler := NewRuntimeContextAssembler(nil, memoryManager)

	history := []skills.Message{
		{Role: "user", Content: "Previous question"},
		{Role: "assistant", Content: "Previous answer"},
	}

	result, err := assembler.Assemble(
		context.Background(),
		corecontext.AssistantContext{ProfileID: "test"},
		corecontext.UserRequest{Message: "follow-up"},
		history,
	)

	if err != nil {
		t.Fatalf("Assemble failed: %v", err)
	}
	// Memory system message + 2 history messages = 3
	if len(result.Messages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(result.Messages))
	}
	// First must be memory system message
	if result.Messages[0].Role != "system" {
		t.Errorf("expected first message to be system (memory), got %q", result.Messages[0].Role)
	}
	// Then history in order
	if result.Messages[1].Content != "Previous question" {
		t.Errorf("expected history message, got %q", result.Messages[1].Content)
	}
}

func TestBuildPersonaSystemPrompt_AllFields(t *testing.T) {
	prompt := buildPersonaSystemPrompt(
		corecontext.Persona{
			Tone:      "friendly",
			Verbosity: "high",
			Domain:    "cardiology",
		},
		"You are Dr. AI.",
	)

	if !strings.Contains(prompt, "You are Dr. AI.") {
		t.Errorf("missing base prompt: %q", prompt)
	}
	if !strings.Contains(prompt, "Tone: friendly") {
		t.Errorf("missing tone: %q", prompt)
	}
	if !strings.Contains(prompt, "Verbosity: high") {
		t.Errorf("missing verbosity: %q", prompt)
	}
	if !strings.Contains(prompt, "Domain: cardiology") {
		t.Errorf("missing domain: %q", prompt)
	}
}

func TestBuildPersonaSystemPrompt_EmptyPersona(t *testing.T) {
	prompt := buildPersonaSystemPrompt(
		corecontext.Persona{},
		"Base prompt only.",
	)
	if prompt != "Base prompt only." {
		t.Errorf("expected 'Base prompt only.', got %q", prompt)
	}
}

func TestBuildPersonaSystemPrompt_EmptyAll(t *testing.T) {
	prompt := buildPersonaSystemPrompt(corecontext.Persona{}, "")
	if prompt != "" {
		t.Errorf("expected empty prompt, got %q", prompt)
	}
}

// Mock implementations

// mockRegistry implements agent.SkillRegistry (List + Get).
type mockRegistry struct {
	skills map[string]mockSkill
}

func (r *mockRegistry) List() []string {
	ids := make([]string, 0, len(r.skills))
	for id := range r.skills {
		ids = append(ids, id)
	}
	return ids
}

func (r *mockRegistry) Get(id string) (skillregistry.Skill, error) {
	s, ok := r.skills[id]
	if !ok {
		return nil, fmt.Errorf("not found: %s", id)
	}
	return s, nil
}

// mockSkill implements skills.Skill.
type mockSkill struct {
	id, name, description string
	inputSchema           *skills.JSONSchema
}

func (s mockSkill) ID() string                      { return s.id }
func (s mockSkill) Name() string                    { return s.name }
func (s mockSkill) Description() string             { return s.description }
func (s mockSkill) InputSchema() *skills.JSONSchema { return s.inputSchema }
func (s mockSkill) OutputSchema() *skills.JSONSchema { return nil }
func (s mockSkill) RequiredPermissions() []string   { return nil }
func (s mockSkill) Timeout() time.Duration           { return 30 * time.Second }
func (s mockSkill) Validate() error                  { return nil }

type errorMemoryManager struct{}

func (m *errorMemoryManager) StoreShortTerm(_ context.Context, _ abstraction.MemoryEntry) error {
	return nil
}
func (m *errorMemoryManager) StoreLongTerm(_ context.Context, _ abstraction.MemoryEntry) error {
	return nil
}
func (m *errorMemoryManager) RetrieveSimilar(_ context.Context, _ string, _ int) ([]abstraction.MemoryEntry, error) {
	return nil, fmt.Errorf("memory unavailable")
}
func (m *errorMemoryManager) RetrieveByTag(_ context.Context, _ []string, _ int) ([]abstraction.MemoryEntry, error) {
	return nil, nil
}
func (m *errorMemoryManager) Forget(_ context.Context, _ string) error { return nil }
func (m *errorMemoryManager) Summarize(_ context.Context, _ []abstraction.MemoryEntry) (abstraction.MemoryEntry, error) {
	return abstraction.MemoryEntry{}, nil
}

type successMemoryManager struct {
	entries []abstraction.MemoryEntry
}

func (m *successMemoryManager) StoreShortTerm(_ context.Context, _ abstraction.MemoryEntry) error {
	return nil
}
func (m *successMemoryManager) StoreLongTerm(_ context.Context, _ abstraction.MemoryEntry) error {
	return nil
}
func (m *successMemoryManager) RetrieveSimilar(_ context.Context, _ string, _ int) ([]abstraction.MemoryEntry, error) {
	return m.entries, nil
}
func (m *successMemoryManager) RetrieveByTag(_ context.Context, _ []string, _ int) ([]abstraction.MemoryEntry, error) {
	return nil, nil
}
func (m *successMemoryManager) Forget(_ context.Context, _ string) error { return nil }
func (m *successMemoryManager) Summarize(_ context.Context, _ []abstraction.MemoryEntry) (abstraction.MemoryEntry, error) {
	return abstraction.MemoryEntry{}, nil
}
