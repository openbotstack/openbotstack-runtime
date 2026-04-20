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
	t.Cleanup(func() { _ = db.Close() })
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
	if err := json.NewDecoder(createResp.Body).Decode(&createBody); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
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
	if err := json.NewDecoder(createResp.Body).Decode(&createBody); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	keyID := createBody["id"]

	// Revoke the key
	rec := doAdminRequest(t, handler, "DELETE", "/v1/admin/keys/"+keyID, nil)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}

	// Verify the key is revoked by listing keys
	listRec := doAdminRequest(t, handler, "GET", "/v1/admin/users/u1/keys", nil)
	var keys []map[string]interface{}
	if err := json.NewDecoder(listRec.Body).Decode(&keys); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	for _, k := range keys {
		if k["id"] == keyID {
			if revoked, _ := k["revoked"].(bool); !revoked {
				t.Error("key should be revoked after DELETE")
			}
		}
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
	if err := json.NewDecoder(createResp.Body).Decode(&createBody); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
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
	if err := json.NewDecoder(createResp.Body).Decode(&createBody); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
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

func TestHandleMe(t *testing.T) {
	// Test with authenticated admin user
	t.Run("authenticated admin", func(t *testing.T) {
		_, _ = setupAdminTest(t) // need DB for migration

		user := &auth.User{ID: "admin", TenantID: "default", Name: "Admin"}
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := middleware.WithUser(r.Context(), user)
			ctx = middleware.WithUserRole(ctx, "admin")
			HandleMe(w, r.WithContext(ctx))
		})

		rec := doAdminRequest(t, handler, "GET", "/v1/me", nil)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
		}

		var resp map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp["user_id"] != "admin" {
			t.Errorf("user_id = %q, want %q", resp["user_id"], "admin")
		}
		if resp["tenant_id"] != "default" {
			t.Errorf("tenant_id = %q, want %q", resp["tenant_id"], "default")
		}
		if resp["name"] != "Admin" {
			t.Errorf("name = %q, want %q", resp["name"], "Admin")
		}
		if resp["role"] != "admin" {
			t.Errorf("role = %q, want %q", resp["role"], "admin")
		}
	})

	t.Run("authenticated member", func(t *testing.T) {
		_, _ = setupAdminTest(t)

		user := &auth.User{ID: "alice", TenantID: "default", Name: "Alice"}
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := middleware.WithUser(r.Context(), user)
			ctx = middleware.WithUserRole(ctx, "member")
			HandleMe(w, r.WithContext(ctx))
		})

		rec := doAdminRequest(t, handler, "GET", "/v1/me", nil)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
		}

		var resp map[string]string
		if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if resp["role"] != "member" {
			t.Errorf("role = %q, want %q", resp["role"], "member")
		}
	})

	t.Run("unauthenticated", func(t *testing.T) {
		handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			HandleMe(w, r)
		})

		rec := doAdminRequest(t, handler, "GET", "/v1/me", nil)

		if rec.Code != http.StatusUnauthorized {
			t.Errorf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
		}
	})
}

