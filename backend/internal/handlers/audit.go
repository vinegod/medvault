package handlers

import (
	"context"
	"database/sql"
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

// auditEntry is one row in the response. Deliberately flat for easy
// rendering.
type auditEntry struct {
	ID         string    `json:"id"`
	Username   string    `json:"username"`
	Action     string    `json:"action"`
	TargetType string    `json:"target_type"`
	TargetID   *string   `json:"target_id"`
	TargetName *string   `json:"target_name"`
	At         time.Time `json:"at"`
}

// List handles GET /api/households/{id}/audit?limit=N. Owners and members
// alike can read the log; the household middleware already constrains
// access to members.
func (h *AuditHandler) List(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())
	paramID := chi.URLParam(r, "id")
	if paramID != householdID {
		api.WriteError(w, http.StatusForbidden, models.CodeForbidden, "not your household")
		return
	}

	limit := auditDefaultLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			if n > auditMaxLimit {
				n = auditMaxLimit
			}
			limit = n
		}
	}

	rows, err := h.DB.QueryContext(r.Context(),
		`SELECT id, username, action, target_type, target_id, target_name, at
		   FROM household_audit
		  WHERE household_id = ?
		  ORDER BY at DESC
		  LIMIT ?`,
		householdID, limit,
	)
	if err != nil {
		api.ServerError(w, "Audit list: query", err)
		return
	}
	defer rows.Close() //nolint:errcheck // error surfaced by rows.Err()

	out := []auditEntry{}
	for rows.Next() {
		var e auditEntry
		if err := rows.Scan(
			&e.ID, &e.Username, &e.Action, &e.TargetType,
			&e.TargetID, &e.TargetName, &e.At,
		); err != nil {
			api.ServerError(w, "Audit list: scan", err)
			return
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		api.ServerError(w, "Audit list: rows", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, out)
}

// WriteAudit appends one row to the household_audit table. Errors are
// logged but never returned: audit-log failure must not block the
// underlying mutation, since the user-visible action already succeeded.
//
// Pass the same *sql.Tx the calling handler is already using so the audit
// write commits or rolls back atomically with the mutation. When called
// outside a transaction, pass an *sql.DB.
type sqlExec interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

func WriteAudit(
	ctx context.Context,
	exec sqlExec,
	householdID, userID, username, action, targetType string,
	targetID, targetName *string,
) {
	if exec == nil {
		return
	}
	_, err := exec.ExecContext(ctx,
		`INSERT INTO household_audit
		   (id, household_id, user_id, username, action, target_type, target_id, target_name)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		uuid.NewString(), householdID, userID, username, action, targetType,
		targetID, targetName,
	)
	if err != nil {
		// Logged at warn (not error) because the user-facing action still
		// succeeded — losing an audit row is worth knowing about but
		// shouldn't trigger an oncall alert.
		slog.Warn("audit write failed",
			"err", err, "household", householdID, "action", action)
	}
}

// Compile-time check that *sql.DB and *sql.Tx both satisfy sqlExec.
var (
	_ sqlExec = (*sql.DB)(nil)
	_ sqlExec = (*sql.Tx)(nil)
)
