package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/vinegod/medvault/backend/internal/api"
	"github.com/vinegod/medvault/backend/internal/auth"
	"github.com/vinegod/medvault/backend/internal/middleware"
	"github.com/vinegod/medvault/backend/internal/models"
)

// LogoutOthers handles POST /api/auth/logout-others. It revokes every
// refresh token of the caller except the one in the body. Access tokens
// already issued stay valid until they expire.
func (h *AuthHandler) LogoutOthers(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFrom(r.Context())

	var req models.RefreshRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "invalid JSON")
		return
	}
	if err := req.Validate(); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, err.Error())
		return
	}

	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE refresh_tokens SET revoked_at = ?
		  WHERE user_id = ? AND revoked_at IS NULL AND token_hash != ?`,
		time.Now().UTC(), userID, auth.HashToken(req.RefreshToken),
	); err != nil {
		api.ServerError(w, "logoutOthers: revoke tokens", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, nil)
}

// DeleteAccount handles DELETE /api/auth/account. After checking the
// password it deletes households where the caller is the only member,
// leaves the others, and deletes the user. It refuses with
// OWNER_TRANSFER_REQUIRED while the caller is the only owner of a
// household that has other members.
func (h *AuthHandler) DeleteAccount(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFrom(r.Context())
	username := middleware.UsernameFrom(r.Context())

	var req models.DeleteAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "invalid JSON")
		return
	}
	if err := req.Validate(); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, err.Error())
		return
	}

	var hash string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT password_hash FROM users WHERE id = ?`, userID,
	).Scan(&hash); err != nil {
		api.ServerError(w, "deleteAccount: query user", err)
		return
	}
	if !auth.CheckPassword(req.Password, hash) {
		api.WriteError(w, http.StatusUnauthorized, models.CodeUnauthorized, "password is incorrect")
		return
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		api.ServerError(w, "deleteAccount: begin tx", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit

	blocking, err := householdsNeedingTransfer(r.Context(), tx, userID)
	if err != nil {
		api.ServerError(w, "deleteAccount", err)
		return
	}
	if len(blocking) > 0 {
		api.WriteError(w, http.StatusConflict, models.CodeOwnerTransferRequired,
			"transfer ownership before deleting your account: "+strings.Join(blocking, ", "))
		return
	}

	if err := removeUser(r.Context(), tx, userID, username); err != nil {
		api.ServerError(w, "deleteAccount", err)
		return
	}
	if err := tx.Commit(); err != nil {
		api.ServerError(w, "deleteAccount: commit", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, nil)
}

// householdsNeedingTransfer returns the names of households where userID is
// the only owner and other members remain.
func householdsNeedingTransfer(ctx context.Context, tx *sql.Tx, userID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx,
		`SELECT h.name
		   FROM memberships m
		   JOIN households h ON h.id = m.household_id
		  WHERE m.user_id = ? AND m.role = ?
		    AND NOT EXISTS (SELECT 1 FROM memberships o
		                     WHERE o.household_id = m.household_id AND o.user_id != m.user_id
		                       AND o.role = ?)
		    AND EXISTS (SELECT 1 FROM memberships o
		                 WHERE o.household_id = m.household_id AND o.user_id != m.user_id)
		  ORDER BY h.name`,
		userID, models.RoleOwner, models.RoleOwner,
	)
	if err != nil {
		return nil, fmt.Errorf("query owned households: %w", err)
	}
	defer rows.Close() //nolint:errcheck // error surfaced by rows.Err()

	var names []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("scan household name: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate owned households: %w", err)
	}
	return names, nil
}

// removeUser deletes the households where userID is the only member, hands
// the user's created_by references in the remaining households to an owner,
// logs the departure there, and deletes the user row.
func removeUser(ctx context.Context, tx *sql.Tx, userID, username string) error {
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM households
		  WHERE id IN (SELECT household_id FROM memberships WHERE user_id = ?)
		    AND NOT EXISTS (SELECT 1 FROM memberships o
		                     WHERE o.household_id = households.id AND o.user_id != ?)`,
		userID, userID,
	); err != nil {
		return fmt.Errorf("delete sole-member households: %w", err)
	}

	// created_by cascades on user delete, which would hard-delete these rows.
	if _, err := tx.ExecContext(ctx,
		`UPDATE medicines SET created_by = (
		     SELECT user_id FROM memberships o
		      WHERE o.household_id = medicines.household_id AND o.user_id != ? AND o.role = ?
		      LIMIT 1)
		  WHERE created_by = ?`,
		userID, models.RoleOwner, userID,
	); err != nil {
		return fmt.Errorf("reassign medicines: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE households SET created_by = (
		     SELECT user_id FROM memberships o
		      WHERE o.household_id = households.id AND o.user_id != ? AND o.role = ?
		      LIMIT 1)
		  WHERE created_by = ?`,
		userID, models.RoleOwner, userID,
	); err != nil {
		return fmt.Errorf("reassign households: %w", err)
	}

	if err := logDeparture(ctx, tx, userID, username); err != nil {
		return err
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, userID); err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	return nil
}

// logDeparture writes a household.member.remove row in every household
// userID still belongs to.
func logDeparture(ctx context.Context, tx *sql.Tx, userID, username string) error {
	rows, err := tx.QueryContext(ctx,
		`SELECT household_id FROM memberships WHERE user_id = ?`, userID)
	if err != nil {
		return fmt.Errorf("query memberships: %w", err)
	}
	var householdIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return fmt.Errorf("scan membership: %w", err)
		}
		householdIDs = append(householdIDs, id)
	}
	if err := errors.Join(rows.Err(), rows.Close()); err != nil {
		return fmt.Errorf("iterate memberships: %w", err)
	}

	for _, id := range householdIDs {
		WriteAudit(ctx, tx, &auditEvent{
			HouseholdID: id,
			UserID:      userID,
			Username:    username,
			Action:      "household.member.remove",
			TargetType:  "user",
			TargetID:    &userID,
			TargetName:  &username,
		})
	}
	return nil
}
