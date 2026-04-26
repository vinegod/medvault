package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/vinegod/medvault/backend/internal/api"
	"github.com/vinegod/medvault/backend/internal/middleware"
	"github.com/vinegod/medvault/backend/internal/models"
)

const (
	syncMaxBodyBytes = 1 << 20 // 1 MB
	syncMaxRecords   = 1000
)

// SyncHandler holds dependencies for the sync endpoint.
type SyncHandler struct {
	DB *sql.DB
}

// syncRequest mirrors the frontend SyncRequest type.
type syncRequest struct {
	LastSync *string          `json:"last_sync"` // null = first sync
	Changes  []models.Medicine `json:"changes"`
}

// syncResponse mirrors the frontend SyncResponse type.
type syncResponse struct {
	Records    []models.Medicine `json:"records"`
	ServerTime string           `json:"server_time"`
}

// Sync handles POST /api/sync.
//
// Protocol (last-write-wins per record):
//  1. Apply client changes: INSERT new records; UPDATE server records only
//     when the client copy is newer. Wrong-household ids are silently skipped.
//  2. Return all household records updated since last_sync (including
//     soft-deleted ones so deletions propagate to other devices).
func (h *SyncHandler) Sync(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())
	callerID := middleware.UserIDFrom(r.Context())

	// Cap body size before decoding to guard against giant payloads.
	r.Body = http.MaxBytesReader(w, r.Body, syncMaxBodyBytes)

	var req syncRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "invalid JSON or body too large")
		return
	}
	if len(req.Changes) > syncMaxRecords {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "too many records in one request (max 1000)")
		return
	}

	tx, err := h.DB.BeginTx(r.Context(), nil)
	if err != nil {
		slog.Error("sync: begin tx", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}
	defer tx.Rollback() //nolint:errcheck

	for i, c := range req.Changes {
		// Run the same validation Create/Update enforce so a buggy or
		// malicious client can't slip oversized notes, HTML in names,
		// invalid units, malformed dates, etc. through the sync path.
		// Soft-deleted records are exempt: a client may sync a tombstone
		// for a record whose original fields would no longer validate.
		if c.DeletedAt == nil {
			if err := c.Validate(); err != nil {
				api.WriteError(w, http.StatusBadRequest, models.CodeValidation,
					fmt.Sprintf("change %d (id=%s): %s", i, c.ID, err.Error()))
				return
			}
		}

		if c.ID == "" {
			c.ID = uuid.NewString() // client should always send an id; generate one as fallback
		}

		// Single-statement upsert with LWW built into the WHERE clause.
		//
		// ON CONFLICT fires when a row with this `id` already exists.
		// The DO UPDATE only applies when:
		//   - the client timestamp is strictly newer (LWW)
		//   - the existing row belongs to the SAME household (security guard)
		//
		// If the server copy is newer, or the id belongs to a different
		// household, the entire ON CONFLICT branch is a no-op.
		_, err := tx.ExecContext(r.Context(), `
			INSERT INTO medicines
			  (id, household_id, created_by, name, quantity, unit, category,
			   expiration_date, storage_location, barcode, notes,
			   created_at, updated_at, deleted_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET
			  name             = excluded.name,
			  quantity         = excluded.quantity,
			  unit             = excluded.unit,
			  category         = excluded.category,
			  expiration_date  = excluded.expiration_date,
			  storage_location = excluded.storage_location,
			  barcode          = excluded.barcode,
			  notes            = excluded.notes,
			  updated_at       = excluded.updated_at,
			  deleted_at       = excluded.deleted_at
			WHERE excluded.updated_at > medicines.updated_at
			  AND medicines.household_id = excluded.household_id`,
			c.ID, householdID, callerID,
			c.Name, c.Quantity, c.Unit, c.Category,
			c.ExpirationDate, c.StorageLocation, c.Barcode, c.Notes,
			c.CreatedAt, c.UpdatedAt, c.DeletedAt,
		)
		if err != nil {
			slog.Error("sync: upsert record", "id", c.ID, "err", err)
			api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		slog.Error("sync: commit", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	// Build the response: all records the client doesn't have yet.
	// last_sync == nil means the client wants everything.
	var (
		rows *sql.Rows
		qErr error
	)
	if req.LastSync == nil {
		rows, qErr = h.DB.QueryContext(r.Context(),
			medicineSelectFrom+` WHERE household_id = ?`,
			householdID,
		)
	} else {
		rows, qErr = h.DB.QueryContext(r.Context(),
			medicineSelectFrom+` WHERE household_id = ? AND updated_at > ?`,
			householdID, *req.LastSync,
		)
	}
	if qErr != nil {
		slog.Error("sync: query records", "err", qErr)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}
	defer rows.Close()

	records := []models.Medicine{}
	for rows.Next() {
		var m models.Medicine
		if err := scanMedicine(rows, &m); err != nil {
			slog.Error("sync: scan record", "err", err)
			api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
			return
		}
		records = append(records, m)
	}
	if err := rows.Err(); err != nil {
		slog.Error("sync: rows error", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	api.WriteJSON(w, http.StatusOK, syncResponse{
		Records:    records,
		ServerTime: time.Now().UTC().Format(time.RFC3339),
	})
}
