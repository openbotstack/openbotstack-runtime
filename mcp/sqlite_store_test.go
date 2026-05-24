package mcp

import (
	"context"
	"testing"

	mcpcore "github.com/openbotstack/openbotstack-core/mcp"
	"github.com/openbotstack/openbotstack-runtime/persistence"
)

func TestSQLiteMCPStore_CRUD(t *testing.T) {
	db, err := persistence.Open(":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	store := NewSQLiteMCPStore(db.DB)
	ctx := context.Background()

	// Create
	cfg := mcpcore.ServerConfig{
		ID:        "test-srv",
		Name:      "Test Server",
		Transport: "stdio",
		Command:   "node",
		Args:      []string{"server.js"},
		Env:       map[string]string{"API_KEY": "test"},
		Enabled:   true,
	}
	if err := store.Create(ctx, cfg); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Get
	got, err := store.Get(ctx, "test-srv")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "Test Server" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.Transport != "stdio" {
		t.Errorf("Transport = %q", got.Transport)
	}
	if got.Command != "node" {
		t.Errorf("Command = %q", got.Command)
	}
	if len(got.Args) != 1 || got.Args[0] != "server.js" {
		t.Errorf("Args = %v", got.Args)
	}
	if !got.Enabled {
		t.Error("expected Enabled = true")
	}

	// List
	configs, err := store.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(configs) != 1 {
		t.Fatalf("List count = %d, want 1", len(configs))
	}

	// Update
	cfg.Name = "Updated Server"
	cfg.Enabled = false
	if err := store.Update(ctx, cfg); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got2, _ := store.Get(ctx, "test-srv")
	if got2.Name != "Updated Server" {
		t.Errorf("updated Name = %q", got2.Name)
	}
	if got2.Enabled {
		t.Error("expected Enabled = false after update")
	}

	// Delete
	if err := store.Delete(ctx, "test-srv"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, err = store.Get(ctx, "test-srv")
	if err == nil {
		t.Error("expected error after delete")
	}
}

func TestSQLiteMCPStore_DeleteNonexistent(t *testing.T) {
	db, _ := persistence.Open(":memory:")
	db.Migrate()
	store := NewSQLiteMCPStore(db.DB)
	err := store.Delete(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error deleting nonexistent server")
	}
}
