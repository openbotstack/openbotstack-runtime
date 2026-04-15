package ratelimit

import (
	"context"
	"errors"
	"testing"

	ratelimit "github.com/openbotstack/openbotstack-core/access/ratelimit"
	"github.com/openbotstack/openbotstack-runtime/persistence"
)

func setupQuotaTestDB(t *testing.T) *persistence.DB {
	t.Helper()
	db, err := persistence.Open(":memory:")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func TestGetQuotaNotFound(t *testing.T) {
	db := setupQuotaTestDB(t)
	defer func() { _ = db.Close() }()

	store := NewSQLiteQuotaStore(db.DB)
	_, err := store.GetQuota(context.Background(), "nonexistent")
	if !errors.Is(err, ratelimit.ErrQuotaNotFound) {
		t.Fatalf("expected ErrQuotaNotFound, got: %v", err)
	}
}

func TestSetAndGetQuota(t *testing.T) {
	db := setupQuotaTestDB(t)
	defer func() { _ = db.Close() }()

	store := NewSQLiteQuotaStore(db.DB)
	ctx := context.Background()

	config := &ratelimit.QuotaConfig{
		TenantRequestsPerMinute: 100,
		TenantTokensPerMinute:   5000,
		UserRequestsPerMinute:   20,
		UserTokensPerMinute:     1000,
	}

	if err := store.SetQuota(ctx, "tenant-1", config); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}

	got, err := store.GetQuota(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("GetQuota: %v", err)
	}

	if got.TenantRequestsPerMinute != 100 {
		t.Errorf("TenantRequestsPerMinute = %d, want 100", got.TenantRequestsPerMinute)
	}
	if got.TenantTokensPerMinute != 5000 {
		t.Errorf("TenantTokensPerMinute = %d, want 5000", got.TenantTokensPerMinute)
	}
	if got.UserRequestsPerMinute != 20 {
		t.Errorf("UserRequestsPerMinute = %d, want 20", got.UserRequestsPerMinute)
	}
	if got.UserTokensPerMinute != 1000 {
		t.Errorf("UserTokensPerMinute = %d, want 1000", got.UserTokensPerMinute)
	}
}

func TestSetQuotaUpdates(t *testing.T) {
	db := setupQuotaTestDB(t)
	defer func() { _ = db.Close() }()

	store := NewSQLiteQuotaStore(db.DB)
	ctx := context.Background()

	config1 := &ratelimit.QuotaConfig{TenantRequestsPerMinute: 50}
	if err := store.SetQuota(ctx, "t1", config1); err != nil {
		t.Fatalf("SetQuota 1: %v", err)
	}

	config2 := &ratelimit.QuotaConfig{TenantRequestsPerMinute: 200}
	if err := store.SetQuota(ctx, "t1", config2); err != nil {
		t.Fatalf("SetQuota 2: %v", err)
	}

	got, err := store.GetQuota(ctx, "t1")
	if err != nil {
		t.Fatalf("GetQuota: %v", err)
	}
	if got.TenantRequestsPerMinute != 200 {
		t.Errorf("TenantRequestsPerMinute = %d, want 200 (updated value)", got.TenantRequestsPerMinute)
	}
}
