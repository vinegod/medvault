package middleware

import (
	"database/sql"
	"net/http"

	"github.com/vinegod/medvault/backend/internal/api"
	"github.com/vinegod/medvault/backend/internal/database"
	"github.com/vinegod/medvault/backend/internal/models"
)

// RequireAdmin rejects callers who are not server admins with 403. Must run
// after RequireAuth.
func RequireAdmin(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			admin, err := database.IsAdmin(r.Context(), db, UserIDFrom(r.Context()))
			if err != nil {
				api.ServerError(w, "RequireAdmin: query", err)
				return
			}
			if !admin {
				api.WriteError(w, http.StatusForbidden, models.CodeForbidden, "server admin required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