func TestUpdateTenant(t *testing.T) {
	_, handler := setupAdminTest(t)

	// Create a tenant
	doAdminRequest(t, handler, "POST", "/v1/admin/tenants", map[string]string{
		"id": "acme", "name": "Acme Corp",
	})

	// Update the tenant name
	rec := doAdminRequest(t, handler, "PUT", "/v1/admin/tenants/acme", map[string]string{
		"name": "Acme Inc",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["name"] != "Acme Inc" {
		t.Errorf("name = %q, want %q", resp["name"], "Acme Inc")
	}

	// Verify via list
	listRec := doAdminRequest(t, handler, "GET", "/v1/admin/tenants", nil)
	var tenants []map[string]string
	json.NewDecoder(listRec.Body).Decode(&tenants)
	for _, t2 := range tenants {
		if t2["id"] == "acme" && t2["name"] != "Acme Inc" {
			t.Errorf("tenant name not updated in list: %q", t2["name"])
		}
	}
}

func TestUpdateTenantNotFound(t *testing.T) {
	_, handler := setupAdminTest(t)

	rec := doAdminRequest(t, handler, "PUT", "/v1/admin/tenants/nonexistent", map[string]string{
		"name": "New Name",
	})

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestDeleteTenant(t *testing.T) {
	_, handler := setupAdminTest(t)

	// Create a tenant
	doAdminRequest(t, handler, "POST", "/v1/admin/tenants", map[string]string{
		"id": "to-delete", "name": "Delete Me",
	})

	// Delete it
	rec := doAdminRequest(t, handler, "DELETE", "/v1/admin/tenants/to-delete", nil)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}

	// Verify via list
	listRec := doAdminRequest(t, handler, "GET", "/v1/admin/tenants", nil)
	var tenants []map[string]string
	json.NewDecoder(listRec.Body).Decode(&tenants)
	for _, t2 := range tenants {
		if t2["id"] == "to-delete" {
			t.Error("tenant should have been deleted")
		}
	}
}

func TestDeleteTenantNotFound(t *testing.T) {
	_, handler := setupAdminTest(t)

	rec := doAdminRequest(t, handler, "DELETE", "/v1/admin/tenants/nonexistent", nil)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestUpdateUser(t *testing.T) {
	_, handler := setupAdminTest(t)

	// Create tenant + user
	doAdminRequest(t, handler, "POST", "/v1/admin/tenants", map[string]string{
		"id": "acme", "name": "Acme Corp",
	})
	doAdminRequest(t, handler, "POST", "/v1/admin/tenants/acme/users", map[string]string{
		"id": "u1", "name": "Alice", "role": "member",
	})

	// Update user name and role
	rec := doAdminRequest(t, handler, "PUT", "/v1/admin/users/u1", map[string]string{
		"name": "Alice Updated",
		"role": "admin",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp["name"] != "Alice Updated" {
		t.Errorf("name = %q, want %q", resp["name"], "Alice Updated")
	}
	if resp["role"] != "admin" {
		t.Errorf("role = %q, want %q", resp["role"], "admin")
	}
}

func TestUpdateUserNotFound(t *testing.T) {
	_, handler := setupAdminTest(t)

	rec := doAdminRequest(t, handler, "PUT", "/v1/admin/users/nonexistent", map[string]string{
		"name": "Ghost",
	})

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestDeleteUser(t *testing.T) {
	_, handler := setupAdminTest(t)

	// Create tenant + user
	doAdminRequest(t, handler, "POST", "/v1/admin/tenants", map[string]string{
		"id": "acme", "name": "Acme Corp",
	})
	doAdminRequest(t, handler, "POST", "/v1/admin/tenants/acme/users", map[string]string{
		"id": "u-del", "name": "Delete Me", "role": "member",
	})

	// Delete user
	rec := doAdminRequest(t, handler, "DELETE", "/v1/admin/users/u-del", nil)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusNoContent, rec.Body.String())
	}

	// Verify user is gone from list
	listRec := doAdminRequest(t, handler, "GET", "/v1/admin/tenants/acme/users", nil)
	var users []map[string]string
	json.NewDecoder(listRec.Body).Decode(&users)
	for _, u := range users {
		if u["id"] == "u-del" {
			t.Error("user should have been deleted")
		}
	}
}

func TestDeleteUserNotFound(t *testing.T) {
	_, handler := setupAdminTest(t)

	rec := doAdminRequest(t, handler, "DELETE", "/v1/admin/users/nonexistent", nil)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestDeleteUserCascadesKeys(t *testing.T) {
	_, handler := setupAdminTest(t)

	// Create tenant + user + key
	doAdminRequest(t, handler, "POST", "/v1/admin/tenants", map[string]string{
		"id": "acme", "name": "Acme Corp",
	})
	doAdminRequest(t, handler, "POST", "/v1/admin/tenants/acme/users", map[string]string{
		"id": "u-cascade", "name": "Cascade", "role": "member",
	})
	createResp := doAdminRequest(t, handler, "POST", "/v1/admin/users/u-cascade/keys", map[string]string{
		"name": "test-key",
	})
	var createBody map[string]string
	json.NewDecoder(createResp.Body).Decode(&createBody)

	// Delete user — keys should cascade
	rec := doAdminRequest(t, handler, "DELETE", "/v1/admin/users/u-cascade", nil)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNoContent)
	}

	// Verify key is gone (revoke should return 404)
	keyID := createBody["id"]
	delKeyRec := doAdminRequest(t, handler, "DELETE", "/v1/admin/keys/"+keyID, nil)
	if delKeyRec.Code != http.StatusNotFound {
		t.Errorf("key should be cascade-deleted, got status %d", delKeyRec.Code)
	}
}

func TestListProviders(t *testing.T) {
	_, handler := setupAdminTest(t)
	// No provider lister set - should return empty array

	rec := doAdminRequest(t, handler, "GET", "/v1/admin/providers", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp []map[string]interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("expected 0 providers without lister, got %d", len(resp))
	}
}

func TestListProvidersWithData(t *testing.T) {
	_, handler := setupAdminTest(t)
	// Note: setupAdminTest doesn't set a provider lister,
	// so this test verifies the endpoint returns empty when no lister is set.
	// The full integration test happens via browser.
	rec := doAdminRequest(t, handler, "GET", "/v1/admin/providers", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAuditEndpoint(t *testing.T) {
	_, handler := setupAdminTest(t)

	rec := doAdminRequest(t, handler, "GET", "/v1/admin/audit", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp []interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	// Empty audit log is expected
	if len(resp) != 0 {
		t.Errorf("expected 0 audit entries, got %d", len(resp))
	}
}

func TestAuditWithFilters(t *testing.T) {
	_, handler := setupAdminTest(t)

	rec := doAdminRequest(t, handler, "GET", "/v1/admin/audit?tenant_id=default&limit=10", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAdminSkillsEndpoint(t *testing.T) {
	_, handler := setupAdminTest(t)
	// No SkillAdmin set - should return empty array

	rec := doAdminRequest(t, handler, "GET", "/v1/admin/skills", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp []interface{}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp) != 0 {
		t.Errorf("expected 0 skills without admin, got %d", len(resp))
	}
}

func TestAdminSkillsMethodNotAllowed(t *testing.T) {
	_, handler := setupAdminTest(t)

	rec := doAdminRequest(t, handler, "POST", "/v1/admin/skills", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestAuditMethodNotAllowed(t *testing.T) {
	_, handler := setupAdminTest(t)

	rec := doAdminRequest(t, handler, "POST", "/v1/admin/audit", nil)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusMethodNotAllowed)
	}
}

func TestAdminEndpointRejectsNonAdmin(t *testing.T) {
	db, err := persistence.Open(":memory:")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer func() { _ = db.Close() }()
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

// --- Provider Config Tests ---

// mockProviderReloader tracks reload calls for test assertions.
type mockProviderReloader struct {
	lastProvider string
	lastBaseURL  string
	lastAPIKey   string
	lastModel    string
	calls        int
	err          error
}

func (m *mockProviderReloader) ReloadProvider(providerName, baseURL, apiKey, model string) error {
	m.calls++
	m.lastProvider = providerName
	m.lastBaseURL = baseURL
	m.lastAPIKey = apiKey
	m.lastModel = model
	return m.err
}

func TestGetProviderConfig(t *testing.T) {
	_, handler := setupAdminTest(t)

	rec := doAdminRequest(t, handler, "GET", "/v1/admin/providers/config", nil)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// Should have "default" and "providers" keys
	if _, ok := resp["default"]; !ok {
		t.Error("missing 'default' field")
	}
	if _, ok := resp["providers"]; !ok {
		t.Error("missing 'providers' field")
	}
}

func TestUpdateProviderConfig(t *testing.T) {
	db, _ := setupAdminTest(t)

	// Setup mock reloader
	ar := NewAdminRouter(db.DB)
	// Re-wrap handler to access the admin router directly
	mockReloader := &mockProviderReloader{}
	ar.SetProviderReloader(mockReloader)

	wrappedHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := &auth.User{ID: "admin", TenantID: "default", Name: "Admin"}
		ctx := middleware.WithUser(r.Context(), user)
		ctx = middleware.WithUserRole(ctx, "admin")
		ar.Handler().ServeHTTP(w, r.WithContext(ctx))
	})

	// Update provider config
	rec := doAdminRequest(t, wrappedHandler, "PUT", "/v1/admin/providers/config", map[string]string{
		"provider": "openai",
		"base_url": "https://api.example.com/v1",
		"api_key":  "sk-test-key-123",
		"model":    "gpt-4o",
	})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, http.StatusOK, rec.Body.String())
	}

	// Verify reloader was called
	if mockReloader.calls != 1 {
		t.Errorf("ReloadProvider calls = %d, want 1", mockReloader.calls)
	}
	if mockReloader.lastProvider != "openai" {
		t.Errorf("provider = %q, want %q", mockReloader.lastProvider, "openai")
	}
	if mockReloader.lastAPIKey != "sk-test-key-123" {
		t.Errorf("api_key = %q, want %q", mockReloader.lastAPIKey, "sk-test-key-123")
	}

	// Verify config persisted in DB
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM provider_config WHERE provider_name = 'openai'").Scan(&count); err != nil {
		t.Fatalf("query: %v", err)
	}
	if count != 1 {
		t.Errorf("provider_config rows = %d, want 1", count)
	}

	// Verify GET returns masked key
	rec2 := doAdminRequest(t, wrappedHandler, "GET", "/v1/admin/providers/config", nil)
	if rec2.Code != http.StatusOK {
		t.Fatalf("GET status = %d, want %d", rec2.Code, http.StatusOK)
	}
	var getConfig map[string]interface{}
	json.Unmarshal(rec2.Body.Bytes(), &getConfig)
	providers := getConfig["providers"].(map[string]interface{})
	openai := providers["openai"].(map[string]interface{})
	if openai["api_key_set"] != true {
		t.Error("api_key_set should be true after setting key")
	}
	// The actual key should never appear in GET response
	if strings.Contains(rec2.Body.String(), "sk-test-key-123") {
		t.Error("API key should be masked in GET response")
	}
}

func TestUpdateProviderConfigValidation(t *testing.T) {
	_, handler := setupAdminTest(t)

	tests := []struct {
		name string
		body map[string]string
	}{
		{"missing provider", map[string]string{"base_url": "https://example.com", "model": "gpt-4o"}},
		{"missing model", map[string]string{"provider": "openai", "base_url": "https://example.com"}},
		{"invalid provider", map[string]string{"provider": "invalid", "base_url": "https://example.com", "model": "gpt-4o"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := doAdminRequest(t, handler, "PUT", "/v1/admin/providers/config", tt.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d for %s; body: %s", rec.Code, http.StatusBadRequest, tt.name, rec.Body.String())
			}
		})
	}
}

func TestTestProviderConnectionValidation(t *testing.T) {
	_, handler := setupAdminTest(t)

	// Missing required fields
	rec := doAdminRequest(t, handler, "POST", "/v1/admin/providers/test", map[string]string{
		"provider": "openai",
	})

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d; body: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestSetDefaultProvider(t *testing.T) {
	db, _ := setupAdminTest(t)

	ar := NewAdminRouter(db.DB)
	mockReloader := &mockProviderReloader{}
	ar.SetProviderReloader(mockReloader)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := &auth.User{ID: "admin", TenantID: "default", Name: "Admin"}
		ctx := middleware.WithUser(r.Context(), user)
		ctx = middleware.WithUserRole(ctx, "admin")
		ar.Handler().ServeHTTP(w, r.WithContext(ctx))
	})

	// Set openai as default with config
	rec := doAdminRequest(t, handler, "PUT", "/v1/admin/providers/config", map[string]string{
		"provider":    "openai",
		"base_url":    "https://api.openai.com/v1",
		"api_key":     "sk-test",
		"model":       "gpt-4o",
		"is_default":  "true",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d; body: %s", rec.Code, rec.Body.String())
	}

	// Verify default is set in DB
	var isDefault int
	if err := db.QueryRow("SELECT is_default FROM provider_config WHERE provider_name = 'openai'").Scan(&isDefault); err != nil {
		t.Fatalf("query: %v", err)
	}
	if isDefault != 1 {
		t.Errorf("is_default = %d, want 1", isDefault)
	}

	// Verify GET returns correct default
	rec2 := doAdminRequest(t, handler, "GET", "/v1/admin/providers/config", nil)
	var resp map[string]interface{}
	json.Unmarshal(rec2.Body.Bytes(), &resp)
	if resp["default"] != "openai" {
		t.Errorf("default = %v, want 'openai'", resp["default"])
	}
}
