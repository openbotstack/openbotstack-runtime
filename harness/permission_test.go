package harness

import (
	"context"
	"testing"

	"github.com/openbotstack/openbotstack-core/control/policy"
)

func TestPermissionChecker_WithRiskLevel(t *testing.T) {
	enforcer := policy.NewEnforcer()
	// Deny clinical skills
	enforcer.AddRule(policy.PolicyRule{
		ID:       "deny-clinical",
		TenantID: "t1",
		Effect:   "deny",
		Action:   "skill.execute",
		Resource: "*",
		Conditions: map[string]string{
			"risk_level": "clinical",
		},
		Priority: 10,
	})

	pc := NewPermissionChecker(nil, enforcer)

	// info skill should be allowed
	err := pc.Check(context.Background(), "summarize", "t1", map[string]string{"risk_level": "info"})
	if err != nil {
		t.Errorf("info skill should be allowed: %v", err)
	}

	// clinical skill should be denied
	err = pc.Check(context.Background(), "prescribe", "t1", map[string]string{"risk_level": "clinical"})
	if err == nil {
		t.Error("clinical skill should be denied")
	}
}

func TestPermissionChecker_WithoutAttributes(t *testing.T) {
	enforcer := policy.NewEnforcer()
	// No rules — default allow
	pc := NewPermissionChecker(nil, enforcer)

	err := pc.Check(context.Background(), "any-skill", "t1")
	if err != nil {
		t.Errorf("should be allowed with no rules: %v", err)
	}
}

func TestPermissionChecker_NoRiskLevel(t *testing.T) {
	enforcer := policy.NewEnforcer()
	enforcer.AddRule(policy.PolicyRule{
		ID:       "deny-clinical",
		TenantID: "t1",
		Effect:   "deny",
		Action:   "skill.execute",
		Resource: "*",
		Conditions: map[string]string{
			"risk_level": "clinical",
		},
		Priority: 10,
	})

	pc := NewPermissionChecker(nil, enforcer)

	// No attributes — risk_level condition won't match, so default allow
	err := pc.Check(context.Background(), "any-skill", "t1")
	if err != nil {
		t.Errorf("should be allowed when no risk_level in attrs: %v", err)
	}
}

func TestPermissionChecker_NilChecker(t *testing.T) {
	var pc *PermissionChecker
	err := pc.Check(context.Background(), "skill", "t1")
	if err != nil {
		t.Errorf("nil checker should always allow: %v", err)
	}
}
