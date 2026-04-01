// Package api provides the REST API for OpenBotStack runtime.
package api

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
)

const (
	// CorrelationIDHeader is the HTTP header used to propagate correlation IDs.
	CorrelationIDHeader = "X-Correlation-ID"
)

type correlationIDKey struct{}

// CorrelationIDFromContext retrieves the correlation ID from the context.
func CorrelationIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(correlationIDKey{}).(string); ok {
		return id
	}
	return ""
}

// CorrelationMiddleware injects a correlation ID into each request context and
// adds it to the response headers. If an incoming request already carries the
// X-Correlation-ID header, that value is reused; otherwise a new UUID is
// generated.
//
// Every log line emitted via slog during the request will include the
// correlation_id attribute when the returned handler is used.
func CorrelationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		correlationID := r.Header.Get(CorrelationIDHeader)
		if correlationID == "" {
			correlationID = uuid.New().String()
		}

		// Propagate the correlation ID back to the caller.
		w.Header().Set(CorrelationIDHeader, correlationID)

		// Attach to context.
		ctx := context.WithValue(r.Context(), correlationIDKey{}, correlationID)

		// Create a logger that always includes the correlation_id attribute.
		logger := slog.With("correlation_id", correlationID)
		logger.Info("request started",
			"method", r.Method,
			"path", r.URL.Path,
		)

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
