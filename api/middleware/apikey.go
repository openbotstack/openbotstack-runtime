package middleware

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"log/slog"
	"net/http"
	"time"

	"github.com/openbotstack/openbotstack-core/access/auth"
)

// APIKeyMiddlewareConfig holds configuration for API Key auth.
type APIKeyMiddlewareConfig struct {
	DB     *sql.DB
	Strict bool // If true, reject requests without valid API key
}

// APIKeyMiddleware creates middleware that validates X-API-Key header.
// Flow:
// 1. Read X-API-Key header
// 2. If present: SHA256(key) → query api_keys WHERE key_hash = ? AND revoked = 0
// 3. If found: load user (join users table) → WithUser(ctx, user) + store role
// 4. If not found and Strict: 401 Unauthorized
// 5. If not found and !Strict: pass through without user
func APIKeyMiddleware(config APIKeyMiddlewareConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := r.Header.Get("X-API-Key")
			if apiKey == "" {
				if config.Strict {
					slog.WarnContext(r.Context(), "api key authentication failed",
						"method", r.Method,
						"path", r.URL.Path,
						"status", http.StatusUnauthorized,
						"error", "missing key",
						"remote_addr", r.RemoteAddr,
					)
					writeMiddlewareError(w, http.StatusUnauthorized, ErrUnauthorized, "missing API key")
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			// Hash the provided key
			hash := sha256.Sum256([]byte(apiKey))
			hashHex := hex.EncodeToString(hash[:])

			// Look up key + user info in single query
			var userID, tenantID, userName, userRole string
			var expiresAt string
			err := config.DB.QueryRow(`
				SELECT u.id, u.tenant_id, u.name, u.role, k.expires_at
				FROM api_keys k
				JOIN users u ON k.user_id = u.id
				WHERE k.key_hash = ? AND k.revoked = 0`, hashHex,
			).Scan(&userID, &tenantID, &userName, &userRole, &expiresAt)

			if err == sql.ErrNoRows {
				if config.Strict {
					slog.WarnContext(r.Context(), "api key authentication failed",
						"method", r.Method,
						"path", r.URL.Path,
						"status", http.StatusUnauthorized,
						"error", "invalid key",
						"remote_addr", r.RemoteAddr,
					)
					writeMiddlewareError(w, http.StatusUnauthorized, ErrUnauthorized, "invalid API key")
					return
				}
				next.ServeHTTP(w, r)
				return
			}
			if err != nil {
				slog.ErrorContext(r.Context(), "api key lookup error",
					"method", r.Method,
					"path", r.URL.Path,
					"status", http.StatusInternalServerError,
					"error", err,
					"remote_addr", r.RemoteAddr,
				)
				writeMiddlewareError(w, http.StatusInternalServerError, ErrInternal, "internal error")
				return
			}

			// Check expiry
			if expiresAt != "" {
				exp, parseErr := time.Parse(time.RFC3339Nano, expiresAt)
				if parseErr == nil && time.Now().UTC().After(exp) {
					if config.Strict {
						slog.WarnContext(r.Context(), "api key authentication failed",
							"method", r.Method,
							"path", r.URL.Path,
							"status", http.StatusUnauthorized,
							"error", "expired key",
							"remote_addr", r.RemoteAddr,
						)
						writeMiddlewareError(w, http.StatusUnauthorized, ErrUnauthorized, "API key expired")
						return
					}
					next.ServeHTTP(w, r)
					return
				}
			}

			// Inject user + role into context
			user := &auth.User{
				ID:       userID,
				TenantID: tenantID,
				Name:     userName,
			}
			ctx := WithUserRole(WithUser(r.Context(), user), userRole)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type roleContextKey struct{}

// WithUserRole adds a user role to the context.
func WithUserRole(ctx context.Context, role string) context.Context {
	return context.WithValue(ctx, roleContextKey{}, role)
}

// RoleFromContext retrieves the user role from the context.
func RoleFromContext(ctx context.Context) string {
	role, _ := ctx.Value(roleContextKey{}).(string)
	return role
}
