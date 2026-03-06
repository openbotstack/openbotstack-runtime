package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server    ServerConfig    `yaml:"server"`
	Redis     RedisConfig     `yaml:"redis"`
	Providers ProvidersConfig `yaml:"providers"`
}

type ServerConfig struct {
	Addr string `yaml:"addr"`
}

type RedisConfig struct {
	URL string `yaml:"url"`
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
	if val := os.Getenv("OBS_REDIS_URL"); val != "" {
		cfg.Redis.URL = val
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

	return cfg, nil
}
