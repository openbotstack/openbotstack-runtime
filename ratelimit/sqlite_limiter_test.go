package ratelimit

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/access/ratelimit"
	"github.com/openbotstack/openbotstack-runtime/persistence"
)

func setupLimiterTestDB(t *testing.T) *persistence.DB {
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

func setupLimiter(t *testing.T) (*SQLiteRateLimiter, *persistence.DB) {
	t.Helper()
	db := setupLimiterTestDB(t)
	quotaStore := NewSQLiteQuotaStore(db.DB)
	limiter := NewSQLiteRateLimiter(db.DB, quotaStore)
	return limiter, db
}

func TestAllowInvalidKey(t *testing.T) {
	limiter, db := setupLimiter(t)
	defer func() { _ = db.Close() }()

	_, err := limiter.Allow(context.Background(), ratelimit.RateLimitKey{})
	if !errors.Is(err, ratelimit.ErrInvalidKey) {
		t.Fatalf("expected ErrInvalidKey, got: %v", err)
	}
}

func TestAllowQuotaNotFound(t *testing.T) {
	limiter, db := setupLimiter(t)
	defer func() { _ = db.Close() }()

	key := ratelimit.RateLimitKey{TenantID: "unknown"}
	_, err := limiter.Allow(context.Background(), key)
	if !errors.Is(err, ratelimit.ErrQuotaNotFound) {
		t.Fatalf("expected ErrQuotaNotFound, got: %v", err)
	}
}

func TestAllowWithinLimit(t *testing.T) {
	limiter, db := setupLimiter(t)
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	quotaStore := NewSQLiteQuotaStore(db.DB)
	if err := quotaStore.SetQuota(ctx, "t1", &ratelimit.QuotaConfig{TenantRequestsPerMinute: 5}); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}

	result, err := limiter.Allow(ctx, ratelimit.RateLimitKey{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if !result.Allowed {
		t.Error("expected Allowed=true")
	}
	// Allow is read-only: does not consume tokens
	if result.Remaining != 5 {
		t.Errorf("Remaining = %d, want 5 (Allow does not consume)", result.Remaining)
	}
}

func TestAllowOverLimit(t *testing.T) {
	limiter, db := setupLimiter(t)
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	quotaStore := NewSQLiteQuotaStore(db.DB)
	if err := quotaStore.SetQuota(ctx, "t1", &ratelimit.QuotaConfig{TenantRequestsPerMinute: 2}); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}

	// Consume both tokens
	if err := limiter.Consume(ctx, ratelimit.RateLimitKey{TenantID: "t1"}, 1); err != nil {
		t.Fatalf("Consume 1: %v", err)
	}
	if err := limiter.Consume(ctx, ratelimit.RateLimitKey{TenantID: "t1"}, 1); err != nil {
		t.Fatalf("Consume 2: %v", err)
	}

	// Allow should report not allowed
	result, err := limiter.Allow(ctx, ratelimit.RateLimitKey{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Allow: %v", err)
	}
	if result.Allowed {
		t.Error("expected Allowed=false (over limit)")
	}
}

func TestConsume(t *testing.T) {
	limiter, db := setupLimiter(t)
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	quotaStore := NewSQLiteQuotaStore(db.DB)
	if err := quotaStore.SetQuota(ctx, "t1", &ratelimit.QuotaConfig{TenantRequestsPerMinute: 10}); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}

	err := limiter.Consume(ctx, ratelimit.RateLimitKey{TenantID: "t1"}, 3)
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}

	remaining, err := limiter.Remaining(ctx, ratelimit.RateLimitKey{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Remaining: %v", err)
	}
	if remaining != 7 {
		t.Errorf("Remaining = %d, want 7", remaining)
	}
}

func TestConsumeExceedsLimit(t *testing.T) {
	limiter, db := setupLimiter(t)
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	quotaStore := NewSQLiteQuotaStore(db.DB)
	if err := quotaStore.SetQuota(ctx, "t1", &ratelimit.QuotaConfig{TenantRequestsPerMinute: 5}); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}

	err := limiter.Consume(ctx, ratelimit.RateLimitKey{TenantID: "t1"}, 10)
	if !errors.Is(err, ratelimit.ErrRateLimitExceeded) {
		t.Fatalf("expected ErrRateLimitExceeded, got: %v", err)
	}
}

func TestRemaining(t *testing.T) {
	limiter, db := setupLimiter(t)
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	quotaStore := NewSQLiteQuotaStore(db.DB)
	if err := quotaStore.SetQuota(ctx, "t1", &ratelimit.QuotaConfig{TenantRequestsPerMinute: 100}); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}

	remaining, err := limiter.Remaining(ctx, ratelimit.RateLimitKey{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Remaining: %v", err)
	}
	if remaining != 100 {
		t.Errorf("Remaining = %d, want 100 (fresh bucket)", remaining)
	}
}

func TestReset(t *testing.T) {
	limiter, db := setupLimiter(t)
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	quotaStore := NewSQLiteQuotaStore(db.DB)
	if err := quotaStore.SetQuota(ctx, "t1", &ratelimit.QuotaConfig{TenantRequestsPerMinute: 5}); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}

	if err := limiter.Consume(ctx, ratelimit.RateLimitKey{TenantID: "t1"}, 3); err != nil {
		t.Fatalf("Consume: %v", err)
	}

	err := limiter.Reset(ctx, ratelimit.RateLimitKey{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Reset: %v", err)
	}

	remaining, _ := limiter.Remaining(ctx, ratelimit.RateLimitKey{TenantID: "t1"})
	if remaining != 5 {
		t.Errorf("Remaining after reset = %d, want 5", remaining)
	}
}

func TestTokenRefill(t *testing.T) {
	db := setupLimiterTestDB(t)
	defer func() { _ = db.Close() }()
	ctx := context.Background()

	quotaStore := NewSQLiteQuotaStore(db.DB)
	if err := quotaStore.SetQuota(ctx, "t1", &ratelimit.QuotaConfig{TenantRequestsPerMinute: 60}); err != nil {
		t.Fatalf("SetQuota: %v", err)
	}

	pastTime := time.Now().Add(-30 * time.Second).UTC().Format(time.RFC3339Nano)
	if _, err := db.Exec(`INSERT INTO rate_limits (key, tokens, last_fill, rate_limit, window_start)
		VALUES (?, 0, ?, 60, ?)`, "tenant:t1", pastTime, pastTime); err != nil {
		t.Fatalf("insert rate limit: %v", err)
	}

	limiter := NewSQLiteRateLimiter(db.DB, quotaStore)
	remaining, err := limiter.Remaining(ctx, ratelimit.RateLimitKey{TenantID: "t1"})
	if err != nil {
		t.Fatalf("Remaining: %v", err)
	}
	if remaining < 25 || remaining > 30 {
		t.Errorf("Remaining = %d, expected ~30 (30s refill at 60/min)", remaining)
	}
}
