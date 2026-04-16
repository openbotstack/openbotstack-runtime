package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type MemoryConfig struct {
	DataDir            string `yaml:"data_dir"`              // default: "./data"
	SummaryThreshold   int    `yaml:"summary_threshold"`     // default: 20 messages
	SummaryEnabled     bool   `yaml:"summary_enabled"`       // default: true
	MaxHistoryMessages int    `yaml:"max_history_messages"`  // default: 50
}

// SandboxConfig controls Wasm skill sandbox behavior.
type SandboxConfig struct {
	// HTTPAllowlist controls which URLs Wasm skills may access.
	// Patterns: "https://api.example.com", "*.example.com", "*" (allow all).
	// Default: ["*"] for development; restrict for production.
	HTTPAllowlist []string `yaml:"http_allowlist"`

	// ToolRegistryURL is the base URL for the tool registry service.
	// Default: "http://localhost:8080"
	ToolRegistryURL string `yaml:"tool_registry_url"`
}

// VectorConfig controls optional vector search capabilities.
type VectorConfig struct {
	// Enabled enables vector semantic search. Requires PostgreSQL + pgvector.
	// Default: false (system uses keyword matching).
	Enabled bool `yaml:"enabled"`

	// DatabaseURL is the PostgreSQL connection string.
	// e.g. "postgres://user:pass@localhost:5432/openbotstack?sslmode=disable"
	// Env override: OBS_VECTOR_DB_URL
	DatabaseURL string `yaml:"database_url"`

	// Model is the embedding model name.
	// Default: "text-embedding-3-small"
	Model string `yaml:"model"`

	// Dimensions is the embedding vector dimension.
	// Default: 512
	Dimensions int `yaml:"dimensions"`
}

// TLSConfig controls TLS/HTTPS configuration.
type TLSConfig struct {
	// CertFile is the path to the TLS certificate file (PEM format).
	// Env override: OBS_TLS_CERT_FILE
	CertFile string `yaml:"cert_file"`

	// KeyFile is the path to the TLS private key file (PEM format).
	// Env override: OBS_TLS_KEY_FILE
	KeyFile string `yaml:"key_file"`
}

type Config struct {
	Server        ServerConfig        `yaml:"server"`
	TLS           TLSConfig           `yaml:"tls"`
	Providers     ProvidersConfig     `yaml:"providers"`
	Observability ObservabilityConfig `yaml:"observability"`
	Memory        MemoryConfig        `yaml:"memory"`
	Sandbox       SandboxConfig       `yaml:"sandbox"`
	Vector        VectorConfig        `yaml:"vector"`
}

type ObservabilityConfig struct {
	LogLevel     string `yaml:"log_level"`     // debug, info, warn, error
	OtelEnabled  bool   `yaml:"otel_enabled"`  // enable OpenTelemetry
	OtelEndpoint string `yaml:"otel_endpoint"` // OTLP gRPC endpoint
}

type ServerConfig struct {
	Addr string `yaml:"addr"`
}

type ProvidersConfig struct {
	LLM LLMConfig `yaml:"llm"`
}

type LLMConfig struct {
	Default    string            `yaml:"default"`
	ModelScope LLMProviderConfig `yaml:"modelscope"`
	OpenAI     LLMProviderConfig `yaml:"openai"`
}

type LLMProviderConfig struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
	Model   string `yaml:"model"`
}

// Load loads configuration from file and environment variables.
func Load(path string) (*Config, error) {
	// Default config
	cfg := &Config{
		Server: ServerConfig{
			Addr: ":8080",
		},
		Providers: ProvidersConfig{
			LLM: LLMConfig{
				Default: "openai",
				OpenAI: LLMProviderConfig{
					BaseURL: "https://api.openai.com/v1",
					Model:   "gpt-4o",
				},
			},
		},
		Observability: ObservabilityConfig{
			LogLevel: "info",
		},
		Memory: MemoryConfig{
			DataDir:            "./data",
			SummaryThreshold:   20,
			SummaryEnabled:     true,
			MaxHistoryMessages: 50,
		},
		Sandbox: SandboxConfig{
			HTTPAllowlist:   []string{"*"},
			ToolRegistryURL: "http://localhost:8080",
		},
	}

	// Load from file if exists
	if path != "" {
		data, err := os.ReadFile(path)
		if err == nil {
			if err := yaml.Unmarshal(data, cfg); err != nil {
				return nil, err
			}
		}
	}

	// Override with Environment Variables
	if val := os.Getenv("OBS_SERVER_ADDR"); val != "" {
		cfg.Server.Addr = val
	}

	// LLM Overrides
	if val := os.Getenv("OBS_LLM_PROVIDER"); val != "" {
		cfg.Providers.LLM.Default = val
	}

	// OpenAI specific overrides (used as default or fallback)
	if val := os.Getenv("OBS_LLM_API_KEY"); val != "" {
		cfg.Providers.LLM.OpenAI.APIKey = val
		cfg.Providers.LLM.ModelScope.APIKey = val // Fallback if user switches provider
	}
	if val := os.Getenv("OBS_LLM_URL"); val != "" {
		cfg.Providers.LLM.OpenAI.BaseURL = val
		cfg.Providers.LLM.ModelScope.BaseURL = val
	}
	if val := os.Getenv("OBS_LLM_MODEL"); val != "" {
		cfg.Providers.LLM.OpenAI.Model = val
		cfg.Providers.LLM.ModelScope.Model = val
	}

	// Observability overrides
	if val := os.Getenv("OBS_LOG_LEVEL"); val != "" {
		cfg.Observability.LogLevel = val
	}

	// Memory overrides
	if val := os.Getenv("OBS_DATA_DIR"); val != "" {
		cfg.Memory.DataDir = val
	}

	// Vector overrides
	if val := os.Getenv("OBS_VECTOR_DB_URL"); val != "" {
		cfg.Vector.DatabaseURL = val
		cfg.Vector.Enabled = true
	}

	// TLS overrides
	if val := os.Getenv("OBS_TLS_CERT_FILE"); val != "" {
		cfg.TLS.CertFile = val
	}
	if val := os.Getenv("OBS_TLS_KEY_FILE"); val != "" {
		cfg.TLS.KeyFile = val
	}

	return cfg, nil
}
