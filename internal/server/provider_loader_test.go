package server

import (
	"path/filepath"
	"testing"

	"github.com/openbotstack/openbotstack-core/ai/providers"
	"github.com/openbotstack/openbotstack-core/ai/router"
	"github.com/openbotstack/openbotstack-runtime/internal/crypto"
	"github.com/openbotstack/openbotstack-runtime/persistence"
)

// newTestDB opens a fresh file-backed SQLite DB with migrations applied.
func newTestDB(t *testing.T) *persistence.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := persistence.Open(dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	return db
}

func insertProviderRow(t *testing.T, db *persistence.DB, id, provider, baseURL, apiKey, model string, isDefault int) {
	t.Helper()
	_, err := db.Exec(`INSERT INTO provider_config (id, provider, name, base_url, api_key, model, is_default, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, '')`, id, provider, provider, baseURL, apiKey, model, isDefault)
	if err != nil {
		t.Fatalf("insert provider row: %v", err)
	}
}

func TestLoadProvidersFromDB_Empty(t *testing.T) {
	db := newTestDB(t)
	factory := providers.NewProviderFactory()
	r := router.NewDefaultRouter()

	n, err := LoadProvidersFromDB(db, factory, r)
	if err != nil {
		t.Fatalf("LoadProvidersFromDB: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 providers from empty DB, got %d", n)
	}
	if len(r.List()) != 0 {
		t.Fatalf("expected empty router, got %v", r.List())
	}
}

func TestLoadProvidersFromDB_PlaintextKey(t *testing.T) {
	db := newTestDB(t)
	insertProviderRow(t, db, "p1", "openai", "https://api.openai.com/v1", "sk-test-key", "gpt-4o", 1)

	factory := providers.NewProviderFactory()
	r := router.NewDefaultRouter()

	n, err := LoadProvidersFromDB(db, factory, r)
	if err != nil {
		t.Fatalf("LoadProvidersFromDB: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 provider registered, got %d", n)
	}
	if len(r.List()) != 1 {
		t.Fatalf("expected 1 provider in router, got %d (%v)", len(r.List()), r.List())
	}
}

func TestLoadProvidersFromDB_SkipsEmptyKey(t *testing.T) {
	db := newTestDB(t)
	// Row with empty API key must be skipped, not registered.
	insertProviderRow(t, db, "p1", "openai", "https://api.openai.com/v1", "", "gpt-4o", 1)

	factory := providers.NewProviderFactory()
	r := router.NewDefaultRouter()

	n, err := LoadProvidersFromDB(db, factory, r)
	if err != nil {
		t.Fatalf("LoadProvidersFromDB: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 providers (empty key skipped), got %d", n)
	}
}

func TestLoadProvidersFromDB_DecryptsEncryptedKey(t *testing.T) {
	// Enable encryption for this test.
	t.Setenv("OBS_DB_ENCRYPTION_KEY", "test-encryption-key-32-chars-min!!")
	encKey := crypto.EncryptionKey()
	if encKey == nil {
		t.Fatal("encryption key not derived from env")
	}

	// Store an encrypted key (as the Admin API would).
	enc, err := crypto.Encrypt(encKey, "sk-secret")
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if !crypto.IsEncrypted(enc) {
		t.Fatalf("expected encrypted value, got %q", enc)
	}

	db := newTestDB(t)
	insertProviderRow(t, db, "p1", "openai", "https://api.openai.com/v1", enc, "gpt-4o", 1)

	factory := providers.NewProviderFactory()
	r := router.NewDefaultRouter()

	n, err := LoadProvidersFromDB(db, factory, r)
	if err != nil {
		t.Fatalf("LoadProvidersFromDB: %v", err)
	}
	if n != 1 {
		t.Fatalf("expected 1 provider decrypted+registered, got %d", n)
	}
}

func TestLoadProvidersFromDB_MultipleProviders(t *testing.T) {
	db := newTestDB(t)
	insertProviderRow(t, db, "p1", "openai", "https://api.openai.com/v1", "sk-1", "gpt-4o", 1)
	insertProviderRow(t, db, "p2", "claude", "https://api.anthropic.com", "sk-2", "claude-3", 0)

	factory := providers.NewProviderFactory()
	r := router.NewDefaultRouter()

	n, err := LoadProvidersFromDB(db, factory, r)
	if err != nil {
		t.Fatalf("LoadProvidersFromDB: %v", err)
	}
	if n != 2 {
		t.Fatalf("expected 2 providers registered, got %d", n)
	}
	if len(r.List()) != 2 {
		t.Fatalf("expected 2 providers in router, got %d (%v)", len(r.List()), r.List())
	}
}
