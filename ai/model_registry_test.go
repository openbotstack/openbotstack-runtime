package ai

import (
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/ai/providers"
)

func TestInMemoryModelRegistry_RegisterAndList(t *testing.T) {
	r := NewInMemoryModelRegistry()

	err := r.Register(providers.ModelEntry{
		ID:           "openai/gpt-4o",
		Provider:     "openai",
		Model:        "gpt-4o",
		Capabilities: []string{"text_generation", "tool_calling"},
	})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	err = r.Register(providers.ModelEntry{
		ID:           "anthropic/claude-3-opus",
		Provider:     "anthropic",
		Model:        "claude-3-opus",
		Capabilities: []string{"text_generation", "streaming"},
	})
	if err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	models := r.List()
	if len(models) != 2 {
		t.Errorf("List() returned %d models, want 2", len(models))
	}
}

func TestInMemoryModelRegistry_Get(t *testing.T) {
	r := NewInMemoryModelRegistry()
	r.Register(providers.ModelEntry{ID: "test/model"})

	entry, ok := r.Get("test/model")
	if !ok {
		t.Error("Get should find registered model")
	}
	if entry.ID != "test/model" {
		t.Errorf("ID = %q, want %q", entry.ID, "test/model")
	}

	_, ok = r.Get("nonexistent")
	if ok {
		t.Error("Get should not find unregistered model")
	}
}

func TestInMemoryModelRegistry_RecordUsage(t *testing.T) {
	r := NewInMemoryModelRegistry()

	err := r.RecordUsage(providers.ModelUsage{
		ExecutionID: "exec-1",
		ModelID:     "openai/gpt-4o",
	})
	if err != nil {
		t.Fatalf("RecordUsage failed: %v", err)
	}

	usage, ok := r.UsageForExecution("exec-1")
	if !ok {
		t.Error("UsageForExecution should find recorded usage")
	}
	if usage.ModelID != "openai/gpt-4o" {
		t.Errorf("ModelID = %q, want %q", usage.ModelID, "openai/gpt-4o")
	}
}

func TestInMemoryModelRegistry_RecordUsageValidation(t *testing.T) {
	r := NewInMemoryModelRegistry()

	err := r.RecordUsage(providers.ModelUsage{ExecutionID: ""})
	if err == nil {
		t.Error("should reject empty execution_id")
	}

	err = r.RecordUsage(providers.ModelUsage{ExecutionID: "exec-1", ModelID: ""})
	if err == nil {
		t.Error("should reject empty model_id")
	}
}

func TestInMemoryModelRegistry_AutoTimestamps(t *testing.T) {
	r := NewInMemoryModelRegistry()
	before := time.Now()

	r.Register(providers.ModelEntry{ID: "test/model"})
	entry, _ := r.Get("test/model")

	if entry.RegisteredAt.Before(before) {
		t.Error("RegisteredAt should be auto-set")
	}
}

func TestInMemoryModelRegistry_RegisterValidation(t *testing.T) {
	r := NewInMemoryModelRegistry()
	err := r.Register(providers.ModelEntry{ID: ""})
	if err == nil {
		t.Error("should reject empty ID")
	}
}

func TestInMemoryModelRegistry_AdminInterface(t *testing.T) {
	r := NewInMemoryModelRegistry()
	r.Register(providers.ModelEntry{
		ID:           "openai/gpt-4o",
		Provider:     "openai",
		Model:        "gpt-4o",
		Capabilities: []string{"text_generation"},
	})
	r.RecordUsage(providers.ModelUsage{
		ExecutionID: "exec-1",
		ModelID:     "openai/gpt-4o",
	})

	models := r.ListModels()
	if len(models) != 1 {
		t.Fatalf("ListModels = %d, want 1", len(models))
	}
	if models[0].Provider != "openai" {
		t.Errorf("Provider = %q, want %q", models[0].Provider, "openai")
	}

	usage, ok := r.GetModelUsage("exec-1")
	if !ok {
		t.Error("GetModelUsage should find usage")
	}
	if usage.ModelID != "openai/gpt-4o" {
		t.Errorf("ModelID = %q, want %q", usage.ModelID, "openai/gpt-4o")
	}

	_, ok = r.GetModelUsage("nonexistent")
	if ok {
		t.Error("GetModelUsage should not find nonexistent execution")
	}
}
