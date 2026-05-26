package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfig_AgentDefaults(t *testing.T) {
	cfg := defaultConfig()

	if cfg.Agent.Mode != "harness" {
		t.Errorf("Agent.Mode = %q, want %q", cfg.Agent.Mode, "harness")
	}
	if cfg.Agent.DualLoop.MaxTurns != 5 {
		t.Errorf("DualLoop.MaxTurns = %d, want 5", cfg.Agent.DualLoop.MaxTurns)
	}
	if cfg.Agent.DualLoop.MaxToolCalls != 10 {
		t.Errorf("DualLoop.MaxToolCalls = %d, want 10", cfg.Agent.DualLoop.MaxToolCalls)
	}
	if cfg.Agent.DualLoop.MaxTurnRuntime != 180*time.Second {
		t.Errorf("DualLoop.MaxTurnRuntime = %v, want 180s", cfg.Agent.DualLoop.MaxTurnRuntime)
	}
	if cfg.Agent.DualLoop.MaxSteps != 10 {
		t.Errorf("DualLoop.MaxSteps = %d, want 10", cfg.Agent.DualLoop.MaxSteps)
	}
	if cfg.Agent.DualLoop.MaxSessionRuntime != 600*time.Second {
		t.Errorf("DualLoop.MaxSessionRuntime = %v, want 600s", cfg.Agent.DualLoop.MaxSessionRuntime)
	}
	if cfg.Agent.DualLoop.MaxRetainedTurns != 4 {
		t.Errorf("DualLoop.MaxRetainedTurns = %d, want 4", cfg.Agent.DualLoop.MaxRetainedTurns)
	}
	if cfg.Agent.DualLoop.DefaultStepTimeout != 120*time.Second {
		t.Errorf("DualLoop.DefaultStepTimeout = %v, want 120s", cfg.Agent.DualLoop.DefaultStepTimeout)
	}
}

func TestConfig_AgentModeEnvOverride(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("OBS_AGENT_MODE", "harness")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Agent.Mode != "harness" {
		t.Errorf("Agent.Mode = %q, want %q (env override)", cfg.Agent.Mode, "harness")
	}
}

func TestConfig_StepTimeoutEnvOverride(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	if err := os.WriteFile(configPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	t.Setenv("OBS_STEP_TIMEOUT", "120s")

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Agent.DualLoop.DefaultStepTimeout != 120*time.Second {
		t.Errorf("DefaultStepTimeout = %v, want 120s (env override)", cfg.Agent.DualLoop.DefaultStepTimeout)
	}
}

func TestConfig_AgentModeFromFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `
agent:
  mode: harness
  dual_loop:
    max_turns: 12
    max_steps: 5
`
	if err := os.WriteFile(configPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Agent.Mode != "harness" {
		t.Errorf("Agent.Mode = %q, want %q", cfg.Agent.Mode, "harness")
	}
	if cfg.Agent.DualLoop.MaxTurns != 12 {
		t.Errorf("DualLoop.MaxTurns = %d, want 12", cfg.Agent.DualLoop.MaxTurns)
	}
	if cfg.Agent.DualLoop.MaxSteps != 5 {
		t.Errorf("DualLoop.MaxSteps = %d, want 5", cfg.Agent.DualLoop.MaxSteps)
	}
	// Non-overridden fields keep defaults
	if cfg.Agent.DualLoop.MaxToolCalls != 10 {
		t.Errorf("DualLoop.MaxToolCalls = %d, want 10 (default)", cfg.Agent.DualLoop.MaxToolCalls)
	}
}

func TestConfig_ExistingFieldsUnaffected(t *testing.T) {
	cfg := defaultConfig()

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
