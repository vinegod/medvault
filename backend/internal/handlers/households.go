package handlers

import (
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
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
		api.ServerError(w, "GetCurrent: query household", err)
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
		api.ServerError(w, "GetCurrent: query members", err)
		return
	}
	defer rows.Close() //nolint:errcheck // error surfaced by rows.Err()

	detail.Members = []models.MemberView{}
	for rows.Next() {
		var mv models.MemberView
		if err := rows.Scan(&mv.UserID, &mv.Username, &mv.Role, &mv.JoinedAt); err != nil {
			api.ServerError(w, "GetCurrent: scan member", err)
			return
		}
		detail.Members = append(detail.Members, mv)
	}
	if err := rows.Err(); err != nil {
		api.ServerError(w, "GetCurrent: rows error", err)
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

	now := time.Now().UTC()
	expiresAt := now.Add(7 * 24 * time.Hour)

	// Retry on PK collision. With a 32^8 keyspace this is theoretical, but
	// when it happens the user shouldn't see a 500 — try a fresh code first.
	var code string
	for attempt := 0; attempt < 3; attempt++ {
		c, err := generateInviteCode()
		if err != nil {
			api.ServerError(w, "CreateInvite: generate code", err)
			return
		}
		_, err = h.DB.ExecContext(r.Context(),
			`INSERT INTO household_invites (code, household_id, invited_by, expires_at, created_at)
			 VALUES (?, ?, ?, ?, ?)`,
			c, householdID, callerID, expiresAt, now,
		)
		if err == nil {
			code = c
			break
		}
		// Anything other than a unique-constraint violation is a real error.
		if !strings.Contains(err.Error(), "UNIQUE constraint failed") {
			api.ServerError(w, "CreateInvite: insert invite", err)
			return
		}
		slog.Warn("CreateInvite: code collision, retrying", "attempt", attempt+1)
	}
	if code == "" {
		slog.Error("CreateInvite: exhausted retries")
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
		 WHERE code = ?
		   AND used_at    IS NULL
		   AND revoked_at IS NULL
		   AND expires_at > ?`,
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
		api.ServerError(w, "Join: query invite", err)
		return
	}

	// Check if the caller is already a member of this household.
	var existing int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM memberships WHERE user_id = ? AND household_id = ?`,
		callerID, invite.HouseholdID,
	).Scan(&existing); err != nil {
		api.ServerError(w, "Join: check existing membership", err)
		return
	}
	if existing > 0 {
		api.WriteError(w, http.StatusConflict, models.CodeConflict, "already a member of this household")
		return
	}

	now := time.Now().UTC()

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		api.ServerError(w, "Join: begin tx", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit; error intentionally discarded

	if _, err = tx.ExecContext(r.Context(),
		`INSERT INTO memberships (user_id, household_id, role, joined_at)
		 VALUES (?, ?, ?, ?)`,
		callerID, invite.HouseholdID, models.RoleMember, now,
	); err != nil {
		api.ServerError(w, "Join: insert membership", err)
		return
	}

	if _, err = tx.ExecContext(r.Context(),
		`UPDATE household_invites SET used_at = ?, used_by = ? WHERE code = ?`,
		now, callerID, invite.Code,
	); err != nil {
		api.ServerError(w, "Join: mark invite used", err)
		return
	}

	// Auto-switch the caller's active household to the one they just joined.
	// This matches the frontend JoinHousehold flow which clears the local
	// cache and re-syncs immediately, expecting to see the joined data.
	if _, err = tx.ExecContext(r.Context(),
		`UPDATE users SET active_household_id = ? WHERE id = ?`,
		invite.HouseholdID, callerID,
	); err != nil {
		api.ServerError(w, "Join: set active household", err)
		return
	}

	if err = tx.Commit(); err != nil {
		api.ServerError(w, "Join: commit", err)
		return
	}

	// Audit outside the tx — the join already succeeded; an audit-write
	// failure shouldn't unwind it.
	username := middleware.UsernameFrom(r.Context())
	WriteAudit(r.Context(), h.DB, invite.HouseholdID, callerID, username,
		"household.member.add", "user", &callerID, &username)

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

	// Capture the username before deleting so the audit row stays readable.
	var targetUsername string
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT username FROM users WHERE id = ?`, targetID,
	).Scan(&targetUsername)

	res, err := h.DB.ExecContext(r.Context(),
		`DELETE FROM memberships WHERE user_id = ? AND household_id = ?`,
		targetID, householdID,
	)
	if err != nil {
		api.ServerError(w, "RemoveMember: delete", err)
		return
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		api.WriteError(w, http.StatusNotFound, models.CodeNotFound, "member not found in this household")
		return
	}

	WriteAudit(r.Context(), h.DB, householdID, callerID,
		middleware.UsernameFrom(r.Context()),
		"household.member.remove", "user", &targetID, &targetUsername)

	api.WriteJSON(w, http.StatusOK, nil)
}

// List handles GET /api/households. Returns every household the caller
// belongs to, with the active one flagged. Does NOT use the household
// middleware: a freshly-removed user must still be able to list any
// remaining memberships.
func (h *HouseholdHandler) List(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFrom(r.Context())

	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT hh.id, hh.name, m.role, m.joined_at,
		        (hh.id = u.active_household_id) AS is_active
		   FROM memberships m
		   JOIN households hh ON hh.id = m.household_id
		   JOIN users      u  ON u.id  = m.user_id
		  WHERE m.user_id = ?
		  ORDER BY m.joined_at ASC`,
		userID,
	)
	if err != nil {
		api.ServerError(w, "List: query memberships", err)
		return
	}
	defer rows.Close() //nolint:errcheck // error surfaced by rows.Err()

	out := []models.MembershipSummary{}
	for rows.Next() {
		var s models.MembershipSummary
		if err := rows.Scan(&s.ID, &s.Name, &s.Role, &s.JoinedAt, &s.IsActive); err != nil {
			api.ServerError(w, "List: scan", err)
			return
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		api.ServerError(w, "List: rows", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, out)
}

// Switch handles POST /api/households/switch. Sets the caller's active
// household after verifying they are still a member of the target.
func (h *HouseholdHandler) Switch(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFrom(r.Context())

	var req models.SwitchHouseholdRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "invalid JSON")
		return
	}
	if err := req.Validate(); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, err.Error())
		return
	}

	var role string
	err := h.DB.QueryRowContext(r.Context(),
		`SELECT role FROM memberships WHERE user_id = ? AND household_id = ?`,
		userID, req.HouseholdID,
	).Scan(&role)
	if errors.Is(err, sql.ErrNoRows) {
		api.WriteError(w, http.StatusForbidden, models.CodeForbidden, "not a member of that household")
		return
	}
	if err != nil {
		api.ServerError(w, "Switch: check membership", err)
		return
	}

	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE users SET active_household_id = ? WHERE id = ?`,
		req.HouseholdID, userID,
	); err != nil {
		api.ServerError(w, "Switch: update active", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, map[string]string{
		"household_id": req.HouseholdID,
		"role":         role,
	})
}

// Create handles POST /api/households. Creates a new household, makes the
// caller the owner, and switches their active household to the new one.
// All in one transaction so a partial failure leaves no orphans.
func (h *HouseholdHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFrom(r.Context())

	var req models.CreateHouseholdRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "invalid JSON")
		return
	}
	if err := req.Validate(); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, err.Error())
		return
	}

	householdID := uuid.NewString()
	now := time.Now().UTC()

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		api.ServerError(w, "Create: begin tx", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit; error intentionally discarded

	if _, err = tx.ExecContext(r.Context(),
		`INSERT INTO households (id, name, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		householdID, req.Name, userID, now, now,
	); err != nil {
		api.ServerError(w, "Create: insert household", err)
		return
	}

	if _, err = tx.ExecContext(r.Context(),
		`INSERT INTO memberships (user_id, household_id, role, joined_at)
		 VALUES (?, ?, ?, ?)`,
		userID, householdID, models.RoleOwner, now,
	); err != nil {
		api.ServerError(w, "Create: insert membership", err)
		return
	}

	if _, err = tx.ExecContext(r.Context(),
		`UPDATE users SET active_household_id = ? WHERE id = ?`,
		householdID, userID,
	); err != nil {
		api.ServerError(w, "Create: set active", err)
		return
	}

	if err = tx.Commit(); err != nil {
		api.ServerError(w, "Create: commit", err)
		return
	}

	api.WriteJSON(w, http.StatusCreated, models.Household{
		ID:        householdID,
		Name:      req.Name,
		CreatedBy: userID,
		CreatedAt: now,
		UpdatedAt: now,
	})
}

