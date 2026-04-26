package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vinegod/medvault/backend/internal/api"
	"github.com/vinegod/medvault/backend/internal/middleware"
	"github.com/vinegod/medvault/backend/internal/models"
)

// HouseholdHandler holds dependencies for all household endpoints.
type HouseholdHandler struct {
	DB *sql.DB
}

// inviteCodeCharset is uppercase alphanumeric — easy to read aloud and type.
const inviteCodeCharset = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // 32 chars, no ambiguous 0/O/1/I

func generateInviteCode() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate invite code: %w", err)
	}
	code := make([]byte, 8)
	for i, v := range b {
		code[i] = inviteCodeCharset[int(v)%len(inviteCodeCharset)]
	}
	return string(code), nil
}

// GetCurrent handles GET /api/households/current.
// Returns the caller's household details plus all current members.
func (h *HouseholdHandler) GetCurrent(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())

	var detail models.HouseholdDetail
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT id, name, created_by, created_at, updated_at
		 FROM households WHERE id = ?`, householdID,
	).Scan(
		&detail.ID, &detail.Name, &detail.CreatedBy,
		&detail.CreatedAt, &detail.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		api.WriteError(w, http.StatusNotFound, models.CodeNotFound, "household not found")
		return
	}
	if err != nil {
		slog.Error("GetCurrent: query household", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT m.user_id, u.username, m.role, m.joined_at
		 FROM memberships m
		 JOIN users u ON u.id = m.user_id
		 WHERE m.household_id = ?
		 ORDER BY m.joined_at ASC`,
		householdID,
	)
	if err != nil {
		slog.Error("GetCurrent: query members", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}
	defer rows.Close() //nolint:errcheck // error surfaced by rows.Err()

	detail.Members = []models.MemberView{}
	for rows.Next() {
		var mv models.MemberView
		if err := rows.Scan(&mv.UserID, &mv.Username, &mv.Role, &mv.JoinedAt); err != nil {
			slog.Error("GetCurrent: scan member", "err", err)
			api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
			return
		}
		detail.Members = append(detail.Members, mv)
	}
	if err := rows.Err(); err != nil {
		slog.Error("GetCurrent: rows error", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	api.WriteJSON(w, http.StatusOK, detail)
}

// CreateInvite handles POST /api/households/{id}/invite.
// Only owners may generate invite codes.
func (h *HouseholdHandler) CreateInvite(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())
	role := middleware.RoleFrom(r.Context())
	callerID := middleware.UserIDFrom(r.Context())
	paramID := chi.URLParam(r, "id")

	// The path param must match the caller's active household so a member
	// can't craft a request for a household they don't belong to.
	if paramID != householdID {
		api.WriteError(w, http.StatusForbidden, models.CodeForbidden, "not your household")
		return
	}
	if role != models.RoleOwner {
		api.WriteError(w, http.StatusForbidden, models.CodeForbidden, "only owners can invite")
		return
	}

	code, err := generateInviteCode()
	if err != nil {
		slog.Error("CreateInvite: generate code", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	now := time.Now().UTC()
	expiresAt := now.Add(7 * 24 * time.Hour)
	_, err = h.DB.ExecContext(r.Context(),
		`INSERT INTO household_invites (code, household_id, invited_by, expires_at, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		code, householdID, callerID, expiresAt, now,
	)
	if err != nil {
		slog.Error("CreateInvite: insert invite", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	api.WriteJSON(w, http.StatusCreated, models.Invite{
		Code:        code,
		HouseholdID: householdID,
		InvitedBy:   callerID,
		ExpiresAt:   expiresAt,
		CreatedAt:   now,
	})
}

// Join handles POST /api/households/join.
// Redeems an invite code and adds the caller as a member of that household.
func (h *HouseholdHandler) Join(w http.ResponseWriter, r *http.Request) {
	callerID := middleware.UserIDFrom(r.Context())

	var req models.JoinRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "invalid JSON")
		return
	}
	if err := req.Validate(); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, err.Error())
		return
	}

	var invite models.Invite
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT code, household_id, invited_by, expires_at, used_at
		 FROM household_invites
		 WHERE code = ? AND used_at IS NULL AND expires_at > ?`,
		req.Code, time.Now().UTC(),
	).Scan(
		&invite.Code, &invite.HouseholdID, &invite.InvitedBy,
		&invite.ExpiresAt, &invite.UsedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "invalid or expired invite code")
		return
	}
	if err != nil {
		slog.Error("Join: query invite", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	// Check if the caller is already a member of this household.
	var existing int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM memberships WHERE user_id = ? AND household_id = ?`,
		callerID, invite.HouseholdID,
	).Scan(&existing); err != nil {
		slog.Error("Join: check existing membership", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}
	if existing > 0 {
		api.WriteError(w, http.StatusConflict, models.CodeConflict, "already a member of this household")
		return
	}

	now := time.Now().UTC()

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		slog.Error("Join: begin tx", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit; error intentionally discarded

	if _, err = tx.ExecContext(r.Context(),
		`INSERT INTO memberships (user_id, household_id, role, joined_at)
		 VALUES (?, ?, ?, ?)`,
		callerID, invite.HouseholdID, models.RoleMember, now,
	); err != nil {
		slog.Error("Join: insert membership", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	if _, err = tx.ExecContext(r.Context(),
		`UPDATE household_invites SET used_at = ?, used_by = ? WHERE code = ?`,
		now, callerID, invite.Code,
	); err != nil {
		slog.Error("Join: mark invite used", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	if err = tx.Commit(); err != nil {
		slog.Error("Join: commit", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	api.WriteJSON(w, http.StatusOK, map[string]string{
		"household_id": invite.HouseholdID,
	})
}

// RemoveMember handles DELETE /api/households/{id}/members/{user_id}.
// Only owners may remove members. Owners cannot remove themselves.
func (h *HouseholdHandler) RemoveMember(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())
	role := middleware.RoleFrom(r.Context())
	callerID := middleware.UserIDFrom(r.Context())
	paramID := chi.URLParam(r, "id")
	targetID := chi.URLParam(r, "user_id")

	if paramID != householdID {
		api.WriteError(w, http.StatusForbidden, models.CodeForbidden, "not your household")
		return
	}
	if role != models.RoleOwner {
		api.WriteError(w, http.StatusForbidden, models.CodeForbidden, "only owners can remove members")
		return
	}
	if targetID == callerID {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "owners cannot remove themselves")
		return
	}

	res, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM memberships WHERE user_id = ? AND household_id = ?`,
		targetID, householdID,
	)
	if err != nil {
		slog.Error("RemoveMember: delete", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		api.WriteError(w, http.StatusNotFound, models.CodeNotFound, "member not found in this household")
		return
	}

	api.WriteJSON(w, http.StatusOK, nil)
}
