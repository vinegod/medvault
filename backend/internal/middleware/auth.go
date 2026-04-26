package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/vinegod/medvault/backend/internal/api"
	"github.com/vinegod/medvault/backend/internal/auth"
	"github.com/vinegod/medvault/backend/internal/models"
)

// RequireAuth returns middleware that:
//   - reads the "Authorization: Bearer <jwt>" header
//   - validates the JWT signature and expiry
//   - injects user_id and username into the request context
//
// Handlers downstream read those values via UserIDFrom / UsernameFrom.
// Any failure returns 401 with the standard error envelope.
func RequireAuth(jwtSecret string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := r.Header.Get("Authorization")
			if header == "" {
				api.WriteError(w, http.StatusUnauthorized, models.CodeUnauthorized, "missing Authorization header")
				return
			}

			// Expected: "Bearer <token>". Split on the first space.
			parts := strings.SplitN(header, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
				api.WriteError(w, http.StatusUnauthorized, models.CodeUnauthorized, "invalid Authorization header")
				return
			}

			claims, err := auth.ValidateToken(jwtSecret, parts[1])
			if err != nil {
				api.WriteError(w, http.StatusUnauthorized, models.CodeUnauthorized, "invalid or expired token")
				return
			}

			ctx := context.WithValue(r.Context(), userIDKey, claims.UserID)
			ctx = context.WithValue(ctx, usernameKey, claims.Username)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
