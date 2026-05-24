package ai

import (
	"fmt"
	"sync"
	"time"

	"github.com/openbotstack/openbotstack-core/ai/providers"
	"github.com/openbotstack/openbotstack-runtime/api"
)

// InMemoryModelRegistry implements providers.ModelRegistry with in-memory storage.
type InMemoryModelRegistry struct {
	mu       sync.RWMutex
	models   map[string]providers.ModelEntry
	usage    map[string]providers.ModelUsage // executionID → usage
}

// NewInMemoryModelRegistry creates a new in-memory model registry.
func NewInMemoryModelRegistry() *InMemoryModelRegistry {
	return &InMemoryModelRegistry{
		models: make(map[string]providers.ModelEntry),
		usage:  make(map[string]providers.ModelUsage),
	}
}

func (r *InMemoryModelRegistry) Register(entry providers.ModelEntry) error {
	if entry.ID == "" {
		return fmt.Errorf("model registry: entry ID is required")
	}
	if entry.RegisteredAt.IsZero() {
		entry.RegisteredAt = time.Now()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.models[entry.ID] = entry
	return nil
}

func (r *InMemoryModelRegistry) List() []providers.ModelEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]providers.ModelEntry, 0, len(r.models))
	for _, m := range r.models {
		result = append(result, m)
	}
	return result
}

func (r *InMemoryModelRegistry) Get(id string) (providers.ModelEntry, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.models[id]
	return m, ok
}

func (r *InMemoryModelRegistry) RecordUsage(usage providers.ModelUsage) error {
	if usage.ExecutionID == "" || usage.ModelID == "" {
		return fmt.Errorf("model registry: execution_id and model_id are required")
	}
	if usage.UsedAt.IsZero() {
		usage.UsedAt = time.Now()
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.usage[usage.ExecutionID] = usage
	return nil
}

func (r *InMemoryModelRegistry) UsageForExecution(executionID string) (providers.ModelUsage, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	u, ok := r.usage[executionID]
	return u, ok
}

// ListModels implements api.ModelRegistryAdmin for the admin API.
func (r *InMemoryModelRegistry) ListModels() []api.ModelInfo {
	entries := r.List()
	result := make([]api.ModelInfo, len(entries))
	for i, e := range entries {
		result[i] = api.ModelInfo{
			ID:           e.ID,
			Provider:     e.Provider,
			Model:        e.Model,
			Capabilities: e.Capabilities,
			RegisteredAt: e.RegisteredAt.Format(time.RFC3339),
		}
	}
	return result
}

// GetModelUsage implements api.ModelRegistryAdmin for the admin API.
func (r *InMemoryModelRegistry) GetModelUsage(executionID string) (*api.ModelUsageInfo, bool) {
	u, ok := r.UsageForExecution(executionID)
	if !ok {
		return nil, false
	}
	return &api.ModelUsageInfo{
		ExecutionID: u.ExecutionID,
		ModelID:     u.ModelID,
		UsedAt:      u.UsedAt.Format(time.RFC3339),
	}, true
}
