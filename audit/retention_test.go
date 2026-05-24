package audit

import (
	"testing"
	"time"
)

func TestRetentionPolicy_DefaultConfig(t *testing.T) {
	cfg := DefaultRetentionConfig()
	if cfg.DefaultDays != 90 {
		t.Errorf("DefaultDays = %d, want 90", cfg.DefaultDays)
	}
	if cfg.Enabled {
		t.Error("should be disabled by default")
	}
}

func TestRetentionPolicy_GetDaysForTenant(t *testing.T) {
	cfg := RetentionConfig{
		Enabled:     true,
		DefaultDays: 90,
		TenantOverrides: map[string]int{
			"tenant-compliance": 365,
			"tenant-short":      30,
		},
	}
	policy := NewRetentionPolicy(cfg, nil)

	tests := []struct {
		tenant string
		want   int
	}{
		{"tenant-compliance", 365},
		{"tenant-short", 30},
		{"tenant-default", 90},
		{"", 90},
	}

	for _, tt := range tests {
		got := policy.DaysForTenant(tt.tenant)
		if got != tt.want {
			t.Errorf("DaysForTenant(%q) = %d, want %d", tt.tenant, got, tt.want)
		}
	}
}

func TestRetentionPolicy_CutoffTime(t *testing.T) {
	cfg := RetentionConfig{
		Enabled:     true,
		DefaultDays: 30,
	}
	policy := NewRetentionPolicy(cfg, nil)

	cutoff := policy.CutoffTime("")
	if cutoff.IsZero() {
		t.Error("CutoffTime should not be zero")
	}

	now := time.Now()
	expectedCutoff := now.AddDate(0, 0, -30)
	diff := cutoff.Sub(expectedCutoff)
	if diff < -time.Minute || diff > time.Minute {
		t.Errorf("CutoffTime = %v, expected ~%v", cutoff, expectedCutoff)
	}
}

func TestRetentionPolicy_ShouldPurge(t *testing.T) {
	cfg := RetentionConfig{
		Enabled:     true,
		DefaultDays: 7,
	}
	policy := NewRetentionPolicy(cfg, nil)

	now := time.Now()

	old := now.AddDate(0, 0, -10)
	if !policy.ShouldPurge(old, "") {
		t.Error("event 10 days old should be purged with 7-day retention")
	}

	recent := now.AddDate(0, 0, -3)
	if policy.ShouldPurge(recent, "") {
		t.Error("event 3 days old should not be purged with 7-day retention")
	}

	boundary := now.AddDate(0, 0, -7)
	if !policy.ShouldPurge(boundary, "") {
		t.Error("event at 7-day boundary should be purged")
	}
}

func TestRetentionPolicy_DisabledNeverPurges(t *testing.T) {
	cfg := RetentionConfig{
		Enabled:     false,
		DefaultDays: 1,
	}
	policy := NewRetentionPolicy(cfg, nil)

	old := time.Now().AddDate(-1, 0, 0)
	if policy.ShouldPurge(old, "") {
		t.Error("disabled policy should never purge")
	}
}

func TestRetentionPolicy_TenantOverrideCutoff(t *testing.T) {
	cfg := RetentionConfig{
		Enabled:     true,
		DefaultDays: 7,
		TenantOverrides: map[string]int{
			"long-lived": 365,
		},
	}
	policy := NewRetentionPolicy(cfg, nil)

	now := time.Now()
	ts := now.AddDate(0, 0, -30)

	if !policy.ShouldPurge(ts, "regular-tenant") {
		t.Error("30-day-old should purge for default 7-day tenant")
	}
	if policy.ShouldPurge(ts, "long-lived") {
		t.Error("30-day-old should NOT purge for 365-day tenant")
	}
}

