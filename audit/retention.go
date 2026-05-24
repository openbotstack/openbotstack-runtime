package audit

import (
	"sync"
	"time"
)

// RetentionConfig controls how long audit events are kept.
type RetentionConfig struct {
	Enabled         bool
	DefaultDays     int
	TenantOverrides map[string]int
}

// Purger deletes audit events older than a cutoff time.
type Purger interface {
	PurgeBefore(cutoff time.Time, tenantID string) (int64, error)
}

// DefaultRetentionConfig returns a sensible default (disabled, 90-day default).
func DefaultRetentionConfig() RetentionConfig {
	return RetentionConfig{
		DefaultDays:     90,
		TenantOverrides: make(map[string]int),
	}
}

// RetentionPolicy evaluates retention rules per tenant.
type RetentionPolicy struct {
	mu    sync.RWMutex
	cfg   RetentionConfig
	purger Purger
}

// NewRetentionPolicy creates a policy from config.
func NewRetentionPolicy(cfg RetentionConfig, purger Purger) *RetentionPolicy {
	if cfg.TenantOverrides == nil {
		cfg.TenantOverrides = make(map[string]int)
	}
	return &RetentionPolicy{cfg: cfg, purger: purger}
}

// DaysForTenant returns the retention period for a tenant.
func (p *RetentionPolicy) DaysForTenant(tenantID string) int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if d, ok := p.cfg.TenantOverrides[tenantID]; ok {
		return d
	}
	return p.cfg.DefaultDays
}

// CutoffTime returns the time before which events should be purged for a tenant.
func (p *RetentionPolicy) CutoffTime(tenantID string) time.Time {
	days := p.DaysForTenant(tenantID)
	return time.Now().AddDate(0, 0, -days)
}

// ShouldPurge returns true if an event at the given timestamp should be purged.
func (p *RetentionPolicy) ShouldPurge(ts time.Time, tenantID string) bool {
	p.mu.RLock()
	enabled := p.cfg.Enabled
	p.mu.RUnlock()

	if !enabled {
		return false
	}
	return !ts.After(p.CutoffTime(tenantID))
}

// SetTenantOverride adds or updates a tenant-specific retention period.
func (p *RetentionPolicy) SetTenantOverride(tenantID string, days int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cfg.TenantOverrides[tenantID] = days
}

// RemoveTenantOverride removes a tenant-specific override, falling back to default.
func (p *RetentionPolicy) RemoveTenantOverride(tenantID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.cfg.TenantOverrides, tenantID)
}

// Config returns a copy of the current retention configuration.
func (p *RetentionPolicy) Config() RetentionConfig {
	p.mu.RLock()
	defer p.mu.RUnlock()
	cfg := p.cfg
	cfg.TenantOverrides = make(map[string]int, len(p.cfg.TenantOverrides))
	for k, v := range p.cfg.TenantOverrides {
		cfg.TenantOverrides[k] = v
	}
	return cfg
}

// PurgeExpired deletes expired audit events for all tenants with overrides,
// then purges expired events for the default retention period.
// Returns total deleted count.
func (p *RetentionPolicy) PurgeExpired() (int64, error) {
	if p.purger == nil {
		return 0, nil
	}

	p.mu.RLock()
	enabled := p.cfg.Enabled
	p.mu.RUnlock()

	if !enabled {
		return 0, nil
	}

	var total int64

	// Purge per-tenant overrides
	p.mu.RLock()
	tenants := make([]string, 0, len(p.cfg.TenantOverrides))
	for t := range p.cfg.TenantOverrides {
		tenants = append(tenants, t)
	}
	p.mu.RUnlock()

	for _, t := range tenants {
		cutoff := p.CutoffTime(t)
		n, err := p.purger.PurgeBefore(cutoff, t)
		if err != nil {
			return total, err
		}
		total += n
	}

	// Purge default (all tenants)
	cutoff := p.CutoffTime("")
	n, err := p.purger.PurgeBefore(cutoff, "")
	if err != nil {
		return total, err
	}
	return total + n, nil
}
