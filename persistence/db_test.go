package persistence

import (
	"database/sql"
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

func TestMigrateTenantColumn(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// MigrateTenantColumn should be a no-op since Migrate already includes tenant_id.
	if err := db.MigrateTenantColumn(); err != nil {
		t.Fatalf("MigrateTenantColumn: %v", err)
	}

	// Verify tenant_id column exists and is TEXT.
	var cid int
	var name, colType string
	err = db.QueryRow(
		"SELECT cid, name, type FROM pragma_table_info('session_entries') WHERE name = 'tenant_id'",
	).Scan(&cid, &name, &colType)
	if err != nil {
		t.Fatalf("tenant_id column not found: %v", err)
	}
	if colType != "TEXT" {
		t.Errorf("tenant_id type = %q, want TEXT", colType)
	}
}

func TestMigrateTenantColumnBeforeMigrate(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()

	// Calling MigrateTenantColumn before Migrate should return an error
	// because the session_entries table does not exist yet.
	err = db.MigrateTenantColumn()
	if err == nil {
		t.Fatal("expected error when calling MigrateTenantColumn before Migrate, got nil")
	}
}
