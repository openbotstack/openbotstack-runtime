package persistence

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

// SeedDefaults creates a default tenant, admin user, and API key if no tenants exist.
// Returns the plaintext API key for one-time display, or empty string if already seeded.
func (db *DB) SeedDefaults() (string, error) {
	// 1. Check if tenants already exist
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM tenants").Scan(&count)
	if err != nil {
		return "", fmt.Errorf("check tenants: %w", err)
	}
	if count > 0 {
		return "", nil
	}

	// 2. Generate API key: obs_ + 32 hex chars = 36 total
	keyBytes := make([]byte, 16) // 16 bytes → 32 hex chars
	if _, err := rand.Read(keyBytes); err != nil {
		return "", fmt.Errorf("generate key: %w", err)
	}
	fullKey := "obs_" + hex.EncodeToString(keyBytes)
	hash := sha256.Sum256([]byte(fullKey))
	hashHex := hex.EncodeToString(hash[:])
	prefix := fullKey[:12] // e.g. "obs_a1b2c3d4"
	now := time.Now().UTC().Format(time.RFC3339Nano)

	// 3. Insert tenant, user, and key in a single transaction
	tx, err := db.Begin()
	if err != nil {
		return "", fmt.Errorf("seed begin tx: %w", err)
	}
	defer tx.Rollback()

	_, err = tx.Exec(`INSERT INTO tenants (id, name, created_at) VALUES (?, ?, ?)`,
		"default", "Default Tenant", now)
	if err != nil {
		return "", fmt.Errorf("insert default tenant: %w", err)
	}

	_, err = tx.Exec(`INSERT INTO users (id, tenant_id, name, role, created_at) VALUES (?, ?, ?, ?, ?)`,
		"admin", "default", "Admin", "admin", now)
	if err != nil {
		return "", fmt.Errorf("insert admin user: %w", err)
	}

	_, err = tx.Exec(`INSERT INTO api_keys (id, tenant_id, user_id, key_prefix, key_hash, name, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"default-key", "default", "admin", prefix, hashHex, "default", now)
	if err != nil {
		return "", fmt.Errorf("insert default key: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("seed commit: %w", err)
	}

	return fullKey, nil
}
