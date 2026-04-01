package ratelimit_test

import (
	"context"
	"testing"

	coreratelimit "github.com/openbotstack/openbotstack-core/access/ratelimit"
	"github.com/openbotstack/openbotstack-runtime/ratelimit"
)

func TestRedisLimiterSetQuota(t *testing.T) {
	limiter := ratelimit.NewInMemoryLimiter()
	ctx := context.Background()

	config := &coreratelimit.QuotaConfig{
		TenantRequestsPerMinute: 100,
		UserRequestsPerMinute:   10,
	}

	err := limiter.SetQuota(ctx, "tenant-1", config)
	if err != nil {
		t.Fatalf("SetQuota failed: %v", err)
	}
}

func TestRedisLimiterAllow(t *testing.T) {
	limiter := ratelimit.NewInMemoryLimiter()
	ctx := context.Background()

	config := &coreratelimit.QuotaConfig{
		TenantRequestsPerMinute: 100,
		UserRequestsPerMinute:   10,
	}
	_ = limiter.SetQuota(ctx, "tenant-1", config)

	key := coreratelimit.RateLimitKey{
		TenantID: "tenant-1",
	}

	result, err := limiter.Allow(ctx, key)
	if err != nil {
		t.Fatalf("Allow failed: %v", err)
	}

	if !result.Allowed {
		t.Error("Expected request to be allowed")
	}

	if result.Remaining <= 0 {
		t.Error("Expected positive remaining tokens")
	}
}

func TestRedisLimiterConsume(t *testing.T) {
	limiter := ratelimit.NewInMemoryLimiter()
	ctx := context.Background()

	config := &coreratelimit.QuotaConfig{
		TenantRequestsPerMinute: 100,
	}
	_ = limiter.SetQuota(ctx, "tenant-1", config)

	key := coreratelimit.RateLimitKey{
		TenantID: "tenant-1",
	}

	// Initialize bucket
	_, _ = limiter.Allow(ctx, key)

	// Consume tokens
	err := limiter.Consume(ctx, key, 10)
	if err != nil {
		t.Fatalf("Consume failed: %v", err)
	}

	remaining, _ := limiter.Remaining(ctx, key)
	if remaining != 90 {
		t.Errorf("Expected 90 remaining, got %d", remaining)
	}
}

func TestRedisLimiterInvalidKey(t *testing.T) {
	limiter := ratelimit.NewInMemoryLimiter()
	ctx := context.Background()

	key := coreratelimit.RateLimitKey{
		TenantID: "", // invalid
	}

	_, err := limiter.Allow(ctx, key)
	if err != coreratelimit.ErrInvalidKey {
		t.Errorf("Expected ErrInvalidKey, got %v", err)
	}
}

func TestRedisLimiterReset(t *testing.T) {
	limiter := ratelimit.NewInMemoryLimiter()
	ctx := context.Background()

	config := &coreratelimit.QuotaConfig{
		TenantRequestsPerMinute: 100,
	}
	_ = limiter.SetQuota(ctx, "tenant-1", config)

	key := coreratelimit.RateLimitKey{
		TenantID: "tenant-1",
	}

	// Initialize and consume
	_, _ = limiter.Allow(ctx, key)
	_ = limiter.Consume(ctx, key, 50)

	// Reset
	err := limiter.Reset(ctx, key)
	if err != nil {
		t.Fatalf("Reset failed: %v", err)
	}

	remaining, _ := limiter.Remaining(ctx, key)
	if remaining != 100 {
		t.Errorf("Expected 100 after reset, got %d", remaining)
	}
}
