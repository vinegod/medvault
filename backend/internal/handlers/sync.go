package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/vinegod/medvault/backend/internal/api"
	"github.com/vinegod/medvault/backend/internal/database"
	"github.com/vinegod/medvault/backend/internal/middleware"
	"github.com/vinegod/medvault/backend/internal/models"
)

const (
	syncMaxBodyBytes = 1 << 20 // 1 MB
	syncMaxRecords   = 1000
	// Allow a small clock-skew window before clamping client timestamps to
	// server `now`. Anything beyond this is treated as a misconfigured or
	// malicious clock and gets the server time instead of the client's.
	syncClockSkew = 5 * time.Minute
)

// SyncHandler holds dependencies for the sync endpoint.
//
// DrugRegistry and DrugCountry are optional. When both are set, records
// inserted for the first time get their empty `category` and `notes` fields
// filled from the drug DB (see enrichOnFirstInsert).
type SyncHandler struct {
	DB           *sql.DB
	DrugRegistry *database.DrugRegistry
	DrugCountry  string
}

// syncRequest mirrors the frontend SyncRequest type.
type syncRequest struct {
	LastSync *string           `json:"last_sync"` // null = first sync
	Changes  []models.Medicine `json:"changes"`
}

// syncResponse mirrors the frontend SyncResponse type.
type syncResponse struct {
	Records    []models.Medicine `json:"records"`
	ServerTime string            `json:"server_time"`
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
		api.ServerError(w, "sync: begin tx", err)
		return
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit; error intentionally discarded

	now := time.Now().UTC()
	clampCutoff := now.Add(syncClockSkew)

	for i := range req.Changes {
		err := h.applyChange(
			r.Context(), tx, &req.Changes[i], i,
			householdID, callerID, now, clampCutoff,
		)
		if err == nil {
			continue
		}
		var se *syncError
		if errors.As(err, &se) {
			api.WriteError(w, se.status, se.code, se.message)
			return
		}
		// Inline pattern (rather than api.ServerError) so the per-record
		// id is in the log line — useful when one of N changes fails.
		slog.Error("sync: upsert record", "id", req.Changes[i].ID, "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	if err := tx.Commit(); err != nil {
		api.ServerError(w, "sync: commit", err)
		return
	}

	records, err := h.queryRecords(r, householdID, req.LastSync)
	if err != nil {
		api.ServerError(w, "sync: query records", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, syncResponse{
		Records:    records,
		ServerTime: time.Now().UTC().Format(time.RFC3339),
	})
}

// syncError is returned by applyChange when the per-change failure is a
// client problem (validation, missing id) rather than an internal error.
// Sync handler maps it to a 4xx response; anything else becomes a 500.
type syncError struct {
	status  int
	code    string
	message string
}

func (e *syncError) Error() string { return e.message }

// applyChange validates one inbound record, clamps client timestamps,
// optionally enriches it from the drug DB, and upserts it under LWW rules.
// Pulled out of Sync to keep that handler under the cognitive-complexity cap.
func (h *SyncHandler) applyChange(
	ctx context.Context,
	tx *sql.Tx,
	c *models.Medicine,
	idx int,
	householdID, callerID string,
	now, clampCutoff time.Time,
) error {
	// Run the same validation Create/Update enforce so a buggy or malicious
	// client can't slip oversized notes, HTML in names, invalid units, etc.
	// through sync. Soft-deleted records are exempt: a tombstone for a
	// record whose original fields no longer validate is still legitimate.
	if c.DeletedAt == nil {
		if err := c.Validate(); err != nil {
			return &syncError{
				status:  http.StatusBadRequest,
				code:    models.CodeValidation,
				message: fmt.Sprintf("change %d (id=%s): %s", idx, c.ID, err.Error()),
			}
		}
	}

	// Reject empty ids explicitly. Generating one server-side would produce
	// a record the client can never reference, leading to duplicate inserts
	// on every retry.
	if c.ID == "" {
		return &syncError{
			status:  http.StatusBadRequest,
			code:    models.CodeValidation,
			message: fmt.Sprintf("change %d: id is required", idx),
		}
	}

	// Clamp client-supplied timestamps to (now + skew) so a wildly wrong
	// device clock can't lock records by always winning LWW. Records with
	// sane clocks are unaffected.
	if c.UpdatedAt.After(clampCutoff) {
		c.UpdatedAt = now
	}
	if c.CreatedAt.After(clampCutoff) {
		c.CreatedAt = now
	}

	// First-insert enrichment: when the id is new and the configured drug DB
	// has an exact name match, fill empty `category` and `notes` from it.
	// Subsequent edits skip this path because the id will then exist.
	if c.DeletedAt == nil {
		h.enrichOnFirstInsert(ctx, tx, c)
	}

	// Single-statement upsert with LWW built into the WHERE clause:
	// ON CONFLICT only updates when the client copy is newer AND the
	// existing row belongs to the same household (security guard).
	_, err := tx.ExecContext(ctx, `
		INSERT INTO medicines
		  (id, household_id, created_by, name, quantity, unit, category,
		   expiration_date, opened_at, days_after_opening,
		   storage_location, barcode, notes,
		   created_at, updated_at, deleted_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		  name               = excluded.name,
		  quantity           = excluded.quantity,
		  unit               = excluded.unit,
		  category           = excluded.category,
		  expiration_date    = excluded.expiration_date,
		  opened_at          = excluded.opened_at,
		  days_after_opening = excluded.days_after_opening,
		  storage_location   = excluded.storage_location,
		  barcode            = excluded.barcode,
		  notes              = excluded.notes,
		  updated_at         = excluded.updated_at,
		  deleted_at         = excluded.deleted_at
		WHERE excluded.updated_at > medicines.updated_at
		  AND medicines.household_id = excluded.household_id`,
		c.ID, householdID, callerID,
		c.Name, c.Quantity, c.Unit, c.Category,
		c.ExpirationDate, c.OpenedAt, c.DaysAfterOpening,
		c.StorageLocation, c.Barcode, c.Notes,
		c.CreatedAt, c.UpdatedAt, c.DeletedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert medicine %s: %w", c.ID, err)
	}
	return nil
}

// enrichOnFirstInsert fills empty `category` and `notes` from the drug DB
// when the medicine `id` does not already exist server-side. No-op when the
// drug registry isn't configured, when both fields are already populated, or
// when the drug DB has no exact-name match. Errors are logged and swallowed:
// enrichment is best-effort and must never block a sync.
func (h *SyncHandler) enrichOnFirstInsert(ctx context.Context, tx *sql.Tx, c *models.Medicine) {
	if h.DrugRegistry == nil || h.DrugCountry == "" {
		return
	}
	if c.Category != "" && c.Notes != "" {
		return
	}

	var exists int
	err := tx.QueryRowContext(ctx, `SELECT 1 FROM medicines WHERE id = ?`, c.ID).Scan(&exists)
	if err == nil {
		return // id already exists — not a first insert
	}
	if !errors.Is(err, sql.ErrNoRows) {
		slog.Warn("sync enrich: id existence check", "id", c.ID, "err", err)
		return
	}

	d, err := LookupExact(ctx, h.DrugRegistry, h.DrugCountry, c.Name)
	if err != nil {
		slog.Warn("sync enrich: drug lookup", "name", c.Name, "err", err)
		return
	}
	if d == nil {
		return
	}

	if c.Category == "" && d.Ingredients != nil {
		c.Category = *d.Ingredients
	}
	if c.Notes == "" {
		var inn, pg string
		if d.INN != nil {
			inn = *d.INN
		}
		if d.PharmaGroup != nil {
			pg = *d.PharmaGroup
		}
		if joined := database.JoinDrugNotes(inn, pg); joined != "" {
			c.Notes = joined
		}
	}
}

// queryRecords fetches all household medicines updated since lastSync.
// If lastSync is nil the full set is returned (first sync).
func (h *SyncHandler) queryRecords(r *http.Request, householdID string, lastSync *string) ([]models.Medicine, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if lastSync == nil {
		rows, err = h.DB.QueryContext(r.Context(),
			medicineSelectFrom+` WHERE household_id = ?`,
			householdID,
		)
	} else {
		rows, err = h.DB.QueryContext(r.Context(),
			medicineSelectFrom+` WHERE household_id = ? AND updated_at > ?`,
			householdID, *lastSync,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("query household records: %w", err)
	}
	defer rows.Close() //nolint:errcheck // error surfaced by rows.Err()

	records := []models.Medicine{}
	for rows.Next() {
		var m models.Medicine
		if err := scanMedicine(rows, &m); err != nil {
			return nil, err
		}
		records = append(records, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate household records: %w", err)
	}
	return records, nil
}
