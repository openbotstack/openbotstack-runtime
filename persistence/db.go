package persistence

import (
	"database/sql"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

// DB wraps *sql.DB with SQLite-specific setup and schema migration.
type DB struct {
	*sql.DB
}

// Open creates a SQLite connection at dbPath.
// Use ":memory:" for an in-memory database.
func Open(dbPath string) (*DB, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", dbPath, err)
	}

	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}
	if _, err := db.Exec("PRAGMA busy_timeout=5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("set busy timeout: %w", err)
	}

	// SQLite: single writer constraint. WAL allows concurrent readers.
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	return &DB{DB: db}, nil
}

// Migrate creates all tables and indexes if they do not exist.
func (db *DB) Migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id          TEXT PRIMARY KEY,
			tenant_id   TEXT NOT NULL,
			user_id     TEXT NOT NULL DEFAULT '',
			request_id  TEXT NOT NULL DEFAULT '',
			action      TEXT NOT NULL,
			resource    TEXT NOT NULL DEFAULT '',
			outcome     TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0,
			metadata    TEXT NOT NULL DEFAULT '{}',
			timestamp   TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_tenant_time ON audit_logs(tenant_id, timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_request ON audit_logs(request_id)`,
		`CREATE TABLE IF NOT EXISTS quotas (
			tenant_id                  TEXT PRIMARY KEY,
			tenant_tokens_per_minute   INTEGER NOT NULL DEFAULT 0,
			tenant_requests_per_minute INTEGER NOT NULL DEFAULT 0,
			user_requests_per_minute   INTEGER NOT NULL DEFAULT 0,
			user_tokens_per_minute     INTEGER NOT NULL DEFAULT 0,
			updated_at                 TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS rate_limits (
			key          TEXT PRIMARY KEY,
			tokens       INTEGER NOT NULL,
			last_fill    TEXT NOT NULL,
			rate_limit   INTEGER NOT NULL,
			window_start TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS session_entries (
			id         TEXT PRIMARY KEY,
			session_id TEXT NOT NULL,
			tenant_id  TEXT NOT NULL DEFAULT '',
			content    TEXT NOT NULL,
			tags       TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL DEFAULT '',
			ttl        INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_session_entries_session ON session_entries(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_session_entries_tenant ON session_entries(tenant_id)`,
		`CREATE TABLE IF NOT EXISTS tenants (
			id         TEXT PRIMARY KEY,
			name       TEXT NOT NULL,
			created_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id         TEXT PRIMARY KEY,
			tenant_id  TEXT NOT NULL,
			name       TEXT NOT NULL,
			role       TEXT NOT NULL DEFAULT 'member',
			created_at TEXT NOT NULL DEFAULT '',
			FOREIGN KEY (tenant_id) REFERENCES tenants(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_users_tenant ON users(tenant_id)`,
		`CREATE TABLE IF NOT EXISTS api_keys (
			id         TEXT PRIMARY KEY,
			tenant_id  TEXT NOT NULL,
			user_id    TEXT NOT NULL,
			key_prefix TEXT NOT NULL,
			key_hash   TEXT NOT NULL UNIQUE,
			name       TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL DEFAULT '',
			expires_at TEXT NOT NULL DEFAULT '',
			revoked    INTEGER NOT NULL DEFAULT 0,
			FOREIGN KEY (tenant_id) REFERENCES tenants(id),
			FOREIGN KEY (user_id) REFERENCES users(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_api_keys_hash ON api_keys(key_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_api_keys_tenant ON api_keys(tenant_id)`,
	}
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("migrate begin tx: %w", err)
	}
	defer tx.Rollback()

	for i, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return fmt.Errorf("migrate statement %d: %w", i, err)
		}
	}
	return tx.Commit()
}

// MigrateTenantColumn adds tenant_id to session_entries if it does not already exist.
// This handles upgrades from schemas that predate multi-tenancy.
func (db *DB) MigrateTenantColumn() error {
	_, err := db.Exec("ALTER TABLE session_entries ADD COLUMN tenant_id TEXT NOT NULL DEFAULT ''")
	if err != nil {
		// SQLite returns "duplicate column name" if column already exists
		if strings.Contains(err.Error(), "duplicate column name") {
			return nil
		}
		return fmt.Errorf("add tenant_id column: %w", err)
	}
	// Add index if column was just added
	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_session_entries_tenant ON session_entries(tenant_id)"); err != nil {
		return fmt.Errorf("create tenant index after migration: %w", err)
	}
	return nil
}
