package adapters

import (
	"sync"

	"github.com/openbotstack/openbotstack-runtime/api"
)

// SkillAdminAdapter tracks skill enable/disable state for the admin API.
type SkillAdminAdapter struct {
	Exec     api.SkillProvider
	mu       sync.RWMutex
	disabled map[string]bool
}

// NewSkillAdminAdapter creates a new skill admin adapter.
func NewSkillAdminAdapter(exec api.SkillProvider) *SkillAdminAdapter {
	return &SkillAdminAdapter{Exec: exec, disabled: make(map[string]bool)}
}

// FilteredList returns skill IDs excluding disabled ones.
func (sa *SkillAdminAdapter) FilteredList() []string {
	all := sa.Exec.List()
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	result := make([]string, 0, len(all))
	for _, id := range all {
		if !sa.disabled[id] {
			result = append(result, id)
		}
	}
	return result
}

// IsDisabled checks if a skill is disabled.
func (sa *SkillAdminAdapter) IsDisabled(id string) bool {
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	return sa.disabled[id]
}

// ListSkills returns all skills with their admin info.
func (sa *SkillAdminAdapter) ListSkills() ([]api.SkillAdminInfo, error) {
	ids := sa.Exec.List()
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	result := make([]api.SkillAdminInfo, 0, len(ids))
	for _, id := range ids {
		s, err := sa.Exec.Get(id)
		if err != nil {
			continue
		}
		result = append(result, api.SkillAdminInfo{
			ID:          s.ID(),
			Name:        s.Name(),
			Description: s.Description(),
			Type:        "declarative",
			Enabled:     !sa.disabled[id],
		})
	}
	return result, nil
}

// SetSkillEnabled enables or disables a skill.
func (sa *SkillAdminAdapter) SetSkillEnabled(skillID string, enabled bool) error {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	if enabled {
		delete(sa.disabled, skillID)
	} else {
		sa.disabled[skillID] = true
	}
	return nil
}
