package middleware

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openbotstack/openbotstack-core/access/auth"
	"github.com/openbotstack/openbotstack-runtime/persistence"
)

// setupTestDB creates an in-memory DB with schema + test data.
func setupTestDB(t *testing.T) (*persistence.DB, string) {
	t.Helper()
	db, err := persistence.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Generate a test key
	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		t.Fatalf("rand.Read: %v", err)
	}
	fullKey := "obs_" + hex.EncodeToString(keyBytes)
	hash := sha256.Sum256([]byte(fullKey))
	hashHex := hex.EncodeToString(hash[:])
	prefix := fullKey[:12]
	now := time.Now().UTC().Format(time.RFC3339Nano)

	// Insert test data
	if _, err := db.Exec("INSERT INTO tenants (id, name, created_at) VALUES (?, ?, ?)", "t1", "Test", now); err != nil {
		t.Fatalf("insert tenant: %v", err)
	}
	if _, err := db.Exec("INSERT INTO users (id, tenant_id, name, role, created_at) VALUES (?, ?, ?, ?, ?)", "u1", "t1", "TestUser", "admin", now); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := db.Exec("INSERT INTO api_keys (id, tenant_id, user_id, key_prefix, key_hash, name, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"k1", "t1", "u1", prefix, hashHex, "test", now); err != nil {
		t.Fatalf("insert api key: %v", err)
	}

	return db, fullKey
}

func TestAPIKeyMiddleware(t *testing.T) {
	// Setup DB and get valid key
	db, validKey := setupTestDB(t)

	tests := []struct {
		name       string
		strict     bool
		apiKey     string // X-API-Key header value, empty = no header
		setHeader  bool   // Whether to set the X-API-Key header at all
		expectUser bool   // Should user be in context?
		expectCode int    // Expected response code (0 = passes through to next handler)
	}{
		{"valid key non-strict", false, "", true, true, 0},
		{"valid key strict", true, "", true, true, 0},
		{"invalid key non-strict", false, "obs_invalid", true, false, 0},
		{"invalid key strict", true, "obs_invalid", true, false, http.StatusUnauthorized},
		{"no header non-strict", false, "", false, false, 0},
		{"no header strict", true, "", false, false, http.StatusUnauthorized},
	}

	// Fill in dynamic test cases
	tests[0].apiKey = validKey
	tests[1].apiKey = validKey

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false
			var gotUser *auth.User
			var gotRole string

			handler := APIKeyMiddleware(APIKeyMiddlewareConfig{DB: db.DB, Strict: tt.strict})(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true
					gotUser, _ = UserFromContext(r.Context())
					gotRole = RoleFromContext(r.Context())
					w.WriteHeader(http.StatusOK)
				}),
			)

			req := httptest.NewRequest("GET", "/test", nil)
			if tt.setHeader {
				req.Header.Set("X-API-Key", tt.apiKey)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if tt.expectCode != 0 {
				if rec.Code != tt.expectCode {
					t.Errorf("status = %d, want %d", rec.Code, tt.expectCode)
				}
				if called {
					t.Error("next handler should not have been called")
				}
				return
			}

			if !called {
				t.Fatal("next handler was not called")
			}
			if tt.expectUser {
				if gotUser == nil {
					t.Fatal("expected user in context, got nil")
				}
				if gotUser.ID != "u1" {
					t.Errorf("user ID = %q, want u1", gotUser.ID)
				}
				if gotUser.TenantID != "t1" {
					t.Errorf("user TenantID = %q, want t1", gotUser.TenantID)
				}
				if gotRole != "admin" {
					t.Errorf("role = %q, want admin", gotRole)
				}
			} else {
				if gotUser != nil {
					t.Errorf("expected no user, got %+v", gotUser)
				}
			}
		})
	}
}

func TestAPIKeyMiddleware_RevokedKey(t *testing.T) {
	db, validKey := setupTestDB(t)

	// Revoke the key
	_, err := db.Exec("UPDATE api_keys SET revoked = 1 WHERE id = ?", "k1")
	if err != nil {
		t.Fatalf("revoke key: %v", err)
	}

	called := false
	handler := APIKeyMiddleware(APIKeyMiddlewareConfig{DB: db.DB, Strict: true})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", validKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if called {
		t.Error("next handler should not have been called for revoked key")
	}
}

func TestAPIKeyMiddleware_ExpiredKey(t *testing.T) {
	db, validKey := setupTestDB(t)

	// Set expiry in the past
	pastTime := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339Nano)
	_, err := db.Exec("UPDATE api_keys SET expires_at = ? WHERE id = ?", pastTime, "k1")
	if err != nil {
		t.Fatalf("set expiry: %v", err)
	}

	called := false
	handler := APIKeyMiddleware(APIKeyMiddlewareConfig{DB: db.DB, Strict: true})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", validKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if called {
		t.Error("next handler should not have been called for expired key")
	}
}

func TestAPIKeyMiddleware_ExpiredKeyNonStrict(t *testing.T) {
	db, validKey := setupTestDB(t)

	// Set expiry in the past
	pastTime := time.Now().UTC().Add(-1 * time.Hour).Format(time.RFC3339Nano)
	_, err := db.Exec("UPDATE api_keys SET expires_at = ? WHERE id = ?", pastTime, "k1")
	if err != nil {
		t.Fatalf("set expiry: %v", err)
	}

	called := false
	var gotUser *auth.User
	handler := APIKeyMiddleware(APIKeyMiddlewareConfig{DB: db.DB, Strict: false})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			gotUser, _ = UserFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", validKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Fatal("next handler should have been called in non-strict mode for expired key")
	}
	if gotUser != nil {
		t.Error("expected no user for expired key in non-strict mode")
	}
}

func TestAPIKeyMiddleware_FutureExpiryAllowed(t *testing.T) {
	db, validKey := setupTestDB(t)

	// Set expiry in the future
	futureTime := time.Now().UTC().Add(24 * time.Hour).Format(time.RFC3339Nano)
	_, err := db.Exec("UPDATE api_keys SET expires_at = ? WHERE id = ?", futureTime, "k1")
	if err != nil {
		t.Fatalf("set expiry: %v", err)
	}

	var gotUser *auth.User
	handler := APIKeyMiddleware(APIKeyMiddlewareConfig{DB: db.DB, Strict: true})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotUser, _ = UserFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", validKey)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotUser == nil {
		t.Fatal("expected user for key with future expiry")
	}
	if gotUser.ID != "u1" {
		t.Errorf("user ID = %q, want u1", gotUser.ID)
	}
}

func TestRequireAdmin(t *testing.T) {
	tests := []struct {
		name       string
		role       string // role to inject, empty = no role
		expectCode int
	}{
		{"admin allowed", "admin", 0},
		{"member rejected", "member", http.StatusForbidden},
		{"no role rejected", "", http.StatusForbidden},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			called := false

			handler := RequireAdmin(
				http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					called = true
					w.WriteHeader(http.StatusOK)
				}),
			)

			req := httptest.NewRequest("GET", "/test", nil)
			if tt.role != "" {
				ctx := WithUserRole(req.Context(), tt.role)
				req = req.WithContext(ctx)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if tt.expectCode != 0 {
				if rec.Code != tt.expectCode {
					t.Errorf("status = %d, want %d", rec.Code, tt.expectCode)
				}
				if called {
					t.Error("next handler should not have been called")
				}
				return
			}

			if !called {
				t.Fatal("next handler was not called")
			}
		})
	}
}
