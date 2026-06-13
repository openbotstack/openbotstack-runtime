package server

import (
	"log/slog"

	"github.com/openbotstack/openbotstack-core/ai/providers"
	"github.com/openbotstack/openbotstack-core/ai/router"
)

// InitAI creates the model router and registers providers configured in the
// database. Providers are runtime-mutable configuration managed via the Admin
// API (POST /v1/admin/providers), not environment variables — env vars are
// reserved for static, deploy-time settings. On a fresh database no providers
// are registered; configure them after startup.
func (b *ServerBuilder) InitAI() *ServerBuilder {
	if b.err != nil {
		return b
	}
	b.requireInit("pdb", "InitAI")

	modelRouter := router.NewDefaultRouter()
	providerFactory := providers.NewProviderFactory()

	n, err := LoadProvidersFromDB(b.pdb, providerFactory, modelRouter)
	if err != nil {
		b.fail("failed to load providers from database", err)
		return b
	}
	if n == 0 {
		slog.Warn("no providers configured — LLM features disabled. " +
			"Configure a provider via the Admin API: POST /v1/admin/providers")
	} else {
		slog.Info("providers loaded from database", "count", n)
	}

	b.modelRouter = modelRouter
	b.providerFactory = providerFactory
	return b
}
