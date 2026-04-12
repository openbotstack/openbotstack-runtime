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
	defer db.Close()

	if err := db.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestMigrateCreatesTables(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	tables := []string{"audit_logs", "quotas", "rate_limits", "session_entries"}
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
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("first Migrate: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("second Migrate: %v", err)
	}
}
