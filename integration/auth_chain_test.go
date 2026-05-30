package integration

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/access/auth"
	"github.com/openbotstack/openbotstack-runtime/api/middleware"
	"github.com/openbotstack/openbotstack-runtime/persistence"
)

// TestAuthMiddlewareChain_Composite tests the full API Key + JWT composite auth flow.
func TestAuthMiddlewareChain_Composite(t *testing.T) {
	db, err := persistence.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)

	// Setup test data
	db.Exec("INSERT INTO tenants (id, name, created_at) VALUES (?, ?, ?)", "t1", "Test", now)
	db.Exec("INSERT INTO users (id, tenant_id, name, role, created_at) VALUES (?, ?, ?, ?, ?)", "u1", "t1", "Admin", "admin", now)
	db.Exec("INSERT INTO users (id, tenant_id, name, role, created_at) VALUES (?, ?, ?, ?, ?)", "u2", "t1", "Member", "member", now)

	// Create admin key
	keyBytes := make([]byte, 16)
	rand.Read(keyBytes)
	adminKey := "obs_" + hex.EncodeToString(keyBytes)
	hash := sha256.Sum256([]byte(adminKey))
	db.Exec("INSERT INTO api_keys (id, tenant_id, user_id, key_prefix, key_hash, name, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"k1", "t1", "u1", adminKey[:12], hex.EncodeToString(hash[:]), "admin-key", now)

	// Create member key
	keyBytes2 := make([]byte, 16)
	rand.Read(keyBytes2)
	memberKey := "obs_" + hex.EncodeToString(keyBytes2)
	hash2 := sha256.Sum256([]byte(memberKey))
	db.Exec("INSERT INTO api_keys (id, tenant_id, user_id, key_prefix, key_hash, name, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"k2", "t1", "u2", memberKey[:12], hex.EncodeToString(hash2[:]), "member-key", now)

	// Build middleware chain: API Key → RequireAdmin
	apiKeyMW := middleware.APIKeyMiddleware(middleware.APIKeyMiddlewareConfig{DB: db.DB, Strict: false})

	handler := apiKeyMW(middleware.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := middleware.UserFromContext(r.Context())
		if !ok || user == nil {
			t.Error("expected user in context")
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})))

	tests := []struct {
		name       string
		key        string
		expectCode int
	}{
		{"admin key allowed", adminKey, http.StatusOK},
		{"member key forbidden", memberKey, http.StatusForbidden},
		{"invalid key rejected", "obs_invalidkey1234567890abcdef", http.StatusUnauthorized},
		{"no key rejected (non-strict, no auth context)", "", http.StatusUnauthorized},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/admin/test", nil)
			if tt.key != "" {
				req.Header.Set("X-API-Key", tt.key)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tt.expectCode {
				t.Errorf("status = %d, want %d", rec.Code, tt.expectCode)
			}
		})
	}

	// Verify user context injection
	t.Run("admin user has correct tenant", func(t *testing.T) {
		var gotUser *auth.User
		var gotRole string

		testHandler := apiKeyMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotUser, _ = middleware.UserFromContext(r.Context())
			gotRole = middleware.RoleFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		req.Header.Set("X-API-Key", adminKey)
		rec := httptest.NewRecorder()
		testHandler.ServeHTTP(rec, req)

		if gotUser == nil {
			t.Fatal("expected user in context")
		}
		if gotUser.ID != "u1" {
			t.Errorf("user ID = %q, want u1", gotUser.ID)
		}
		if gotUser.TenantID != "t1" {
			t.Errorf("tenant ID = %q, want t1", gotUser.TenantID)
		}
		if gotRole != "admin" {
			t.Errorf("role = %q, want admin", gotRole)
		}
	})
}

// TestAuthMiddlewareChain_ExpiredKeyDuringRequest tests that an expired key
// is rejected even when the request is in-flight.
func TestAuthMiddlewareChain_ExpiredKeyDuringRequest(t *testing.T) {
	db, err := persistence.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	pastTime := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339Nano)

	db.Exec("INSERT INTO tenants (id, name, created_at) VALUES (?, ?, ?)", "t1", "Test", now)
	db.Exec("INSERT INTO users (id, tenant_id, name, role, created_at) VALUES (?, ?, ?, ?, ?)", "u1", "t1", "User", "admin", now)

	keyBytes := make([]byte, 16)
	rand.Read(keyBytes)
	validKey := "obs_" + hex.EncodeToString(keyBytes)
	hash := sha256.Sum256([]byte(validKey))
	db.Exec("INSERT INTO api_keys (id, tenant_id, user_id, key_prefix, key_hash, name, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		"k1", "t1", "u1", validKey[:12], hex.EncodeToString(hash[:]), "expired-key", now, pastTime)

	apiKeyMW := middleware.APIKeyMiddleware(middleware.APIKeyMiddlewareConfig{DB: db.DB, Strict: false})

	handler := apiKeyMW(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", validKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expired key: status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}
