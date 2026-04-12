package persistence

import (
	"database/sql"
	"fmt"

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

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(2)

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
			content    TEXT NOT NULL,
			tags       TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL DEFAULT '',
			ttl        INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE INDEX IF NOT EXISTS idx_session_entries_session ON session_entries(session_id)`,
	}
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}
