// Package ratelimit implements Redis-backed token bucket rate limiting.
package ratelimit

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/openbotstack/openbotstack-core/ratelimit"
)

// RedisLimiter implements RateLimiter using Redis (in-memory stub for now).
type RedisLimiter struct {
	mu      sync.RWMutex
	buckets map[string]*tokenBucket
	config  map[string]*ratelimit.QuotaConfig
}

type tokenBucket struct {
	tokens    int64
	lastFill  time.Time
	rateLimit int64 // tokens per minute
}

// NewRedisLimiter creates a new rate limiter.
// TODO: Replace with actual Redis client.
func NewRedisLimiter() *RedisLimiter {
	return &RedisLimiter{
		buckets: make(map[string]*tokenBucket),
		config:  make(map[string]*ratelimit.QuotaConfig),
	}
}

// SetQuota sets the quota config for a tenant.
func (r *RedisLimiter) SetQuota(ctx context.Context, tenantID string, config *ratelimit.QuotaConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.config[tenantID] = config
	return nil
}

// Allow checks if the request is allowed.
func (r *RedisLimiter) Allow(ctx context.Context, key ratelimit.RateLimitKey) (*ratelimit.RateLimitResult, error) {
	if key.TenantID == "" {
		return nil, ratelimit.ErrInvalidKey
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	bucketKey := r.bucketKey(key)
	bucket, exists := r.buckets[bucketKey]

	if !exists {
		// Create new bucket with default rate
		config := r.config[key.TenantID]
		rate := int64(1000) // default
		if config != nil {
			if key.UserID != "" {
				rate = config.UserRequestsPerMinute
			} else {
				rate = config.TenantRequestsPerMinute
			}
		}

		bucket = &tokenBucket{
			tokens:    rate,
			lastFill:  time.Now(),
			rateLimit: rate,
		}
		r.buckets[bucketKey] = bucket
	}

	// Refill tokens based on elapsed time
	elapsed := time.Since(bucket.lastFill)
	refill := int64(elapsed.Minutes() * float64(bucket.rateLimit))
	if refill > 0 {
		bucket.tokens = min(bucket.tokens+refill, bucket.rateLimit)
		bucket.lastFill = time.Now()
	}

	result := &ratelimit.RateLimitResult{
		Remaining: bucket.tokens,
		ResetAt:   time.Now().Add(time.Minute),
	}

	if bucket.tokens > 0 {
		result.Allowed = true
	} else {
		result.Allowed = false
		result.RetryAfter = time.Minute
	}

	return result, nil
}

// Consume deducts tokens from the quota.
func (r *RedisLimiter) Consume(ctx context.Context, key ratelimit.RateLimitKey, tokens int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	bucketKey := r.bucketKey(key)
	bucket, exists := r.buckets[bucketKey]

	if !exists {
		return ratelimit.ErrQuotaNotFound
	}

	if bucket.tokens < tokens {
		return ratelimit.ErrRateLimitExceeded
	}

	bucket.tokens -= tokens
	return nil
}

// Remaining returns the remaining quota.
func (r *RedisLimiter) Remaining(ctx context.Context, key ratelimit.RateLimitKey) (int64, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	bucketKey := r.bucketKey(key)
	bucket, exists := r.buckets[bucketKey]

	if !exists {
		return 0, ratelimit.ErrQuotaNotFound
	}

	return bucket.tokens, nil
}

// Reset resets the quota for a key.
func (r *RedisLimiter) Reset(ctx context.Context, key ratelimit.RateLimitKey) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	bucketKey := r.bucketKey(key)
	bucket, exists := r.buckets[bucketKey]

	if !exists {
		return ratelimit.ErrQuotaNotFound
	}

	bucket.tokens = bucket.rateLimit
	bucket.lastFill = time.Now()
	return nil
}

// bucketKey generates the Redis key for a rate limit.
func (r *RedisLimiter) bucketKey(key ratelimit.RateLimitKey) string {
	if key.UserID != "" {
		return fmt.Sprintf("rate:user:%s:%s", key.TenantID, key.UserID)
	}
	return fmt.Sprintf("rate:tenant:%s", key.TenantID)
}
