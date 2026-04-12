// Package ratelimit implements rate limiting.
package ratelimit

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	ratelimit "github.com/openbotstack/openbotstack-core/access/ratelimit"
)

// SQLiteRateLimiter implements ratelimit.RateLimiter using a SQLite-backed token bucket.
type SQLiteRateLimiter struct {
	db         *sql.DB
	quotaStore ratelimit.QuotaStore
}

// NewSQLiteRateLimiter creates a new SQLite-backed rate limiter.
func NewSQLiteRateLimiter(db *sql.DB, quotaStore ratelimit.QuotaStore) *SQLiteRateLimiter {
	return &SQLiteRateLimiter{db: db, quotaStore: quotaStore}
}

// bucketKey generates the storage key for a RateLimitKey.
func bucketKey(key ratelimit.RateLimitKey) string {
	if key.UserID != "" {
		return fmt.Sprintf("tenant:%s:user:%s", key.TenantID, key.UserID)
	}
	return fmt.Sprintf("tenant:%s", key.TenantID)
}

// rateForQuota returns the rate limit to use based on the key scope.
func rateForQuota(config *ratelimit.QuotaConfig, key ratelimit.RateLimitKey) int64 {
	if key.UserID != "" {
		return config.UserRequestsPerMinute
	}
	return config.TenantRequestsPerMinute
}

// loadQuota fetches the quota config for the given tenant.
// Must be called BEFORE starting a transaction to avoid SQLite single-connection deadlock.
func (l *SQLiteRateLimiter) loadQuota(ctx context.Context, tenantID string) (*ratelimit.QuotaConfig, error) {
	return l.quotaStore.GetQuota(ctx, tenantID)
}

// refillBucket reads the bucket within a transaction, computes token refill,
// and returns current state. If the bucket does not exist, it creates one
// using the provided quota config.
func refillBucket(ctx context.Context, tx *sql.Tx, key ratelimit.RateLimitKey, config *ratelimit.QuotaConfig) (tokens int64, lastFill time.Time, rateLimit int64, err error) {
	bk := bucketKey(key)

	var storedTokens int64
	var storedLastFill string
	var storedRateLimit int64

	err = tx.QueryRowContext(ctx,
		"SELECT tokens, last_fill, rate_limit FROM rate_limits WHERE key = ?", bk,
	).Scan(&storedTokens, &storedLastFill, &storedRateLimit)
	if err == sql.ErrNoRows {
		// Create a fresh bucket using the pre-loaded quota config.
		rate := rateForQuota(config, key)
		now := time.Now().UTC()
		_, err = tx.ExecContext(ctx,
			"INSERT INTO rate_limits (key, tokens, last_fill, rate_limit, window_start) VALUES (?, ?, ?, ?, ?)",
			bk, rate, now.Format(time.RFC3339Nano), rate, now.Format(time.RFC3339Nano),
		)
		if err != nil {
			return 0, time.Time{}, 0, fmt.Errorf("insert rate limit bucket %s: %w", bk, err)
		}
		return rate, now, rate, nil
	}
	if err != nil {
		return 0, time.Time{}, 0, fmt.Errorf("read rate limit bucket %s: %w", bk, err)
	}

	fillTime, perr := time.Parse(time.RFC3339Nano, storedLastFill)
	if perr != nil {
		return 0, time.Time{}, 0, fmt.Errorf("parse last_fill: %w", perr)
	}

	elapsed := time.Since(fillTime)
	refill := int64(elapsed.Seconds() * float64(storedRateLimit) / 60.0)
	if refill > 0 {
		storedTokens = min(storedTokens+refill, storedRateLimit)
		fillTime = time.Now().UTC()
	}

	return storedTokens, fillTime, storedRateLimit, nil
}

// Allow checks if the request is allowed, consuming one token if so.
func (l *SQLiteRateLimiter) Allow(ctx context.Context, key ratelimit.RateLimitKey) (*ratelimit.RateLimitResult, error) {
	if key.TenantID == "" {
		return nil, ratelimit.ErrInvalidKey
	}

	// Load quota BEFORE starting a transaction to avoid single-connection deadlock.
	config, err := l.loadQuota(ctx, key.TenantID)
	if err != nil {
		return nil, err
	}

	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	tokens, fillTime, _, err := refillBucket(ctx, tx, key, config)
	if err != nil {
		return nil, err
	}

	result := &ratelimit.RateLimitResult{
		Remaining: tokens,
		ResetAt:   time.Now().Add(time.Minute),
	}

	if tokens > 0 {
		result.Allowed = true
		tokens--
		result.Remaining = tokens
	} else {
		result.Allowed = false
		result.RetryAfter = time.Minute
	}

	bk := bucketKey(key)
	_, err = tx.ExecContext(ctx,
		"UPDATE rate_limits SET tokens = ?, last_fill = ? WHERE key = ?",
		tokens, fillTime.Format(time.RFC3339Nano), bk,
	)
	if err != nil {
		return nil, fmt.Errorf("update rate limit bucket %s: %w", bk, err)
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}

	return result, nil
}

// Consume deducts the specified number of tokens from the quota.
func (l *SQLiteRateLimiter) Consume(ctx context.Context, key ratelimit.RateLimitKey, tokens int64) error {
	if key.TenantID == "" {
		return ratelimit.ErrInvalidKey
	}

	// Load quota BEFORE starting a transaction to avoid single-connection deadlock.
	config, err := l.loadQuota(ctx, key.TenantID)
	if err != nil {
		return err
	}

	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	current, fillTime, _, err := refillBucket(ctx, tx, key, config)
	if err != nil {
		return err
	}

	if current < tokens {
		return ratelimit.ErrRateLimitExceeded
	}

	remaining := current - tokens
	bk := bucketKey(key)
	_, err = tx.ExecContext(ctx,
		"UPDATE rate_limits SET tokens = ?, last_fill = ? WHERE key = ?",
		remaining, fillTime.Format(time.RFC3339Nano), bk,
	)
	if err != nil {
		return fmt.Errorf("update rate limit bucket %s: %w", bk, err)
	}

	return tx.Commit()
}

// Remaining returns the remaining quota (read-only, no side effects).
func (l *SQLiteRateLimiter) Remaining(ctx context.Context, key ratelimit.RateLimitKey) (int64, error) {
	if key.TenantID == "" {
		return 0, ratelimit.ErrInvalidKey
	}

	// Load quota BEFORE starting a transaction to avoid single-connection deadlock.
	config, err := l.loadQuota(ctx, key.TenantID)
	if err != nil {
		return 0, err
	}

	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	tokens, _, _, err := refillBucket(ctx, tx, key, config)
	if err != nil {
		return 0, err
	}

	return tokens, nil
}

// Reset deletes the rate limit bucket so the next request starts fresh.
func (l *SQLiteRateLimiter) Reset(ctx context.Context, key ratelimit.RateLimitKey) error {
	if key.TenantID == "" {
		return ratelimit.ErrInvalidKey
	}

	bk := bucketKey(key)
	_, err := l.db.ExecContext(ctx, "DELETE FROM rate_limits WHERE key = ?", bk)
	if err != nil {
		return fmt.Errorf("reset rate limit bucket %s: %w", bk, err)
	}
	return nil
}
