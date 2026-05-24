package adapters

import (
	"time"

	"github.com/openbotstack/openbotstack-runtime/api"
	"github.com/openbotstack/openbotstack-runtime/audit"
)

// RetentionManagerAdapter adapts audit.RetentionPolicy to api.RetentionManager.
type RetentionManagerAdapter struct {
	Policy *audit.RetentionPolicy
}

func (a *RetentionManagerAdapter) RetentionConfig() api.RetentionConfigSnapshot {
	cfg := a.Policy.Config()
	return api.RetentionConfigSnapshot{
		Enabled:         cfg.Enabled,
		DefaultDays:     cfg.DefaultDays,
		TenantOverrides: cfg.TenantOverrides,
	}
}

func (a *RetentionManagerAdapter) SetTenantOverride(tenantID string, days int) {
	a.Policy.SetTenantOverride(tenantID, days)
}

func (a *RetentionManagerAdapter) RemoveTenantOverride(tenantID string) {
	a.Policy.RemoveTenantOverride(tenantID)
}

func (a *RetentionManagerAdapter) PurgeExpired() (int64, error) {
	return a.Policy.PurgeExpired()
}

// AuditPurger adapts execution_logs.PurgeBefore(ctx, time, string) to audit.Purger interface.
type AuditPurger struct {
	PurgerFunc func(cutoff time.Time, tenantID string) (int64, error)
}

func (a *AuditPurger) PurgeBefore(cutoff time.Time, tenantID string) (int64, error) {
	return a.PurgerFunc(cutoff, tenantID)
}
