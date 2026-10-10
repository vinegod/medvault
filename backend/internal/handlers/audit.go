package handlers

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/vinegod/medvault/backend/internal/api"
	"github.com/vinegod/medvault/backend/internal/middleware"
	"github.com/vinegod/medvault/backend/internal/models"
)

const (
	auditDefaultLimit = 50
	auditMaxLimit     = 200
)

// AuditHandler exposes the household activity log. Writes happen via
// WriteAudit (called inline by other handlers); reads via List below.
type AuditHandler struct {
	DB *sql.DB
}

// auditEntry is one row in the response. QuantityBefore/After are set when
// a medicine's quantity changed.
type auditEntry struct {
	ID             string    `json:"id"`
	Username       string    `json:"username"`
	Action         string    `json:"action"`
	TargetType     string    `json:"target_type"`
	TargetID       *string   `json:"target_id"`
	TargetName     *string   `json:"target_name"`
	QuantityBefore *int      `json:"quantity_before"`
	QuantityAfter  *int      `json:"quantity_after"`
	At             time.Time `json:"at"`
}

// List handles GET /api/households/{id}/audit?limit=N. Owners and members
// alike can read the log; the household middleware already constrains
// access to members.
func (h *AuditHandler) List(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())
	if chi.URLParam(r, "id") != householdID {
		api.WriteError(w, http.StatusForbidden, models.CodeForbidden, "not your household")
		return
	}

	entries, err := h.queryAudit(r, householdID, nil)
	if err != nil {
		api.ServerError(w, "Audit list", err)
		return
	}
	api.WriteJSON(w, http.StatusOK, entries)
}

// MedicineHistory handles GET /api/medicines/{id}/history?limit=N: the
// audit rows for one medicine in the caller's household, newest first.
// An unknown id yields an empty list.
func (h *AuditHandler) MedicineHistory(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())
	id := chi.URLParam(r, "id")

	entries, err := h.queryAudit(r, householdID, &id)
	if err != nil {
		api.ServerError(w, "Medicine history", err)
		return
	}
	api.WriteJSON(w, http.StatusOK, entries)
}

