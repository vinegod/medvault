package middleware

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"

	"github.com/vinegod/medvault/backend/internal/api"
	"github.com/vinegod/medvault/backend/internal/models"
)

// RequireHousehold looks up the caller's active household membership and
// injects household_id + role into the request context. Must run AFTER
// RequireAuth so user_id is already in ctx.
//
// Active-household selection: we pick the *most recently joined* household.
// A user registers (joined_at=now -> personal household), then optionally
// joins a shared household later via an invite (joined_at=later -> that
// household becomes active). This matches the "I just joined, that's where
// I'm working" UX without needing an explicit switch.
//
// Limitation: no way to switch back to an earlier household. Phase 11 will
// add `users.active_household_id` + a switcher endpoint when the frontend
// lands.
func RequireHousehold(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := UserIDFrom(r.Context())
			if userID == "" {
				api.WriteError(w, http.StatusUnauthorized, models.CodeUnauthorized, "auth required")
				return
			}

			var householdID, role string
			err := db.QueryRowContext(r.Context(),
				`SELECT household_id, role FROM memberships
				 WHERE user_id = ? ORDER BY joined_at DESC LIMIT 1`,
				userID,
			).Scan(&householdID, &role)
			if err == sql.ErrNoRows {
				api.WriteError(w, http.StatusForbidden, models.CodeForbidden, "no household membership")
				return
			}
			if err != nil {
				slog.Error("household middleware: query membership", "err", err)
				api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
				return
			}

			ctx := context.WithValue(r.Context(), householdIDKey, householdID)
			ctx = context.WithValue(ctx, roleKey, role)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
