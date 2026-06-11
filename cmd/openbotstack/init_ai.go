package main

import (
	"log/slog"

	"github.com/openbotstack/openbotstack-core/ai/providers"
	"github.com/openbotstack/openbotstack-core/ai/router"
	"github.com/openbotstack/openbotstack-runtime/config"
)

// InitAI creates the model router and registers the configured provider.
func (b *ServerBuilder) InitAI() *ServerBuilder {
	b.requireInit("cfg", "InitAI")
	modelRouter := router.NewDefaultRouter()
	providerName := b.cfg.Providers.LLM.Default
	var providerConfig config.LLMProviderConfig

	switch providerName {
	case "modelscope":
		providerConfig = b.cfg.Providers.LLM.ModelScope
	case "claude":
		providerConfig = b.cfg.Providers.LLM.Claude
	default:
		providerConfig = b.cfg.Providers.LLM.OpenAI
	}

	providerFactory := providers.NewProviderFactory()

	if providerConfig.APIKey != "" {
		llmProvider := providerFactory.Create(providerName, providerConfig.BaseURL, providerConfig.APIKey, providerConfig.Model)
		if err := modelRouter.Register(llmProvider); err != nil {
			slog.Error("failed to register provider", "error", err)
		} else {
			slog.Info("llm provider registered", "provider", providerName, "model", providerConfig.Model, "base_url", providerConfig.BaseURL)
		}
	} else {
		slog.Warn("LLM API key not set, LLM features will be disabled")
	}

	seedProviderConfig(b.pdb, providerName, providerConfig, true)

	b.modelRouter = modelRouter
	b.providerFactory = providerFactory
	b.providerName = providerName
	b.providerConfig = providerConfig
	return b
}