// queryAudit reads the household's audit rows, newest first, optionally
// narrowed to one target. The limit comes from the ?limit query parameter.
func (h *AuditHandler) queryAudit(r *http.Request, householdID string, targetID *string) ([]auditEntry, error) {
	query := `SELECT id, username, action, target_type, target_id, target_name,
	                 quantity_before, quantity_after, at
	            FROM household_audit
	           WHERE household_id = ?`
	args := []any{householdID}
	if targetID != nil {
		query += ` AND target_id = ?`
		args = append(args, *targetID)
	}
	// at has one-second resolution; rowid orders rows written in the same second.
	query += ` ORDER BY at DESC, rowid DESC LIMIT ?`
	args = append(args, auditLimit(r))

	rows, err := h.DB.QueryContext(r.Context(), query, args...)
	if err != nil {
		return nil, fmt.Errorf("query audit: %w", err)
	}
	defer rows.Close() //nolint:errcheck // error surfaced by rows.Err()

	out := []auditEntry{}
	for rows.Next() {
		var e auditEntry
		if err := rows.Scan(
			&e.ID, &e.Username, &e.Action, &e.TargetType, &e.TargetID, &e.TargetName,
			&e.QuantityBefore, &e.QuantityAfter, &e.At,
		); err != nil {
			return nil, fmt.Errorf("scan audit: %w", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit: %w", err)
	}
	return out, nil
}

func auditLimit(r *http.Request) int {
	n, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || n <= 0 {
		return auditDefaultLimit
	}
	return min(n, auditMaxLimit)
}

// sqlExec is satisfied by both *sql.DB and *sql.Tx.
type sqlExec interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// auditEvent is one row to append to household_audit.
type auditEvent struct {
	HouseholdID    string
	UserID         string
	Username       string
	Action         string
	TargetType     string
	TargetID       *string
	TargetName     *string
	QuantityBefore *int
	QuantityAfter  *int
}

// medicineEvent builds the audit event for a medicine action by the caller.
func medicineEvent(ctx context.Context, householdID, action string, m *models.Medicine) *auditEvent {
	return &auditEvent{
		HouseholdID: householdID,
		UserID:      middleware.UserIDFrom(ctx),
		Username:    middleware.UsernameFrom(ctx),
		Action:      action,
		TargetType:  "medicine",
		TargetID:    &m.ID,
		TargetName:  &m.Name,
	}
}

// WriteAudit appends one row to household_audit. Errors are logged, not
// returned, so an audit failure never blocks the mutation. Pass the
// caller's *sql.Tx to commit atomically with it, or the *sql.DB otherwise.
func WriteAudit(ctx context.Context, exec sqlExec, e *auditEvent) {
	_, err := exec.ExecContext(ctx,
		`INSERT INTO household_audit
		   (id, household_id, user_id, username, action, target_type, target_id, target_name,
		    quantity_before, quantity_after)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), e.HouseholdID, e.UserID, e.Username, e.Action, e.TargetType,
		e.TargetID, e.TargetName, e.QuantityBefore, e.QuantityAfter,
	)
	if err != nil {
		// Logged at warn (not error) because the user-facing action still
		// succeeded — losing an audit row is worth knowing about but
		// shouldn't trigger an oncall alert.
		slog.Warn("audit write failed",
			"err", err, "household", e.HouseholdID, "action", e.Action)
	}
}

// medicineAudit describes how a medicine write should appear in the log.
type medicineAudit struct {
	action         string
	quantityBefore *int
	quantityAfter  *int
}

// classifyMedicineChange returns the audit action for a write from before
// to after; before is nil for a first insert. A quantity-only change is
// medicine.quantity, any other edit medicine.update. ok is false when no
// user-visible field changed.
func classifyMedicineChange(before, after *models.Medicine) (medicineAudit, bool) {
	if before == nil {
		return medicineAudit{action: "medicine.create"}, true
	}
	wasDeleted, isDeleted := before.DeletedAt != nil, after.DeletedAt != nil
	switch {
	case !wasDeleted && isDeleted && after.DisposedAt != nil:
		return medicineAudit{action: "medicine.dispose"}, true
	case !wasDeleted && isDeleted:
		return medicineAudit{action: "medicine.delete"}, true
	case wasDeleted && !isDeleted:
		return medicineAudit{action: "medicine.restore"}, true
	case isDeleted:
		return medicineAudit{}, false
	}

	fieldsChanged := !sameEditableFields(before, after)
	qtyChanged := before.Quantity != after.Quantity
	if !fieldsChanged && !qtyChanged {
		return medicineAudit{}, false
	}
	a := medicineAudit{action: "medicine.update"}
	if !fieldsChanged {
		a.action = "medicine.quantity"
	}
	if qtyChanged {
		a.quantityBefore, a.quantityAfter = &before.Quantity, &after.Quantity
	}
	return a, true
}

// sameEditableFields compares every user-editable field except quantity.
// A nil ActiveIngredient on after means "not sent" and keeps the old value.
func sameEditableFields(a, b *models.Medicine) bool {
	return a.Name == b.Name &&
		a.Unit == b.Unit &&
		a.Category == b.Category &&
		(b.ActiveIngredient == nil || deref(a.ActiveIngredient) == *b.ActiveIngredient) &&
		deref(a.ExpirationDate) == deref(b.ExpirationDate) &&
		deref(a.OpenedAt) == deref(b.OpenedAt) &&
		equalIntPtr(a.DaysAfterOpening, b.DaysAfterOpening) &&
		a.StorageLocation == b.StorageLocation &&
		a.Barcode == b.Barcode &&
		a.Notes == b.Notes
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func equalIntPtr(a, b *int) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
