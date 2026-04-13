package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openbotstack/openbotstack-core/access/auth"
	"github.com/openbotstack/openbotstack-runtime/api/middleware"
	"github.com/openbotstack/openbotstack-runtime/persistence"
)

// setupAdminTest creates a full admin test environment with an in-memory DB,
// seeded defaults, and a handler wrapped with admin role injection.
func setupAdminTest(t *testing.T) (*persistence.DB, http.Handler) {
	t.Helper()
	db, err := persistence.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	// Seed default tenant + admin user
	if _, err := db.SeedDefaults(); err != nil {
		t.Fatalf("SeedDefaults: %v", err)
	}

	adminRouter := NewAdminRouter(db.DB)

	// Wrap with middleware that injects admin user + role
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := &auth.User{ID: "admin", TenantID: "default", Name: "Admin"}
		ctx := middleware.WithUser(r.Context(), user)
		ctx = middleware.WithUserRole(ctx, "admin")
		adminRouter.Handler().ServeHTTP(w, r.WithContext(ctx))
	})

	return db, handler
}

// doAdminRequest is a helper that sends a request to the admin handler.
func doAdminRequest(t *testing.T, handler http.Handler, method, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	var req *http.Request
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		req = httptest.NewRequest(method, path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestCreateTenant(t *testing.T) {
	_, handler := setupAdminTest(t)

	rec := doAdminRequest(t, handler, "POST", "/v1/admin/tenants", map[string]string{
		"id": "acme", "name": "Acme Corp",
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["id"] != "acme" {
		t.Errorf("id = %q, want %q", resp["id"], "acme")
	}
	if resp["name"] != "Acme Corp" {
		t.Errorf("name = %q, want %q", resp["name"], "Acme Corp")
	}
	if resp["created_at"] == "" {
		t.Error("created_at is empty")
	}
}

func TestListTenants(t *testing.T) {
	_, handler := setupAdminTest(t)

	rec := doAdminRequest(t, handler, "GET", "/v1/admin/tenants", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp []map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp) < 1 {
		t.Fatalf("expected at least 1 tenant (default), got %d", len(resp))
	}
	// Check that the default tenant is present
	found := false
	for _, t2 := range resp {
		if t2["id"] == "default" {
			found = true
			break
		}
	}
	if !found {
		t.Error("default tenant not found in list")
	}
}

func TestCreateUser(t *testing.T) {
	_, handler := setupAdminTest(t)

	// Create a new tenant first
	doAdminRequest(t, handler, "POST", "/v1/admin/tenants", map[string]string{
		"id": "acme", "name": "Acme Corp",
	})

	rec := doAdminRequest(t, handler, "POST", "/v1/admin/tenants/acme/users", map[string]string{
		"id": "u1", "name": "Alice", "role": "member",
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["id"] != "u1" {
		t.Errorf("id = %q, want %q", resp["id"], "u1")
	}
	if resp["tenant_id"] != "acme" {
		t.Errorf("tenant_id = %q, want %q", resp["tenant_id"], "acme")
	}
	if resp["name"] != "Alice" {
		t.Errorf("name = %q, want %q", resp["name"], "Alice")
	}
	if resp["role"] != "member" {
		t.Errorf("role = %q, want %q", resp["role"], "member")
	}
}

func TestListUsers(t *testing.T) {
	_, handler := setupAdminTest(t)

	// Create tenant + user
	doAdminRequest(t, handler, "POST", "/v1/admin/tenants", map[string]string{
		"id": "acme", "name": "Acme Corp",
	})
	doAdminRequest(t, handler, "POST", "/v1/admin/tenants/acme/users", map[string]string{
		"id": "u1", "name": "Alice", "role": "member",
	})

	rec := doAdminRequest(t, handler, "GET", "/v1/admin/tenants/acme/users", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp []map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("expected 1 user, got %d", len(resp))
	}
	if resp[0]["id"] != "u1" {
		t.Errorf("id = %q, want %q", resp[0]["id"], "u1")
	}
	if resp[0]["name"] != "Alice" {
		t.Errorf("name = %q, want %q", resp[0]["name"], "Alice")
	}
}

func TestCreateAPIKey(t *testing.T) {
	_, handler := setupAdminTest(t)

	// Create tenant + user
	doAdminRequest(t, handler, "POST", "/v1/admin/tenants", map[string]string{
		"id": "acme", "name": "Acme Corp",
	})
	doAdminRequest(t, handler, "POST", "/v1/admin/tenants/acme/users", map[string]string{
		"id": "u1", "name": "Alice", "role": "member",
	})

	rec := doAdminRequest(t, handler, "POST", "/v1/admin/users/u1/keys", map[string]string{
		"name": "test-key",
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	// Verify the full key is returned and has the correct prefix
	if !strings.HasPrefix(resp["key"], "obs_") {
		t.Errorf("key = %q, want obs_ prefix", resp["key"])
	}
	if len(resp["key"]) != 36 { // obs_ (4) + 32 hex chars
		t.Errorf("key length = %d, want 36", len(resp["key"]))
	}
	if resp["key_prefix"] != resp["key"][:12] {
		t.Errorf("key_prefix = %q, want first 12 chars of key %q", resp["key_prefix"], resp["key"])
	}
	if resp["name"] != "test-key" {
		t.Errorf("name = %q, want %q", resp["name"], "test-key")
	}
}

func TestListAPIKeys(t *testing.T) {
	_, handler := setupAdminTest(t)

	// Create tenant + user + key
	doAdminRequest(t, handler, "POST", "/v1/admin/tenants", map[string]string{
		"id": "acme", "name": "Acme Corp",
	})
	doAdminRequest(t, handler, "POST", "/v1/admin/tenants/acme/users", map[string]string{
		"id": "u1", "name": "Alice", "role": "member",
	})

	// Create a key first
	createResp := doAdminRequest(t, handler, "POST", "/v1/admin/users/u1/keys", map[string]string{
		"name": "test-key",
	})
	var createBody map[string]string
	json.NewDecoder(createResp.Body).Decode(&createBody)
	fullKey := createBody["key"]

	// Now list keys
	rec := doAdminRequest(t, handler, "GET", "/v1/admin/users/u1/keys", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp []map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp) != 1 {
		t.Fatalf("expected 1 key, got %d", len(resp))
	}

	// Verify the response has prefix only (NOT the full key)
	key := resp[0]
	prefix, _ := key["key_prefix"].(string)
	if prefix != fullKey[:12] {
		t.Errorf("key_prefix = %q, want %q", prefix, fullKey[:12])
	}
	// Ensure no "key" field is present (full key should NOT be in list response)
	if _, hasKey := key["key"]; hasKey {
		t.Error("list response should NOT contain the full key")
	}
	if revoked, _ := key["revoked"].(bool); revoked {
		t.Error("newly created key should not be revoked")
	}
}

func TestRevokeKey(t *testing.T) {
	_, handler := setupAdminTest(t)

	// Create tenant + user + key
	doAdminRequest(t, handler, "POST", "/v1/admin/tenants", map[string]string{
		"id": "acme", "name": "Acme Corp",
	})
	doAdminRequest(t, handler, "POST", "/v1/admin/tenants/acme/users", map[string]string{
		"id": "u1", "name": "Alice", "role": "member",
	})
	createResp := doAdminRequest(t, handler, "POST", "/v1/admin/users/u1/keys", map[string]string{
		"name": "test-key",
	})
	var createBody map[string]string
	json.NewDecoder(createResp.Body).Decode(&createBody)
	keyID := createBody["id"]

	// Revoke the key
	rec := doAdminRequest(t, handler, "DELETE", "/v1/admin/keys/"+keyID, nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["id"] != keyID {
		t.Errorf("id = %v, want %q", resp["id"], keyID)
	}
	if revoked, _ := resp["revoked"].(bool); !revoked {
		t.Error("revoked should be true")
	}
}

func TestRevokedKeyCannotAuth(t *testing.T) {
	db, handler := setupAdminTest(t)

	// Create tenant + user + key
	doAdminRequest(t, handler, "POST", "/v1/admin/tenants", map[string]string{
		"id": "acme", "name": "Acme Corp",
	})
	doAdminRequest(t, handler, "POST", "/v1/admin/tenants/acme/users", map[string]string{
		"id": "u1", "name": "Alice", "role": "member",
	})
	createResp := doAdminRequest(t, handler, "POST", "/v1/admin/users/u1/keys", map[string]string{
		"name": "test-key",
	})
	var createBody map[string]string
	json.NewDecoder(createResp.Body).Decode(&createBody)
	fullKey := createBody["key"]
	keyID := createBody["id"]

	// Revoke the key
	doAdminRequest(t, handler, "DELETE", "/v1/admin/keys/"+keyID, nil)

	// Now try to authenticate with the revoked key using APIKeyMiddleware
	called := false
	mw := middleware.APIKeyMiddleware(middleware.APIKeyMiddlewareConfig{DB: db.DB, Strict: true})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", fullKey)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want %d for revoked key", rec.Code, http.StatusUnauthorized)
	}
	if called {
		t.Error("next handler should not have been called for revoked key")
	}
}

func TestCreateTenantMissingFields(t *testing.T) {
	_, handler := setupAdminTest(t)

	tests := []struct {
		name string
		body map[string]string
	}{
		{"missing id", map[string]string{"name": "Test"}},
		{"missing name", map[string]string{"id": "test"}},
		{"empty both", map[string]string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doAdminRequest(t, handler, "POST", "/v1/admin/tenants", tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", rec.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestCreateUserForMissingTenant(t *testing.T) {
	_, handler := setupAdminTest(t)

	// FK enforcement rejects creating a user under a nonexistent tenant.
	rec := doAdminRequest(t, handler, "POST", "/v1/admin/tenants/nonexistent/users", map[string]string{
		"id": "u1", "name": "Alice", "role": "member",
	})

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}
}

func TestCreateAPIKeyForMissingUser(t *testing.T) {
	_, handler := setupAdminTest(t)

	rec := doAdminRequest(t, handler, "POST", "/v1/admin/users/nonexistent/keys", map[string]string{
		"name": "test-key",
	})

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestRevokeNonexistentKey(t *testing.T) {
	_, handler := setupAdminTest(t)

	rec := doAdminRequest(t, handler, "DELETE", "/v1/admin/keys/nonexistent", nil)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusNotFound, rec.Body.String())
	}
}

func TestCreateUserDefaultRole(t *testing.T) {
	_, handler := setupAdminTest(t)

	// Create a tenant
	doAdminRequest(t, handler, "POST", "/v1/admin/tenants", map[string]string{
		"id": "acme", "name": "Acme Corp",
	})

	// Create user without specifying role
	rec := doAdminRequest(t, handler, "POST", "/v1/admin/tenants/acme/users", map[string]string{
		"id": "u1", "name": "Alice",
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusCreated, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["role"] != "member" {
		t.Errorf("role = %q, want %q (default)", resp["role"], "member")
	}
}

func TestListUsersEmpty(t *testing.T) {
	_, handler := setupAdminTest(t)

	// Create a tenant but no users
	doAdminRequest(t, handler, "POST", "/v1/admin/tenants", map[string]string{
		"id": "empty", "name": "Empty Corp",
	})

	rec := doAdminRequest(t, handler, "GET", "/v1/admin/tenants/empty/users", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}

	var resp []map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("expected 0 users, got %d", len(resp))
	}
}

func TestCreatedAPIKeyCanAuthenticate(t *testing.T) {
	db, handler := setupAdminTest(t)

	// Create tenant + user + key
	doAdminRequest(t, handler, "POST", "/v1/admin/tenants", map[string]string{
		"id": "acme", "name": "Acme Corp",
	})
	doAdminRequest(t, handler, "POST", "/v1/admin/tenants/acme/users", map[string]string{
		"id": "u1", "name": "Alice", "role": "admin",
	})
	createResp := doAdminRequest(t, handler, "POST", "/v1/admin/users/u1/keys", map[string]string{
		"name": "test-key",
	})
	var createBody map[string]string
	json.NewDecoder(createResp.Body).Decode(&createBody)
	fullKey := createBody["key"]

	// Verify the key works with APIKeyMiddleware
	var gotUser *auth.User
	var gotRole string
	mw := middleware.APIKeyMiddleware(middleware.APIKeyMiddlewareConfig{DB: db.DB, Strict: true})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotUser, _ = middleware.UserFromContext(r.Context())
			gotRole = middleware.RoleFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		}),
	)

	req := httptest.NewRequest("GET", "/test", nil)
	req.Header.Set("X-API-Key", fullKey)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if gotUser == nil {
		t.Fatal("expected user in context")
	}
	if gotUser.ID != "u1" {
		t.Errorf("user ID = %q, want %q", gotUser.ID, "u1")
	}
	if gotUser.TenantID != "acme" {
		t.Errorf("user TenantID = %q, want %q", gotUser.TenantID, "acme")
	}
	if gotRole != "admin" {
		t.Errorf("role = %q, want %q", gotRole, "admin")
	}

	// Also verify the hash is stored correctly
	hash := sha256.Sum256([]byte(fullKey))
	hashHex := hex.EncodeToString(hash[:])
	var stored string
	err := db.QueryRow("SELECT key_hash FROM api_keys WHERE key_hash = ?", hashHex).Scan(&stored)
	if err != nil {
		t.Fatalf("query stored hash: %v", err)
	}
}

func TestAdminEndpointRejectsNonAdmin(t *testing.T) {
	db, err := persistence.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(); err != nil {
		t.Fatalf("Migrate: %v", err)
	}

	adminRouter := NewAdminRouter(db.DB)
	// Do NOT inject admin role — use bare handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		adminRouter.Handler().ServeHTTP(w, r)
	})

	rec := doAdminRequest(t, handler, "GET", "/v1/admin/tenants", nil)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want %d for non-admin", rec.Code, http.StatusForbidden)
	}
}