func TestRetentionPolicy_SetTenantOverride(t *testing.T) {
	cfg := RetentionConfig{
		Enabled:     true,
		DefaultDays: 90,
	}
	policy := NewRetentionPolicy(cfg, nil)

	policy.SetTenantOverride("new-tenant", 180)
	got := policy.DaysForTenant("new-tenant")
	if got != 180 {
		t.Errorf("DaysForTenant after SetTenantOverride = %d, want 180", got)
	}
}

func TestRetentionPolicy_RemoveTenantOverride(t *testing.T) {
	cfg := RetentionConfig{
		Enabled:         true,
		DefaultDays:     90,
		TenantOverrides: map[string]int{"special": 365},
	}
	policy := NewRetentionPolicy(cfg, nil)

	policy.RemoveTenantOverride("special")
	got := policy.DaysForTenant("special")
	if got != 90 {
		t.Errorf("DaysForTenant after removal = %d, want default 90", got)
	}
}

func TestRetentionPolicy_Config(t *testing.T) {
	cfg := RetentionConfig{
		Enabled:     true,
		DefaultDays: 90,
		TenantOverrides: map[string]int{
			"t1": 30,
		},
	}
	policy := NewRetentionPolicy(cfg, nil)

	snapshot := policy.Config()
	if snapshot.DefaultDays != 90 {
		t.Errorf("Config DefaultDays = %d, want 90", snapshot.DefaultDays)
	}
	if snapshot.TenantOverrides["t1"] != 30 {
		t.Errorf("Config TenantOverrides[t1] = %d, want 30", snapshot.TenantOverrides["t1"])
	}

	// Verify it's a copy
	snapshot.TenantOverrides["t2"] = 60
	got := policy.DaysForTenant("t2")
	if got != 90 {
		t.Error("Config() should return a copy, not a reference")
	}
}

type mockPurger struct {
	purged []purgeCall
}

type purgeCall struct {
	cutoff   time.Time
	tenantID string
}

func (m *mockPurger) PurgeBefore(cutoff time.Time, tenantID string) (int64, error) {
	m.purged = append(m.purged, purgeCall{cutoff: cutoff, tenantID: tenantID})
	return 5, nil
}

func TestRetentionPolicy_PurgeExpired(t *testing.T) {
	mp := &mockPurger{}
	cfg := RetentionConfig{
		Enabled:     true,
		DefaultDays: 30,
		TenantOverrides: map[string]int{
			"long-lived": 365,
		},
	}
	policy := NewRetentionPolicy(cfg, mp)

	total, err := policy.PurgeExpired()
	if err != nil {
		t.Fatalf("PurgeExpired failed: %v", err)
	}

	// 1 call for long-lived tenant + 1 call for default
	if len(mp.purged) != 2 {
		t.Errorf("PurgeExpired called purger %d times, want 2", len(mp.purged))
	}

	if total != 10 { // 5 per call * 2 calls
		t.Errorf("PurgeExpired total = %d, want 10", total)
	}
}

func TestRetentionPolicy_PurgeExpired_Disabled(t *testing.T) {
	mp := &mockPurger{}
	cfg := RetentionConfig{
		Enabled:     false,
		DefaultDays: 30,
	}
	policy := NewRetentionPolicy(cfg, mp)

	total, err := policy.PurgeExpired()
	if err != nil {
		t.Fatalf("PurgeExpired failed: %v", err)
	}
	if total != 0 {
		t.Errorf("disabled PurgeExpired = %d, want 0", total)
	}
	if len(mp.purged) != 0 {
		t.Error("disabled policy should not call purger")
	}
}

func TestRetentionPolicy_PurgeExpired_NilPurger(t *testing.T) {
	cfg := RetentionConfig{
		Enabled:     true,
		DefaultDays: 30,
	}
	policy := NewRetentionPolicy(cfg, nil)

	total, err := policy.PurgeExpired()
	if err != nil {
		t.Fatalf("PurgeExpired with nil purger failed: %v", err)
	}
	if total != 0 {
		t.Errorf("nil purger PurgeExpired = %d, want 0", total)
	}
}
