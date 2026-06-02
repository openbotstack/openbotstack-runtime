package api

import (
	"log/slog"

	"github.com/openbotstack/openbotstack-core/ai/providers"
	"github.com/openbotstack/openbotstack-core/ai/router"
	"github.com/openbotstack/openbotstack-core/ai/types"
)

// RouterProviderLister adapts a ModelRouter to the ProviderLister interface.
type RouterProviderLister struct {
	Router *router.DefaultRouter
}

func (m *RouterProviderLister) ListProviders() []ProviderInfo {
	ids := m.Router.List()
	result := make([]ProviderInfo, 0, len(ids))
	for _, id := range ids {
		provider, err := m.Router.Route(nil, types.ModelConstraints{})
		caps := []string{}
		if err == nil && provider != nil {
			for _, c := range provider.Capabilities() {
				caps = append(caps, string(c))
			}
		}
		result = append(result, ProviderInfo{
			ID:           id,
			Capabilities: caps,
		})
	}
	return result
}

// RouterProviderReloader adapts a DefaultRouter + ProviderFactory to the ProviderReloader interface.
type RouterProviderReloader struct {
	Router  *router.DefaultRouter
	Factory *providers.ProviderFactory
}

func (p *RouterProviderReloader) ReloadProvider(providerName, baseURL, apiKey, model string) error {
	newProvider := p.Factory.Create(providerName, baseURL, apiKey, model)
	// Remove any existing provider of the same driver so only this one routes.
	p.Router.Unregister(providerName)
	p.Router.Replace(newProvider)
	slog.Info("provider hot-reloaded", "provider", providerName, "model", model, "base_url", baseURL)
	return nil
}
