package persistence

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenInMemory(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open in-memory: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestMigrateCreatesTables(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	tables := []string{"audit_logs", "quotas", "rate_limits", "session_entries", "tenants", "users", "api_keys"}
	for _, table := range tables {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err == sql.ErrNoRows {
			t.Errorf("table %q not created", table)
		} else if err != nil {
			t.Fatalf("checking table %q: %v", table, err)
		}
	}
}

func TestMigrateIdempotent(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.Migrate(); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
}

// TestOpenCreatesMissingParentDir verifies Open creates the parent directory
// when it does not exist. This prevents SQLITE_CANTOPEN(14) on first run
// when the default path is e.g. "data/openbotstack.db" and data/ is absent.
func TestOpenCreatesMissingParentDir(t *testing.T) {
	base := t.TempDir()
	// nested/deep does not exist yet
	dbPath := filepath.Join(base, "nested", "deep", "test.db")

	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open with missing parent dir: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	// Parent directory must now exist and the DB file must be created.
	if info, err := os.Stat(filepath.Dir(dbPath)); err != nil {
		t.Fatalf("parent dir not created: %v", err)
	} else if !info.IsDir() {
		t.Fatalf("parent path is not a directory")
	}
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("db file not created: %v", err)
	}
}


