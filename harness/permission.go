package harness

import (
	"context"
	"fmt"

	"github.com/openbotstack/openbotstack-core/control/policy"
	"github.com/openbotstack/openbotstack-core/execution"
)

// PermissionChecker enforces tool/skill execution permissions.
type PermissionChecker struct {
	config   *execution.PermissionConfig
	enforcer *policy.Enforcer
}

// NewPermissionChecker creates a permission checker.
// Either config or enforcer may be nil (no restrictions for that layer).
func NewPermissionChecker(config *execution.PermissionConfig, enforcer *policy.Enforcer) *PermissionChecker {
	return &PermissionChecker{
		config:   config,
		enforcer: enforcer,
	}
}

// Check verifies that a tool/skill execution is permitted.
// Checks both PermissionConfig (per-execution) and PolicyEnforcer (per-tenant).
func (pc *PermissionChecker) Check(ctx context.Context, name, tenantID string, extraAttrs ...map[string]string) error {
	if pc == nil {
		return nil
	}

	// Layer 1: Per-execution permission config
	if pc.config != nil {
		allowed, reason := pc.config.IsAllowed(name)
		if !allowed {
			return fmt.Errorf("permission denied for %q: %s", name, reason)
		}
	}

	// Layer 2: Tenant-level policy enforcement
	if pc.enforcer != nil && tenantID != "" {
		var attrs map[string]string
		if len(extraAttrs) > 0 {
			attrs = extraAttrs[0]
		}
		if err := pc.enforcer.Evaluate(ctx, tenantID, "skill.execute", "skill/"+name, attrs); err != nil {
			return fmt.Errorf("policy denied for %q: %w", name, err)
		}
	}

	return nil
}
