package adapters

import (
	"github.com/openbotstack/openbotstack-core/capability"
	"github.com/openbotstack/openbotstack-runtime/api"
)

// CapabilityListerAdapter adapts capability.CapabilityRegistry to api.CapabilityLister.
type CapabilityListerAdapter struct {
	registry capability.CapabilityRegistry
}

// NewCapabilityLister creates a new adapter.
func NewCapabilityLister(registry capability.CapabilityRegistry) *CapabilityListerAdapter {
	return &CapabilityListerAdapter{registry: registry}
}

// List returns all capabilities as API descriptors.
func (a *CapabilityListerAdapter) List() []api.CapabilityDescriptor {
	if a.registry == nil {
		return nil
	}
	descs := a.registry.List()
	result := make([]api.CapabilityDescriptor, len(descs))
	for i, d := range descs {
		result[i] = api.CapabilityDescriptor{
			ID:          d.ID,
			Name:        d.Name,
			Description: d.Description,
			Kind:        string(d.Kind),
			SourceID:    d.SourceID,
		}
	}
	return result
}
