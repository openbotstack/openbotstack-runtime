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

func TestVectorConfigDefaults(t *testing.T) {
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	// Vector search is disabled by default
	if cfg.Vector.Enabled {
		t.Errorf("default Vector.Enabled = %v, want false", cfg.Vector.Enabled)
	}
	if cfg.Vector.DatabaseURL != "" {
		t.Errorf("default Vector.DatabaseURL = %q, want empty", cfg.Vector.DatabaseURL)
	}
	if cfg.Vector.Model != "" {
		t.Errorf("default Vector.Model = %q, want empty (model set by EmbeddingService)", cfg.Vector.Model)
	}
	if cfg.Vector.Dimensions != 0 {
		t.Errorf("default Vector.Dimensions = %d, want 0", cfg.Vector.Dimensions)
	}
}

func TestVectorConfigEnvOverride(t *testing.T) {
	os.Setenv("OBS_VECTOR_DB_URL", "postgres://user:pass@localhost:5432/testdb")
	defer os.Unsetenv("OBS_VECTOR_DB_URL")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Vector.Enabled {
		t.Error("expected Vector.Enabled = true when OBS_VECTOR_DB_URL is set")
	}
	if cfg.Vector.DatabaseURL != "postgres://user:pass@localhost:5432/testdb" {
		t.Errorf("Vector.DatabaseURL = %q, want postgres://user:pass@localhost:5432/testdb", cfg.Vector.DatabaseURL)
	}
}

func TestVectorConfigFromYAML(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")

	content := `
vector:
  enabled: true
  database_url: "postgres://localhost:5432/obs"
  model: "text-embedding-3-large"
  dimensions: 1536
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write temp config: %v", err)
	}

	cfg, err := Load(cfgPath)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if !cfg.Vector.Enabled {
		t.Error("expected Vector.Enabled = true from YAML")
	}
	if cfg.Vector.DatabaseURL != "postgres://localhost:5432/obs" {
		t.Errorf("Vector.DatabaseURL = %q, want 'postgres://localhost:5432/obs'", cfg.Vector.DatabaseURL)
	}
	if cfg.Vector.Model != "text-embedding-3-large" {
		t.Errorf("Vector.Model = %q, want 'text-embedding-3-large'", cfg.Vector.Model)
	}
	if cfg.Vector.Dimensions != 1536 {
		t.Errorf("Vector.Dimensions = %d, want 1536", cfg.Vector.Dimensions)
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