// Rename handles PATCH /api/households/{id}. Owners only.
func (h *HouseholdHandler) Rename(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())
	role := middleware.RoleFrom(r.Context())
	paramID := chi.URLParam(r, "id")

	if paramID != householdID {
		api.WriteError(w, http.StatusForbidden, models.CodeForbidden, "not your household")
		return
	}
	if role != models.RoleOwner {
		api.WriteError(w, http.StatusForbidden, models.CodeForbidden, "only owners can rename")
		return
	}

	var req models.RenameHouseholdRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "invalid JSON")
		return
	}
	if err := req.Validate(); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, err.Error())
		return
	}

	now := time.Now().UTC()
	if _, err := h.DB.ExecContext(r.Context(),
		`UPDATE households SET name = ?, updated_at = ? WHERE id = ?`,
		req.Name, now, householdID,
	); err != nil {
		api.ServerError(w, "Rename: update", err)
		return
	}

	WriteAudit(r.Context(), h.DB, householdID,
		middleware.UserIDFrom(r.Context()),
		middleware.UsernameFrom(r.Context()),
		"household.rename", "household", &householdID, &req.Name)

	api.WriteJSON(w, http.StatusOK, map[string]any{
		"id":         householdID,
		"name":       req.Name,
		"updated_at": now,
	})
}

