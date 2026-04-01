package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
	"github.com/openbotstack/openbotstack-core/access/auth"
)

type userContextKey struct{}

// WithUser adds a user to the context.
func WithUser(ctx context.Context, user *auth.User) context.Context {
	return context.WithValue(ctx, userContextKey{}, user)
}

// UserFromContext retrieves the user from the context.
func UserFromContext(ctx context.Context) (*auth.User, bool) {
	user, ok := ctx.Value(userContextKey{}).(*auth.User)
	return user, ok
}

// JWTMiddlewareConfig holds configuration for the JWT middleware.
type JWTMiddlewareConfig struct {
	// SecretKey is used to verify the JWT signature.
	SecretKey []byte
	// Strict determines if requests without a valid token are rejected.
	// If false, invalid or missing tokens just mean no User is attached to context.
	Strict bool
}

// JWTMiddleware creates an HTTP middleware that parses JWTs and attaches the User to the context.
func JWTMiddleware(config JWTMiddlewareConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				if config.Strict {
					http.Error(w, "missing authorization header", http.StatusUnauthorized)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			parts := strings.SplitN(authHeader, " ", 2)
			if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
				if config.Strict {
					http.Error(w, "invalid authorization header format", http.StatusUnauthorized)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			tokenStr := parts[1]
			token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
				return config.SecretKey, nil
			})

			if err != nil || !token.Valid {
				if config.Strict {
					http.Error(w, "invalid token", http.StatusUnauthorized)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			claims, ok := token.Claims.(jwt.MapClaims)
			if !ok {
				if config.Strict {
					http.Error(w, "invalid token claims", http.StatusUnauthorized)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			user := extractUserFromClaims(claims)
			ctx := WithUser(r.Context(), user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func extractUserFromClaims(claims jwt.MapClaims) *auth.User {
	user := &auth.User{}

	if tid, ok := claims["tenant_id"].(string); ok {
		user.TenantID = tid
	}
	// 'sub' is standard for user id
	if sub, ok := claims["sub"].(string); ok {
		user.ID = sub
	} else if uid, ok := claims["user_id"].(string); ok {
		user.ID = uid
	}

	if name, ok := claims["name"].(string); ok {
		user.Name = name
	}

	return user
}
