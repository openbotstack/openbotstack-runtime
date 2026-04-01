package ratelimit

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/openbotstack/openbotstack-core/access/ratelimit"
)

var allowScript = redis.NewScript(`
local tokensKey = KEYS[1]
local timestampKey = KEYS[2]

local rate = tonumber(ARGV[1])
local capacity = tonumber(ARGV[2])
local now = tonumber(ARGV[3])
local requested = tonumber(ARGV[4])

local fill_time = capacity / rate
local ttl = math.floor(fill_time * 2)
if ttl < 60 then
    ttl = 60
end

local last_tokens = tonumber(redis.call("get", tokensKey))
if last_tokens == nil then
  last_tokens = capacity
end

local last_refreshed = tonumber(redis.call("get", timestampKey))
if last_refreshed == nil then
  last_refreshed = 0
end

local delta = math.max(0, now - last_refreshed)
local filled_tokens = math.min(capacity, last_tokens + (delta * rate))
local allowed = filled_tokens >= requested

local new_tokens = filled_tokens
if allowed then
  new_tokens = filled_tokens - requested
end

redis.call("setex", tokensKey, ttl, new_tokens)
redis.call("setex", timestampKey, ttl, now)

return { allowed and 1 or 0, new_tokens }
`)

// RedisLimiter implements RateLimiter using Redis.
type RedisLimiter struct {
	client *redis.Client
	prefix string
}

// NewRedisLimiter creates a new Redis rate limiter.
func NewRedisLimiter(client *redis.Client) *RedisLimiter {
	return &RedisLimiter{
		client: client,
		prefix: "ratelimit:",
	}
}

// SetQuota stores the quota config in Redis.
func (r *RedisLimiter) SetQuota(ctx context.Context, tenantID string, config *ratelimit.QuotaConfig) error {
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	key := r.prefix + "config:" + tenantID
	return r.client.Set(ctx, key, data, 0).Err()
}

func (r *RedisLimiter) getConfig(ctx context.Context, tenantID string) (*ratelimit.QuotaConfig, error) {
	key := r.prefix + "config:" + tenantID
	data, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		if err == redis.Nil {
			return nil, nil // Default
		}
		return nil, err
	}
	var config ratelimit.QuotaConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

func (r *RedisLimiter) getRate(ctx context.Context, key ratelimit.RateLimitKey) int64 {
	rate := int64(1000)
	config, _ := r.getConfig(ctx, key.TenantID)
	if config != nil {
		if key.UserID != "" {
			rate = config.UserRequestsPerMinute
		} else {
			rate = config.TenantRequestsPerMinute
		}
	}
	return rate
}

// Allow checks if the request is allowed.
func (r *RedisLimiter) Allow(ctx context.Context, key ratelimit.RateLimitKey) (*ratelimit.RateLimitResult, error) {
	if key.TenantID == "" {
		return nil, ratelimit.ErrInvalidKey
	}

	rate := r.getRate(ctx, key)
	// rate is per minute. So tokens per second is rate / 60.
	ratePerSec := float64(rate) / 60.0

	return r.evaluate(ctx, key, ratePerSec, rate, 1)
}

// Consume deducts tokens from the quota.
func (r *RedisLimiter) Consume(ctx context.Context, key ratelimit.RateLimitKey, tokens int64) error {
	if key.TenantID == "" {
		return ratelimit.ErrInvalidKey
	}

	rate := r.getRate(ctx, key)
	ratePerSec := float64(rate) / 60.0

	res, err := r.evaluate(ctx, key, ratePerSec, rate, float64(tokens))
	if err != nil {
		return err
	}
	if !res.Allowed {
		return ratelimit.ErrRateLimitExceeded
	}
	return nil
}

func (r *RedisLimiter) evaluate(ctx context.Context, key ratelimit.RateLimitKey, rate float64, capacity int64, requested float64) (*ratelimit.RateLimitResult, error) {
	bucketKey := r.bucketKey(key)
	tokensKey := bucketKey + ":tokens"
	timestampKey := bucketKey + ":ts"

	now := time.Now().Unix()

	res, err := allowScript.Run(ctx, r.client, []string{tokensKey, timestampKey}, rate, capacity, now, requested).Result()
	if err != nil {
		return nil, fmt.Errorf("redis format error: %w", err)
	}

	resArr, ok := res.([]interface{})
	if !ok || len(resArr) != 2 {
		return nil, fmt.Errorf("unexpected allow script response")
	}

	allowed := resArr[0].(int64) == 1
	var remaining float64
	switch v := resArr[1].(type) {
	case int64:
		remaining = float64(v)
	case float64:
		remaining = v
	}

	result := &ratelimit.RateLimitResult{
		Remaining: int64(remaining),
		ResetAt:   time.Now().Add(time.Minute),
		Allowed:   allowed,
	}

	if !allowed {
		result.RetryAfter = time.Minute
	}

	return result, nil
}

// Remaining returns the remaining quota.
func (r *RedisLimiter) Remaining(ctx context.Context, key ratelimit.RateLimitKey) (int64, error) {
	if key.TenantID == "" {
		return 0, ratelimit.ErrInvalidKey
	}

	rate := r.getRate(ctx, key)
	ratePerSec := float64(rate) / 60.0

	res, err := r.evaluate(ctx, key, ratePerSec, rate, 0)
	if err != nil {
		return 0, err
	}
	return res.Remaining, nil
}

// Reset resets the quota for a key.
func (r *RedisLimiter) Reset(ctx context.Context, key ratelimit.RateLimitKey) error {
	bucketKey := r.bucketKey(key)
	return r.client.Del(ctx, bucketKey+":tokens", bucketKey+":ts").Err()
}

// bucketKey generates the key for a rate limit.
func (r *RedisLimiter) bucketKey(key ratelimit.RateLimitKey) string {
	if key.UserID != "" {
		return fmt.Sprintf("%suser:%s:%s", r.prefix, key.TenantID, key.UserID)
	}
	return fmt.Sprintf("%stenant:%s", r.prefix, key.TenantID)
}
