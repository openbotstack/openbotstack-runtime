package config

import (
	"os"
	"strings"
	"time"

	mcpcore "github.com/openbotstack/openbotstack-core/mcp"
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
	Agent         AgentConfig         `yaml:"agent"`
	CORS          CORSConfig          `yaml:"cors"`
	MCP           MCPConfig           `yaml:"mcp"`
}

// CORSConfig controls Cross-Origin Resource Sharing settings.
type CORSConfig struct {
	// AllowedOrigins specifies which origins may access the API.
	// Use ["*"] for development; restrict in production.
	// Default: ["*"]
	AllowedOrigins []string `yaml:"allowed_origins"`
}

// AgentConfig controls agent execution parameters.
type AgentConfig struct {
	// Mode selects the agent execution strategy.
	// Currently only "harness" is supported (default).
	// Env override: OBS_AGENT_MODE
	Mode string `yaml:"mode"`

	// DualLoop holds bounds for the execution harness and reasoning loop.
	DualLoop DualLoopConfig `yaml:"dual_loop"`
}

// DualLoopConfig holds bounds for the execution harness and reasoning loop.
type DualLoopConfig struct {
	MaxTurns           int           `yaml:"max_turns"`
	MaxToolCalls       int           `yaml:"max_tool_calls"`
	MaxTurnRuntime     time.Duration `yaml:"max_turn_runtime"`
	MaxSteps           int           `yaml:"max_steps"`
	MaxSessionRuntime  time.Duration `yaml:"max_session_runtime"`
	MaxRetainedTurns   int           `yaml:"max_retained_turns"`
	DefaultStepTimeout time.Duration `yaml:"default_step_timeout"`
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
	Default     string            `yaml:"default"`
	ModelScope  LLMProviderConfig `yaml:"modelscope"`
	OpenAI      LLMProviderConfig `yaml:"openai"`
	Claude      LLMProviderConfig `yaml:"claude"`
	SiliconFlow LLMProviderConfig `yaml:"siliconflow"`
}

type LLMProviderConfig struct {
	BaseURL string `yaml:"base_url"`
	APIKey  string `yaml:"api_key"`
	Model   string `yaml:"model"`
}

// MCPConfig configures MCP server connections.
type MCPConfig struct {
	Servers []MCPServerConfig `yaml:"servers"`
}

// MCPServerConfig describes a single MCP server to connect to.
type MCPServerConfig struct {
	ID        string              `yaml:"id"`
	Name      string              `yaml:"name"`
	Transport string              `yaml:"transport"` // "stdio" | "sse"
	Command   string              `yaml:"command,omitempty"`
	Args      []string            `yaml:"args,omitempty"`
	URL       string              `yaml:"url,omitempty"`
	Env       map[string]string   `yaml:"env,omitempty"`
	Auth      *MCPServerAuthConfig `yaml:"auth,omitempty"`
	Enabled   bool                `yaml:"enabled"`
}

// MCPServerAuthConfig describes authentication for an MCP server.
type MCPServerAuthConfig struct {
	Type    string            `yaml:"type"`               // "bearer" | "api_key" | "custom" | "none"
	Token   string            `yaml:"token,omitempty"`     // bearer token or API key value
	Header  string            `yaml:"header,omitempty"`    // custom header name for api_key (default: X-API-Key)
	Headers map[string]string `yaml:"headers,omitempty"`   // custom headers for HTTP transports
	EnvAuth map[string]string `yaml:"env_auth,omitempty"`  // env vars for stdio transport
}

// defaultConfig returns the default configuration with all fields populated.
func defaultConfig() *Config {
	return &Config{
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
			HTTPAllowlist:   []string{},
			ToolRegistryURL: "http://localhost:8080",
		},
		Agent: AgentConfig{
			Mode: "harness",
			DualLoop: DualLoopConfig{
				MaxTurns:           5,
				MaxToolCalls:       10,
				MaxTurnRuntime:     180 * time.Second,
				MaxSteps:           10,
				MaxSessionRuntime:  600 * time.Second,
				MaxRetainedTurns:   4,
				DefaultStepTimeout: 120 * time.Second,
			},
		},
		CORS: CORSConfig{
			AllowedOrigins: []string{"*"},
		},
	}
}

// Load loads configuration from file and environment variables.
func Load(path string) (*Config, error) {
	cfg := defaultConfig()

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
		cfg.Providers.LLM.ModelScope.APIKey = val
		cfg.Providers.LLM.Claude.APIKey = val
		cfg.Providers.LLM.SiliconFlow.APIKey = val
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
	if val := os.Getenv("OBS_OTEL_ENABLED"); val == "true" {
		cfg.Observability.OtelEnabled = true
	}
	if val := os.Getenv("OBS_OTEL_ENDPOINT"); val != "" {
		cfg.Observability.OtelEndpoint = val
	}

	// Memory overrides
	if val := os.Getenv("OBS_DATA_DIR"); val != "" {
		cfg.Memory.DataDir = val
	}

	// CORS overrides
	if val := os.Getenv("OBS_CORS_ORIGINS"); val != "" {
		cfg.CORS.AllowedOrigins = strings.Split(val, ",")
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

	// Agent overrides
	if val := os.Getenv("OBS_AGENT_MODE"); val != "" {
		cfg.Agent.Mode = val
	}
	if val := os.Getenv("OBS_STEP_TIMEOUT"); val != "" {
		if d, err := time.ParseDuration(val); err == nil {
			cfg.Agent.DualLoop.DefaultStepTimeout = d
		}
	}

	return cfg, nil
}

// ToCoreServers converts config MCPServerConfigs to core ServerConfigs.
func (c *MCPConfig) ToCoreServers() []mcpcore.ServerConfig {
	if len(c.Servers) == 0 {
		return nil
	}
	out := make([]mcpcore.ServerConfig, len(c.Servers))
	for i, s := range c.Servers {
		out[i] = s.toCore()
	}
	return out
}

func (s MCPServerConfig) toCore() mcpcore.ServerConfig {
	cfg := mcpcore.ServerConfig{
		ID:        s.ID,
		Name:      s.Name,
		Transport: s.Transport,
		Command:   s.Command,
		Args:      s.Args,
		URL:       s.URL,
		Env:       s.Env,
		Enabled:   s.Enabled,
	}
	if s.Auth != nil {
		cfg.Auth = &mcpcore.ServerAuth{
			Type:    s.Auth.Type,
			Token:   s.Auth.Token,
			Header:  s.Auth.Header,
			Headers: s.Auth.Headers,
			EnvAuth: s.Auth.EnvAuth,
		}
	}
	return cfg
}
