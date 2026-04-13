package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadObservabilityConfig(t *testing.T) {
	// Create a temp config file with observability section
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `
server:
  addr: ":9090"

observability:
  log_level: "debug"
  otel_enabled: true
  otel_endpoint: "localhost:4317"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Observability.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.Observability.LogLevel, "debug")
	}
	if cfg.Observability.OtelEnabled != true {
		t.Errorf("OtelEnabled = %v, want %v", cfg.Observability.OtelEnabled, true)
	}
	if cfg.Observability.OtelEndpoint != "localhost:4317" {
		t.Errorf("OtelEndpoint = %q, want %q", cfg.Observability.OtelEndpoint, "localhost:4317")
	}
}

func TestLoadObservabilityDefaults(t *testing.T) {
	// Load with no file — should get zero-value observability config
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Observability.LogLevel != "info" {
		t.Errorf("default LogLevel = %q, want %q", cfg.Observability.LogLevel, "info")
	}
	if cfg.Observability.OtelEnabled != false {
		t.Errorf("default OtelEnabled = %v, want false", cfg.Observability.OtelEnabled)
	}
	if cfg.Observability.OtelEndpoint != "" {
		t.Errorf("default OtelEndpoint = %q, want empty string", cfg.Observability.OtelEndpoint)
	}
}

func TestLoadDefaultConfigFile(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `
observability:
  log_level: "info"
  otel_enabled: false
  otel_endpoint: ""
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.Observability.LogLevel != "info" {
		t.Errorf("LogLevel = %q, want %q", cfg.Observability.LogLevel, "info")
	}
	if cfg.Observability.OtelEnabled != false {
		t.Errorf("OtelEnabled = %v, want false", cfg.Observability.OtelEnabled)
	}
}
