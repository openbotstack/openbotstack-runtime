// Package ratelimit implements rate limiting.
package ratelimit

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	ratelimit "github.com/openbotstack/openbotstack-core/access/ratelimit"
)

// SQLiteQuotaStore implements ratelimit.QuotaStore using SQLite.
type SQLiteQuotaStore struct {
	db *sql.DB
}

// NewSQLiteQuotaStore creates a new SQLite-backed quota store.
func NewSQLiteQuotaStore(db *sql.DB) *SQLiteQuotaStore {
	return &SQLiteQuotaStore{db: db}
}

// GetQuota retrieves the quota configuration for a tenant.
func (s *SQLiteQuotaStore) GetQuota(ctx context.Context, tenantID string) (*ratelimit.QuotaConfig, error) {
	var config ratelimit.QuotaConfig
	err := s.db.QueryRowContext(ctx, `
		SELECT tenant_tokens_per_minute, tenant_requests_per_minute,
		       user_requests_per_minute, user_tokens_per_minute
		FROM quotas WHERE tenant_id = ?`, tenantID,
	).Scan(
		&config.TenantTokensPerMinute,
		&config.TenantRequestsPerMinute,
		&config.UserRequestsPerMinute,
		&config.UserTokensPerMinute,
	)
	if err == sql.ErrNoRows {
		return nil, ratelimit.ErrQuotaNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get quota for %s: %w", tenantID, err)
	}
	return &config, nil
}

// SetQuota creates or updates the quota configuration for a tenant.
func (s *SQLiteQuotaStore) SetQuota(ctx context.Context, tenantID string, config *ratelimit.QuotaConfig) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		INSERT OR REPLACE INTO quotas
			(tenant_id, tenant_tokens_per_minute, tenant_requests_per_minute,
			 user_requests_per_minute, user_tokens_per_minute, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		tenantID,
		config.TenantTokensPerMinute,
		config.TenantRequestsPerMinute,
		config.UserRequestsPerMinute,
		config.UserTokensPerMinute,
		now,
	)
	if err != nil {
		return fmt.Errorf("set quota for %s: %w", tenantID, err)
	}
	return nil
}
