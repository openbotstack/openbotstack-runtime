package persistence

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

func TestSeedDefaults(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// First call: should create default tenant/user/key
	key, err := db.SeedDefaults()
	if err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}

	// Verify key format: "obs_" (4) + 32 hex chars = 36 chars total
	if len(key) != 36 {
		t.Errorf("key length = %d, want 36", len(key))
	}
	if key[:4] != "obs_" {
		t.Errorf("key prefix = %q, want obs_", key[:4])
	}

	// Verify tenant exists
	var tenantName string
	err = db.QueryRow("SELECT name FROM tenants WHERE id = ?", "default").Scan(&tenantName)
	if err != nil {
		t.Fatalf("select default tenant: %v", err)
	}
	if tenantName != "Default Tenant" {
		t.Errorf("tenant name = %q, want 'Default Tenant'", tenantName)
	}

	// Verify admin user exists with role 'admin'
	var userName, userRole, userTenantID string
	err = db.QueryRow("SELECT name, role, tenant_id FROM users WHERE id = ?", "admin").Scan(&userName, &userRole, &userTenantID)
	if err != nil {
		t.Fatalf("select admin user: %v", err)
	}
	if userName != "Admin" {
		t.Errorf("user name = %q, want 'Admin'", userName)
	}
	if userRole != "admin" {
		t.Errorf("user role = %q, want 'admin'", userRole)
	}
	if userTenantID != "default" {
		t.Errorf("user tenant_id = %q, want 'default'", userTenantID)
	}

	// Verify API key hash matches
	var keyHash string
	err = db.QueryRow("SELECT key_hash FROM api_keys WHERE id = ?", "default-key").Scan(&keyHash)
	if err != nil {
		t.Fatalf("select default key: %v", err)
	}
	expectedHash := sha256.Sum256([]byte(key))
	expectedHex := hex.EncodeToString(expectedHash[:])
	if keyHash != expectedHex {
		t.Errorf("key hash mismatch: got %q, want %q", keyHash, expectedHex)
	}

	// Verify key_prefix stored correctly
	var keyPrefix string
	err = db.QueryRow("SELECT key_prefix FROM api_keys WHERE id = ?", "default-key").Scan(&keyPrefix)
	if err != nil {
		t.Fatalf("select key prefix: %v", err)
	}
	if keyPrefix != key[:12] {
		t.Errorf("key prefix = %q, want %q", keyPrefix, key[:12])
	}

	// Second call: should return "" (already seeded)
	key2, err := db.SeedDefaults()
	if err != nil {
		t.Fatalf("SeedDefaults second call: %v", err)
	}
	if key2 != "" {
		t.Errorf("second call returned key %q, want empty string", key2)
	}
}

func TestSeedDefaultsIdempotent(t *testing.T) {
	db, err := Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	key1, err := db.SeedDefaults()
	if err != nil {
		t.Fatalf("first SeedDefaults: %v", err)
	}
	if key1 == "" {
		t.Fatal("first call returned empty key")
	}

	key2, err := db.SeedDefaults()
	if err != nil {
		t.Fatalf("second SeedDefaults: %v", err)
	}
	if key2 != "" {
		t.Errorf("second call returned %q, want empty (already seeded)", key2)
	}
}