// Leave handles DELETE /api/households/{id}/membership. Caller leaves the
// given household. Special cases:
//   - Sole member (count = 1): household is deleted (cascades to medicines).
//   - Sole owner with other members still present: 409
//     OWNER_TRANSFER_REQUIRED. Caller must transfer first.
//   - Otherwise: membership row deleted; if the household was the user's
//     active one, the FK ON DELETE SET NULL clears the pointer (we delete
//     by user_id+household_id, so the FK doesn't fire — handle manually).
func (h *HouseholdHandler) Leave(w http.ResponseWriter, r *http.Request) {
	userID := middleware.UserIDFrom(r.Context())
	paramID := chi.URLParam(r, "id")

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		api.ServerError(w, "Leave: begin tx", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit; error intentionally discarded

	// Verify the caller is actually a member, and capture their role.
	var callerRole string
	err = tx.QueryRowContext(r.Context(),
		`SELECT role FROM memberships WHERE user_id = ? AND household_id = ?`,
		userID, paramID,
	).Scan(&callerRole)
	if errors.Is(err, sql.ErrNoRows) {
		api.WriteError(w, http.StatusNotFound, models.CodeNotFound, "not a member of that household")
		return
	}
	if err != nil {
		api.ServerError(w, "Leave: check membership", err)
		return
	}

	var totalMembers, totalOwners int
	// COALESCE: SUM over zero rows returns NULL, which would fail to scan
	// into an int. Cascade rules make this unreachable today, but defending
	// against a NULL is a one-word change.
	if err := tx.QueryRowContext(r.Context(),
		`SELECT COUNT(*),
		        COALESCE(SUM(CASE WHEN role = ? THEN 1 ELSE 0 END), 0)
		   FROM memberships WHERE household_id = ?`,
		models.RoleOwner, paramID,
	).Scan(&totalMembers, &totalOwners); err != nil {
		api.ServerError(w, "Leave: count members", err)
		return
	}

	// Sole owner of a household with co-members must transfer first.
	if callerRole == models.RoleOwner && totalOwners == 1 && totalMembers > 1 {
		api.WriteError(w, http.StatusConflict, models.CodeOwnerTransferRequired,
			"transfer ownership before leaving")
		return
	}

	if totalMembers == 1 {
		// Sole member — delete the household. Cascade clears medicines,
		// invites, memberships, and SET NULLs active_household_id.
		if _, err := tx.ExecContext(r.Context(),
			`DELETE FROM households WHERE id = ?`, paramID,
		); err != nil {
			api.ServerError(w, "Leave: delete household", err)
			return
		}
	} else {
		if _, err := tx.ExecContext(r.Context(),
			`DELETE FROM memberships WHERE user_id = ? AND household_id = ?`,
			userID, paramID,
		); err != nil {
			api.ServerError(w, "Leave: delete membership", err)
			return
		}
		// Membership delete doesn't trigger the FK ON DELETE on
		// users.active_household_id (that fires on households deletion).
		// Clear the pointer manually if it was the household we left.
		if _, err := tx.ExecContext(r.Context(),
			`UPDATE users SET active_household_id = NULL
			  WHERE id = ? AND active_household_id = ?`,
			userID, paramID,
		); err != nil {
			api.ServerError(w, "Leave: clear active", err)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		api.ServerError(w, "Leave: commit", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, nil)
}

// Transfer handles POST /api/households/{id}/transfer. Owner promotes
// another member to owner and demotes themselves to member. Single tx.
func (h *HouseholdHandler) Transfer(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())
	role := middleware.RoleFrom(r.Context())
	callerID := middleware.UserIDFrom(r.Context())
	paramID := chi.URLParam(r, "id")

	if paramID != householdID {
		api.WriteError(w, http.StatusForbidden, models.CodeForbidden, "not your household")
		return
	}
	if role != models.RoleOwner {
		api.WriteError(w, http.StatusForbidden, models.CodeForbidden, "only owners can transfer")
		return
	}

	var req models.TransferOwnershipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "invalid JSON")
		return
	}
	if err := req.Validate(); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, err.Error())
		return
	}
	if req.ToUserID == callerID {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation,
			"cannot transfer ownership to yourself")
		return
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		api.ServerError(w, "Transfer: begin tx", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit; error intentionally discarded

	// Verify target is a member of this household.
	var existing int
	if err := tx.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM memberships
		  WHERE user_id = ? AND household_id = ?`,
		req.ToUserID, householdID,
	).Scan(&existing); err != nil {
		api.ServerError(w, "Transfer: check target", err)
		return
	}
	if existing == 0 {
		api.WriteError(w, http.StatusNotFound, models.CodeNotFound,
			"target user is not a member of this household")
		return
	}

	if _, err := tx.ExecContext(r.Context(),
		`UPDATE memberships SET role = ?
		  WHERE user_id = ? AND household_id = ?`,
		models.RoleOwner, req.ToUserID, householdID,
	); err != nil {
		api.ServerError(w, "Transfer: promote target", err)
		return
	}
	if _, err := tx.ExecContext(r.Context(),
		`UPDATE memberships SET role = ?
		  WHERE user_id = ? AND household_id = ?`,
		models.RoleMember, callerID, householdID,
	); err != nil {
		api.ServerError(w, "Transfer: demote caller", err)
		return
	}

	if err := tx.Commit(); err != nil {
		api.ServerError(w, "Transfer: commit", err)
		return
	}

	var newOwnerName string
	_ = h.DB.QueryRowContext(r.Context(),
		`SELECT username FROM users WHERE id = ?`, req.ToUserID,
	).Scan(&newOwnerName)
	WriteAudit(r.Context(), h.DB, householdID, callerID,
		middleware.UsernameFrom(r.Context()),
		"household.transfer", "user", &req.ToUserID, &newOwnerName)

	api.WriteJSON(w, http.StatusOK, nil)
}

// ListInvites handles GET /api/households/{id}/invites. Owner only.
// Returns active (unused, unrevoked, unexpired) codes for this household.
func (h *HouseholdHandler) ListInvites(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())
	role := middleware.RoleFrom(r.Context())
	paramID := chi.URLParam(r, "id")

	if paramID != householdID {
		api.WriteError(w, http.StatusForbidden, models.CodeForbidden, "not your household")
		return
	}
	if role != models.RoleOwner {
		api.WriteError(w, http.StatusForbidden, models.CodeForbidden, "only owners can list invites")
		return
	}

	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT code, household_id, invited_by, expires_at, created_at
		   FROM household_invites
		  WHERE household_id = ?
		    AND used_at    IS NULL
		    AND revoked_at IS NULL
		    AND expires_at > ?
		  ORDER BY created_at DESC`,
		householdID, time.Now().UTC(),
	)
	if err != nil {
		api.ServerError(w, "ListInvites: query", err)
		return
	}
	defer rows.Close() //nolint:errcheck // error surfaced by rows.Err()

	out := []models.Invite{}
	for rows.Next() {
		var inv models.Invite
		if err := rows.Scan(&inv.Code, &inv.HouseholdID, &inv.InvitedBy,
			&inv.ExpiresAt, &inv.CreatedAt); err != nil {
			api.ServerError(w, "ListInvites: scan", err)
			return
		}
		out = append(out, inv)
	}
	if err := rows.Err(); err != nil {
		api.ServerError(w, "ListInvites: rows", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, out)
}

// RevokeInvite handles DELETE /api/households/{id}/invites/{code}. Owner
// only. Sets revoked_at; the row stays for audit but Join now rejects it.
func (h *HouseholdHandler) RevokeInvite(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())
	role := middleware.RoleFrom(r.Context())
	paramID := chi.URLParam(r, "id")
	code := chi.URLParam(r, "code")

	if paramID != householdID {
		api.WriteError(w, http.StatusForbidden, models.CodeForbidden, "not your household")
		return
	}
	if role != models.RoleOwner {
		api.WriteError(w, http.StatusForbidden, models.CodeForbidden, "only owners can revoke invites")
		return
	}

	res, err := h.DB.ExecContext(r.Context(),
		`UPDATE household_invites
		    SET revoked_at = ?
		  WHERE code = ?
		    AND household_id = ?
		    AND used_at    IS NULL
		    AND revoked_at IS NULL`,
		time.Now().UTC(), code, householdID,
	)
	if err != nil {
		api.ServerError(w, "RevokeInvite: update", err)
		return
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		api.WriteError(w, http.StatusNotFound, models.CodeNotFound,
			"invite not found, already used, or already revoked")
		return
	}

	api.WriteJSON(w, http.StatusOK, nil)
}
