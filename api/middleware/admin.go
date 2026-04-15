package middleware

import (
	"log/slog"
	"net/http"
)

// RequireAdmin checks that the authenticated user has admin role.
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
