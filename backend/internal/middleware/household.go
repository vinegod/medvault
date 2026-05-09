package middleware

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/vinegod/medvault/backend/internal/api"
	"github.com/vinegod/medvault/backend/internal/models"
)

// RequireHousehold reads the caller's active household + role and injects
// both into the request context. Must run AFTER RequireAuth so user_id is
// already present.
//
// Active-household resolution:
//  1. Read users.active_household_id (set by Register / Switch / Join).
//  2. Verify the caller is still a member of that household.
//  3. If the pointer is null OR the membership has been removed (the
//     household was deleted, or another owner kicked them), fall back to
//     any remaining membership and persist it. This keeps the user
//     functional after destructive actions taken on other devices.
//  4. If no membership exists at all → 403.
func RequireHousehold(db *sql.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			userID := UserIDFrom(r.Context())
			if userID == "" {
				api.WriteError(w, http.StatusUnauthorized, models.CodeUnauthorized, "auth required")
				return
			}

			householdID, role, err := resolveActiveHousehold(r.Context(), db, userID)
			if errors.Is(err, errNoMembership) {
				api.WriteError(w, http.StatusForbidden, models.CodeForbidden, "no household membership")
				return
			}
			if err != nil {
				slog.Error("household middleware: resolve", "err", err)
				api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
				return
			}

			ctx := context.WithValue(r.Context(), householdIDKey, householdID)
			ctx = context.WithValue(ctx, roleKey, role)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

var errNoMembership = errors.New("no household membership")

// resolveActiveHousehold returns (householdID, role) for the caller's
// active household. If the stored active_household_id is stale, it picks
// the most-recently-joined membership instead and persists the choice
// so subsequent requests skip the fallback path.
func resolveActiveHousehold(
	ctx context.Context, db *sql.DB, userID string,
) (householdID, role string, err error) {
	var activeIDN, roleN sql.NullString
	err = db.QueryRowContext(ctx,
		`SELECT u.active_household_id, m.role
		   FROM users u
		   LEFT JOIN memberships m
		     ON m.user_id = u.id
		    AND m.household_id = u.active_household_id
		  WHERE u.id = ?`,
		userID,
	).Scan(&activeIDN, &roleN)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", errNoMembership
	}
	if err != nil {
		return "", "", fmt.Errorf("query active household: %w", err)
	}
	if activeIDN.Valid && roleN.Valid {
		return activeIDN.String, roleN.String, nil
	}

	// Pointer is null OR the membership row is gone. Pick a fallback.
	var fallbackID, fallbackRole string
	err = db.QueryRowContext(ctx,
		`SELECT household_id, role FROM memberships
		  WHERE user_id = ?
		  ORDER BY joined_at DESC
		  LIMIT 1`,
		userID,
	).Scan(&fallbackID, &fallbackRole)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", errNoMembership
	}
	if err != nil {
		return "", "", fmt.Errorf("query fallback membership: %w", err)
	}

	if _, err := db.ExecContext(ctx,
		`UPDATE users SET active_household_id = ? WHERE id = ?`,
		fallbackID, userID,
	); err != nil {
		return "", "", fmt.Errorf("persist fallback active: %w", err)
	}

	return fallbackID, fallbackRole, nil
}
