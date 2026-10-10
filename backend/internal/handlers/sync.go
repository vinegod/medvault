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

	// last_sync is the server_time echoed back from a previous response
	// (RFC 3339, seconds precision). RFC3339Nano also accepts fractional
	// seconds, so clients that reformat the value keep working.
	var since *time.Time
	if req.LastSync != nil {
		t, err := time.Parse(time.RFC3339Nano, *req.LastSync)
		if err != nil {
			api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "last_sync must be an RFC 3339 timestamp")
			return
		}
		t = t.UTC()
		since = &t
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

	// Capture server_time before the read so a write landing between the
	// query and the response is picked up by the next sync instead of
	// falling behind the cursor. RFC3339 truncates to whole seconds, which
	// only widens that safety margin (re-sent records are idempotent).
	serverTime := time.Now().UTC()

	records, err := h.queryRecords(r, householdID, since)
	if err != nil {
		api.ServerError(w, "sync: query records", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, syncResponse{
		Records:    records,
		ServerTime: serverTime.Format(time.RFC3339),
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

	normalizeTimestamps(c, now, clampCutoff)

	before, err := loadMedicine(ctx, tx, c.ID)
	if err != nil {
		return err
	}

	if before == nil && c.DeletedAt == nil {
		h.enrichFromDrugDB(ctx, c)
	}

	// Single-statement upsert with LWW built into the WHERE clause:
	// ON CONFLICT only updates when the client copy is newer AND the
	// existing row belongs to the same household (security guard).
	// A nil active_ingredient keeps the stored value.
	res, err := tx.ExecContext(ctx, `
		INSERT INTO medicines
		  (id, household_id, created_by, name, quantity, unit, category, active_ingredient,
		   expiration_date, opened_at, days_after_opening,
		   storage_location, barcode, notes,
		   created_at, updated_at, deleted_at, disposed_at, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
		  name               = excluded.name,
		  quantity           = excluded.quantity,
		  unit               = excluded.unit,
		  category           = excluded.category,
		  active_ingredient  = COALESCE(excluded.active_ingredient, medicines.active_ingredient),
		  expiration_date    = excluded.expiration_date,
		  opened_at          = excluded.opened_at,
		  days_after_opening = excluded.days_after_opening,
		  storage_location   = excluded.storage_location,
		  barcode            = excluded.barcode,
		  notes              = excluded.notes,
		  updated_at         = excluded.updated_at,
		  deleted_at         = excluded.deleted_at,
		  disposed_at        = excluded.disposed_at,
		  synced_at          = excluded.synced_at
		WHERE excluded.updated_at > medicines.updated_at
		  AND medicines.household_id = excluded.household_id`,
		c.ID, householdID, callerID,
		c.Name, c.Quantity, c.Unit, c.Category, c.ActiveIngredient,
		c.ExpirationDate, c.OpenedAt, c.DaysAfterOpening,
		c.StorageLocation, c.Barcode, c.Notes,
		c.CreatedAt, c.UpdatedAt, c.DeletedAt, c.DisposedAt, now,
	)
	if err != nil {
		return fmt.Errorf("upsert medicine %s: %w", c.ID, err)
	}
	applied, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("upsert medicine %s: rows affected: %w", c.ID, err)
	}
	if applied == 0 {
		return nil // lost LWW or belongs to another household
	}

	if change, ok := classifyMedicineChange(before, c); ok {
		e := medicineEvent(ctx, householdID, change.action, c)
		e.QuantityBefore, e.QuantityAfter = change.quantityBefore, change.quantityAfter
		WriteAudit(ctx, tx, e)
	}
	return nil
}

// normalizeTimestamps converts c's timestamps to UTC, clears DisposedAt on
// a live record, and clamps CreatedAt/UpdatedAt past clampCutoff to now.
func normalizeTimestamps(c *models.Medicine, now, clampCutoff time.Time) {
	// Stored as text and compared lexicographically, so every value needs
	// the same offset.
	c.UpdatedAt = c.UpdatedAt.UTC()
	c.CreatedAt = c.CreatedAt.UTC()
	if c.DeletedAt != nil {
		d := c.DeletedAt.UTC()
		c.DeletedAt = &d
	}
	if c.DeletedAt == nil {
		c.DisposedAt = nil
	} else if c.DisposedAt != nil {
		d := c.DisposedAt.UTC()
		c.DisposedAt = &d
	}

	// A wildly wrong device clock must not win every LWW round.
	if c.UpdatedAt.After(clampCutoff) {
		c.UpdatedAt = now
	}
	if c.CreatedAt.After(clampCutoff) {
		c.CreatedAt = now
	}
}

// loadMedicine returns the stored medicine with the given id in any
// household, or nil when the id is new.
func loadMedicine(ctx context.Context, tx *sql.Tx, id string) (*models.Medicine, error) {
	var m models.Medicine
	err := scanMedicine(tx.QueryRowContext(ctx, medicineSelectFrom+` WHERE id = ?`, id), &m)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load medicine %s: %w", id, err)
	}
	return &m, nil
}

// enrichFromDrugDB fills an empty active_ingredient (INN, else the
// ingredient list) and empty notes (pharma group) from an exact drug DB
// name match. Lookup errors are logged and ignored.
func (h *SyncHandler) enrichFromDrugDB(ctx context.Context, c *models.Medicine) {
	if h.DrugRegistry == nil || h.DrugCountry == "" {
		return
	}
	if deref(c.ActiveIngredient) != "" && c.Notes != "" {
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

	// Drug DB values can exceed our field limits. Keep the record valid,
	// otherwise the client gets it back and every later edit fails sync.
	origIngredient, origNotes := c.ActiveIngredient, c.Notes
	defer func() {
		if c.Validate() != nil {
			c.ActiveIngredient, c.Notes = origIngredient, origNotes
		}
	}()

	if deref(c.ActiveIngredient) == "" {
		switch {
		case d.INN != nil && *d.INN != "":
			c.ActiveIngredient = d.INN
		case d.Ingredients != nil:
			c.ActiveIngredient = d.Ingredients
		}
	}
	if c.Notes == "" && d.PharmaGroup != nil {
		c.Notes = *d.PharmaGroup
	}
}

// queryRecords fetches all household medicines written on the server
// after since. If since is nil the full set is returned (first sync).
//
// Filters on synced_at (server clock), not updated_at (client clock), so an
// offline edit uploaded late still reaches devices that synced in between.
func (h *SyncHandler) queryRecords(r *http.Request, householdID string, since *time.Time) ([]models.Medicine, error) {
	var (
		rows *sql.Rows
		err  error
	)
	if since == nil {
		rows, err = h.DB.QueryContext(r.Context(),
			medicineSelectFrom+` WHERE household_id = ?`,
			householdID,
		)
	} else {
		rows, err = h.DB.QueryContext(r.Context(),
			medicineSelectFrom+` WHERE household_id = ? AND synced_at > ?`,
			householdID, *since,
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
