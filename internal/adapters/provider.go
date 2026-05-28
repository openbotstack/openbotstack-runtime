package adapters

import (
	"log/slog"

	"github.com/openbotstack/openbotstack-core/ai/providers"
	"github.com/openbotstack/openbotstack-core/ai/router"
	"github.com/openbotstack/openbotstack-core/control/skills"
	"github.com/openbotstack/openbotstack-runtime/api"
)

// ModelRouterLister adapts a ModelRouter to the api.ProviderLister interface.
type ModelRouterLister struct {
	Router *router.DefaultRouter
}

func (m *ModelRouterLister) ListProviders() []api.ProviderInfo {
	ids := m.Router.List()
	result := make([]api.ProviderInfo, 0, len(ids))
	for _, id := range ids {
		provider, err := m.Router.Route(nil, skills.ModelConstraints{})
		caps := []string{}
		if err == nil && provider != nil {
			for _, c := range provider.Capabilities() {
				caps = append(caps, string(c))
			}
		}
		result = append(result, api.ProviderInfo{
			ID:           id,
			Capabilities: caps,
		})
	}
	return result
}

// ProviderReloader adapts a DefaultRouter to the api.ProviderReloader interface.
type ProviderReloader struct {
	Router  *router.DefaultRouter
	Factory *providers.ProviderFactory
}

func (p *ProviderReloader) ReloadProvider(providerName, baseURL, apiKey, model string) error {
	newProvider := p.Factory.Create(providerName, baseURL, apiKey, model)
	// Remove any existing provider of the same driver so only this one routes.
	p.Router.Unregister(providerName)
	p.Router.Replace(newProvider)
	slog.Info("provider hot-reloaded", "provider", providerName, "model", model, "base_url", baseURL)
	return nil
}
