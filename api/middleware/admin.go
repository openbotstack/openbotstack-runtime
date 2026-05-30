package middleware

import (
	"log/slog"
	"net/http"
)

// RequireAdmin checks that the authenticated user has admin role.
// Returns 401 if not authenticated, 403 if authenticated but not admin.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, hasUser := UserFromContext(r.Context())
		if !hasUser || user == nil {
			slog.WarnContext(r.Context(), "access denied",
				"method", r.Method,
				"path", r.URL.Path,
				"status", http.StatusUnauthorized,
				"error", "not authenticated",
			)
			writeMiddlewareError(w, http.StatusUnauthorized, ErrUnauthorized, "not authenticated")
			return
		}
		role := RoleFromContext(r.Context())
		if role != "admin" {
			slog.WarnContext(r.Context(), "access denied",
				"method", r.Method,
				"path", r.URL.Path,
				"status", http.StatusForbidden,
				"error", "admin role required",
			)
			writeMiddlewareError(w, http.StatusForbidden, ErrForbidden, "admin role required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
