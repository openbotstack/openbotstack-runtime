package integration

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/ai/providers"
	"github.com/openbotstack/openbotstack-core/ai/router"
	"github.com/openbotstack/openbotstack-runtime/api"
	"github.com/openbotstack/openbotstack-runtime/api/middleware"
	"github.com/openbotstack/openbotstack-runtime/internal/adapters"
	"github.com/openbotstack/openbotstack-runtime/persistence"
)

func TestProviderHotReload_PreservesURL(t *testing.T) {
	db, err := persistence.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	db.Exec("INSERT INTO tenants (id, name, created_at) VALUES (?, ?, ?)", "default", "Default", now)
	db.Exec("INSERT INTO users (id, tenant_id, name, role, created_at) VALUES (?, ?, ?, ?, ?)", "admin", "default", "Admin", "admin", now)

	keyBytes := make([]byte, 16)
	rand.Read(keyBytes)
	adminKey := "obs_" + hex.EncodeToString(keyBytes)
	hash := sha256.Sum256([]byte(adminKey))
	db.Exec("INSERT INTO api_keys (id, tenant_id, user_id, key_prefix, key_hash, name, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"k1", "default", "admin", adminKey[:12], hex.EncodeToString(hash[:]), "test", now)

	customURL := "https://custom-llm.example.com/v1"
	db.Exec(`INSERT INTO provider_config (provider_name, base_url, api_key, model, is_default, updated_at)
		VALUES (?, ?, ?, ?, 1, ?)`, "openai", customURL, "sk-test-key", "test-model", now)

	modelRouter := router.NewDefaultRouter()
	factory := providers.NewProviderFactory()
	adminRouter := api.NewAdminRouter(db.DB)
	adminRouter.SetProviderReloader(&adapters.ProviderReloader{Router: modelRouter, Factory: factory})

	apiKeyMW := middleware.APIKeyMiddleware(middleware.APIKeyMiddlewareConfig{DB: db.DB, Strict: false})
	mux := http.NewServeMux()
	mux.Handle("/", apiKeyMW(adminRouter.Handler()))

	// PUT without base_url — should preserve custom URL
	body, _ := json.Marshal(map[string]string{
		"provider":   "openai",
		"api_key":    "",
		"model":      "updated-model",
		"is_default": "true",
	})
	req := httptest.NewRequest("PUT", "/v1/admin/providers/config", bytes.NewReader(body))
	req.Header.Set("X-API-Key", adminKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", rec.Code, rec.Body.String())
	}

	var dbURL string
	db.QueryRow("SELECT base_url FROM provider_config WHERE provider_name = ?", "openai").Scan(&dbURL)
	if dbURL != customURL {
		t.Errorf("base_url = %q, want %q (preserved)", dbURL, customURL)
	}

	var dbModel string
	db.QueryRow("SELECT model FROM provider_config WHERE provider_name = ?", "openai").Scan(&dbModel)
	if dbModel != "updated-model" {
		t.Errorf("model = %q, want updated-model", dbModel)
	}
}

func TestProviderHotReload_ExplicitURLOverrides(t *testing.T) {
	db, err := persistence.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	db.Exec("INSERT INTO tenants (id, name, created_at) VALUES (?, ?, ?)", "default", "Default", now)
	db.Exec("INSERT INTO users (id, tenant_id, name, role, created_at) VALUES (?, ?, ?, ?, ?)", "admin", "default", "Admin", "admin", now)

	keyBytes := make([]byte, 16)
	rand.Read(keyBytes)
	adminKey := "obs_" + hex.EncodeToString(keyBytes)
	hash := sha256.Sum256([]byte(adminKey))
	db.Exec("INSERT INTO api_keys (id, tenant_id, user_id, key_prefix, key_hash, name, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"k1", "default", "admin", adminKey[:12], hex.EncodeToString(hash[:]), "test", now)

	db.Exec(`INSERT INTO provider_config (provider_name, base_url, api_key, model, is_default, updated_at)
		VALUES (?, ?, ?, ?, 1, ?)`, "openai", "https://old.example.com/v1", "sk-old", "old-model", now)

	modelRouter := router.NewDefaultRouter()
	factory := providers.NewProviderFactory()
	adminRouter := api.NewAdminRouter(db.DB)
	adminRouter.SetProviderReloader(&adapters.ProviderReloader{Router: modelRouter, Factory: factory})

	apiKeyMW := middleware.APIKeyMiddleware(middleware.APIKeyMiddlewareConfig{DB: db.DB, Strict: false})
	mux := http.NewServeMux()
	mux.Handle("/", apiKeyMW(adminRouter.Handler()))

	body, _ := json.Marshal(map[string]string{
		"provider":   "openai",
		"base_url":   "https://new-endpoint.example.com/v1",
		"api_key":    "sk-new-key",
		"model":      "new-model",
		"is_default": "true",
	})
	req := httptest.NewRequest("PUT", "/v1/admin/providers/config", bytes.NewReader(body))
	req.Header.Set("X-API-Key", adminKey)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("PUT status = %d", rec.Code)
	}

	var dbURL string
	db.QueryRow("SELECT base_url FROM provider_config WHERE provider_name = ?", "openai").Scan(&dbURL)
	if dbURL != "https://new-endpoint.example.com/v1" {
		t.Errorf("base_url = %q, want new URL (overridden)", dbURL)
	}
}
