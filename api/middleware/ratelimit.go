package middleware

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	ratelimit "github.com/openbotstack/openbotstack-core/access/ratelimit"
)

// rateLimitBypassPaths are paths that skip rate limiting entirely.
var rateLimitBypassPaths = map[string]bool{
	"/health":  true,
	"/healthz": true,
	"/readyz":  true,
	"/metrics": true,
}

// RateLimitMiddleware creates HTTP middleware that enforces rate limits
// based on the authenticated user's tenant/user identity in the request context.
//
// Behavior:
//   - Health/metrics endpoints bypass rate limiting entirely.
//   - If no user is in context (auth middleware handles rejection), pass through.
//   - If the rate limiter returns ErrQuotaNotFound, pass through (no quota configured).
//   - On other rate limiter errors, log and pass through (fail-open).
//   - If allowed, sets X-RateLimit-Remaining header on the response.
//   - If blocked, returns 429 with Retry-After header.
func RateLimitMiddleware(limiter ratelimit.RateLimiter) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip rate limiting for health/metrics endpoints.
			if rateLimitBypassPaths[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			// If no user in context, pass through — auth middleware handles rejection.
			user, ok := UserFromContext(r.Context())
			if !ok {
				next.ServeHTTP(w, r)
				return
			}

			key := ratelimit.RateLimitKey{
				TenantID: user.TenantID,
				UserID:   user.ID,
			}

			result, err := limiter.Allow(r.Context(), key)
			if err != nil {
				if errors.Is(err, ratelimit.ErrQuotaNotFound) {
					// No quota configured for this tenant — pass through.
					next.ServeHTTP(w, r)
					return
				}
				// Other errors: log and pass through (fail-open, don't block on limiter failure).
				slog.ErrorContext(r.Context(), "rate limiter error, passing through",
					"method", r.Method,
					"path", r.URL.Path,
					"tenant_id", user.TenantID,
					"user_id", user.ID,
					"error", err,
				)
				next.ServeHTTP(w, r)
				return
			}

			if !result.Allowed {
				w.Header().Set("Retry-After", strconv.Itoa(int(result.RetryAfter.Seconds())))
				writeMiddlewareError(w, http.StatusTooManyRequests, ErrRateLimited, "rate limit exceeded")
				return
			}

			// Set remaining quota header on the response.
			w.Header().Set("X-RateLimit-Remaining", strconv.FormatInt(result.Remaining, 10))
			next.ServeHTTP(w, r)
		})
	}
}
