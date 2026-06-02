package api

import (
	"context"
	"fmt"
	"sync"

	"github.com/openbotstack/openbotstack-core/registry/skills"
	"github.com/openbotstack/openbotstack-runtime/internal/skillutil"
)

// SkillReloader reloads skills from disk. Implemented by SkillWatcher.
type SkillReloader interface {
	Rescan(ctx context.Context) error
	ReloadSkillByID(ctx context.Context, skillID string) error
}

// SkillAdminService tracks skill enable/disable state for the admin API.
// It implements the SkillAdmin interface by wrapping a SkillProvider.
type SkillAdminService struct {
	Exec     SkillProvider
	reloader SkillReloader
	mu       sync.RWMutex
	disabled map[string]bool
}

// NewSkillAdminService creates a new skill admin service.
func NewSkillAdminService(exec SkillProvider) *SkillAdminService {
	return &SkillAdminService{Exec: exec, disabled: make(map[string]bool)}
}

// SetReloader sets the skill reloader for hot-reload support.
func (sa *SkillAdminService) SetReloader(r SkillReloader) {
	sa.mu.Lock()
	sa.reloader = r
	sa.mu.Unlock()
}

func (sa *SkillAdminService) getReloader() SkillReloader {
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	return sa.reloader
}

// FilteredList returns skill IDs excluding disabled ones.
func (sa *SkillAdminService) FilteredList() []string {
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
func (sa *SkillAdminService) IsDisabled(id string) bool {
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	return sa.disabled[id]
}

// ListSkills returns all skills with their admin info.
func (sa *SkillAdminService) ListSkills() ([]SkillAdminInfo, error) {
	ids := sa.Exec.List()
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	result := make([]SkillAdminInfo, 0, len(ids))
	for _, id := range ids {
		s, err := sa.Exec.Get(id)
		if err != nil {
			continue
		}
		result = append(result, SkillAdminInfo{
			ID:          s.ID(),
			Name:        s.Name(),
			Description: s.Description(),
			Type:        skillTypeFromSkill(s),
			Enabled:     !sa.disabled[id],
		})
	}
	return result, nil
}

// SetSkillEnabled enables or disables a skill.
func (sa *SkillAdminService) SetSkillEnabled(skillID string, enabled bool) error {
	sa.mu.Lock()
	defer sa.mu.Unlock()
	if enabled {
		delete(sa.disabled, skillID)
	} else {
		sa.disabled[skillID] = true
	}
	return nil
}

// ReloadSkills rescans the skills directory and reloads all skills.
func (sa *SkillAdminService) ReloadSkills(ctx context.Context) error {
	r := sa.getReloader()
	if r == nil {
		return fmt.Errorf("skill reloader not configured")
	}
	return r.Rescan(ctx)
}

// ReloadSkill reloads a single skill by its ID.
func (sa *SkillAdminService) ReloadSkill(ctx context.Context, skillID string) error {
	r := sa.getReloader()
	if r == nil {
		return fmt.Errorf("skill reloader not configured")
	}
	return r.ReloadSkillByID(ctx, skillID)
}

// skillTypeFromSkill extracts the type string from a skill.
func skillTypeFromSkill(s skills.Skill) string {
	return skillutil.SkillTypeFromID(s)
}
