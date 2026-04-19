package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfig_AgentDefaults(t *testing.T) {
	cfg := defaultConfig()

	if cfg.Agent.Mode != "single_pass" {
		t.Errorf("Agent.Mode = %q, want %q", cfg.Agent.Mode, "single_pass")
	}
	if cfg.Agent.DualLoop.MaxTurns != 8 {
		t.Errorf("DualLoop.MaxTurns = %d, want 8", cfg.Agent.DualLoop.MaxTurns)
	}
	if cfg.Agent.DualLoop.MaxToolCalls != 20 {
		t.Errorf("DualLoop.MaxToolCalls = %d, want 20", cfg.Agent.DualLoop.MaxToolCalls)
	}
	if cfg.Agent.DualLoop.MaxTurnRuntime != 30*time.Second {
		t.Errorf("DualLoop.MaxTurnRuntime = %v, want 30s", cfg.Agent.DualLoop.MaxTurnRuntime)
	}
	if cfg.Agent.DualLoop.MaxWorkflowSteps != 5 {
		t.Errorf("DualLoop.MaxWorkflowSteps = %d, want 5", cfg.Agent.DualLoop.MaxWorkflowSteps)
	}
	if cfg.Agent.DualLoop.MaxSessionRuntime != 60*time.Second {
		t.Errorf("DualLoop.MaxSessionRuntime = %v, want 60s", cfg.Agent.DualLoop.MaxSessionRuntime)
	}
	if cfg.Agent.DualLoop.MaxRetainedTurns != 4 {
		t.Errorf("DualLoop.MaxRetainedTurns = %d, want 4", cfg.Agent.DualLoop.MaxRetainedTurns)
	}
}

func TestConfig_AgentModeEnvOverride(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	// Write empty config so Load doesn't fail
	if err := os.WriteFile(configPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("OBS_AGENT_MODE", "dual_loop")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Agent.Mode != "dual_loop" {
		t.Errorf("Agent.Mode = %q, want %q (env override)", cfg.Agent.Mode, "dual_loop")
	}
}

func TestConfig_AgentModeFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `
agent:
  mode: dual_loop
  dual_loop:
    max_turns: 12
    max_workflow_steps: 3
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Agent.Mode != "dual_loop" {
		t.Errorf("Agent.Mode = %q, want %q", cfg.Agent.Mode, "dual_loop")
	}
	if cfg.Agent.DualLoop.MaxTurns != 12 {
		t.Errorf("DualLoop.MaxTurns = %d, want 12", cfg.Agent.DualLoop.MaxTurns)
	}
	if cfg.Agent.DualLoop.MaxWorkflowSteps != 3 {
		t.Errorf("DualLoop.MaxWorkflowSteps = %d, want 3", cfg.Agent.DualLoop.MaxWorkflowSteps)
	}
	// Non-overridden fields keep defaults
	if cfg.Agent.DualLoop.MaxToolCalls != 20 {
		t.Errorf("DualLoop.MaxToolCalls = %d, want 20 (default)", cfg.Agent.DualLoop.MaxToolCalls)
	}
}

func TestConfig_ExistingFieldsUnaffected(t *testing.T) {
	cfg := defaultConfig()

	// Existing fields should have the same defaults as before
	if cfg.Server.Addr != ":8080" {
		t.Errorf("Server.Addr = %q, want %q", cfg.Server.Addr, ":8080")
	}
	if cfg.Providers.LLM.Default != "openai" {
		t.Errorf("Providers.LLM.Default = %q, want %q", cfg.Providers.LLM.Default, "openai")
	}
	if cfg.Memory.DataDir != "./data" {
		t.Errorf("Memory.DataDir = %q, want %q", cfg.Memory.DataDir, "./data")
	}
}
