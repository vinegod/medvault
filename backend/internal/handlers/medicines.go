package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/vinegod/medvault/backend/internal/api"
	"github.com/vinegod/medvault/backend/internal/middleware"
	"github.com/vinegod/medvault/backend/internal/models"
)

// MedicineHandler holds dependencies for all medicine endpoints.
type MedicineHandler struct {
	DB *sql.DB
}

// scanMedicine reads one medicine row into a models.Medicine.
// Column order must match every SELECT that uses this helper.
func scanMedicine(row interface {
	Scan(...any) error
}, m *models.Medicine,
) error {
	if err := row.Scan(
		&m.ID, &m.Name, &m.Quantity, &m.Unit, &m.Category, &m.ActiveIngredient,
		&m.ExpirationDate, &m.OpenedAt, &m.DaysAfterOpening,
		&m.StorageLocation, &m.Barcode, &m.Notes, &m.PhotoHash,
		&m.CreatedAt, &m.UpdatedAt, &m.DeletedAt, &m.DisposedAt,
	); err != nil {
		return fmt.Errorf("scan medicine: %w", err)
	}
	return nil
}

// medicineSelectFrom is the standard projection used by every read.
// Includes the FROM clause so callers just append a WHERE.
const medicineSelectFrom = `SELECT
	id, name, quantity, unit, category, active_ingredient,
	expiration_date, opened_at, days_after_opening,
	storage_location, barcode, notes, photo_hash,
	created_at, updated_at, deleted_at, disposed_at
	FROM medicines`

// List handles GET /api/medicines.
// Returns all non-deleted medicines for the caller's household.
func (h *MedicineHandler) List(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())

	rows, err := h.DB.QueryContext(r.Context(),
		medicineSelectFrom+` WHERE household_id = ? AND deleted_at IS NULL
		 ORDER BY name ASC`,
		householdID,
	)
	if err != nil {
		api.ServerError(w, "List medicines: query", err)
		return
	}
	defer rows.Close() //nolint:errcheck // error surfaced by rows.Err()

	medicines := []models.Medicine{}
	for rows.Next() {
		var m models.Medicine
		if err := scanMedicine(rows, &m); err != nil {
			api.ServerError(w, "List medicines: scan", err)
			return
		}
		medicines = append(medicines, m)
	}
	if err := rows.Err(); err != nil {
		api.ServerError(w, "List medicines: rows", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, medicines)
}

// Get handles GET /api/medicines/{id}.
func (h *MedicineHandler) Get(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())
	id := chi.URLParam(r, "id")

	var m models.Medicine
	err := scanMedicine(h.DB.QueryRowContext(r.Context(),
		medicineSelectFrom+` WHERE id = ? AND household_id = ? AND deleted_at IS NULL`,
		id, householdID,
	), &m)
	if errors.Is(err, sql.ErrNoRows) {
		api.WriteError(w, http.StatusNotFound, models.CodeNotFound, "medicine not found")
		return
	}
	if err != nil {
		api.ServerError(w, "Get medicine: query", err)
		return
	}

	api.WriteJSON(w, http.StatusOK, m)
}

// Create handles POST /api/medicines.
func (h *MedicineHandler) Create(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())
	callerID := middleware.UserIDFrom(r.Context())

	var req models.MedicineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "invalid JSON")
		return
	}
	if err := req.Validate(); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, err.Error())
		return
	}

	id := uuid.NewString()
	now := time.Now().UTC()

	_, err := h.DB.ExecContext(r.Context(),
		`INSERT INTO medicines
		 (id, household_id, created_by, name, quantity, unit, category, active_ingredient,
		  expiration_date, opened_at, days_after_opening,
		  storage_location, barcode, notes, created_at, updated_at, synced_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, householdID, callerID,
		req.Name, req.Quantity, req.Unit, req.Category, req.ActiveIngredient,
		req.ExpirationDate, req.OpenedAt, req.DaysAfterOpening,
		req.StorageLocation, req.Barcode, req.Notes,
		now, now, now,
	)
	if err != nil {
		api.ServerError(w, "Create medicine: insert", err)
		return
	}

	var m models.Medicine
	if err := scanMedicine(h.DB.QueryRowContext(r.Context(),
		medicineSelectFrom+` WHERE id = ?`, id,
	), &m); err != nil {
		api.ServerError(w, "medicine read-back after write", err)
		return
	}

	WriteAudit(r.Context(), h.DB, medicineEvent(r.Context(), householdID, "medicine.create", &m))

	api.WriteJSON(w, http.StatusCreated, m)
}

// Update handles PUT /api/medicines/{id}.
// Any household member may update any medicine in their household.
func (h *MedicineHandler) Update(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())
	id := chi.URLParam(r, "id")

	// Load the current row before decoding the body: a bad id gets a clean
	// 404, and the old values decide which audit action this edit is.
	var before models.Medicine
	err := scanMedicine(h.DB.QueryRowContext(r.Context(),
		medicineSelectFrom+` WHERE id = ? AND household_id = ? AND deleted_at IS NULL`,
		id, householdID,
	), &before)
	if errors.Is(err, sql.ErrNoRows) {
		api.WriteError(w, http.StatusNotFound, models.CodeNotFound, "medicine not found")
		return
	}
	if err != nil {
		api.ServerError(w, "Update medicine: load", err)
		return
	}

	var req models.MedicineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "invalid JSON")
		return
	}
	if err := req.Validate(); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, err.Error())
		return
	}

	now := time.Now().UTC()
	_, err = h.DB.ExecContext(r.Context(),
		`UPDATE medicines
		 SET name=?, quantity=?, unit=?, category=?,
		     active_ingredient=COALESCE(?, active_ingredient),
		     expiration_date=?, opened_at=?, days_after_opening=?,
		     storage_location=?, barcode=?, notes=?,
		     updated_at=?, synced_at=?
		 WHERE id = ? AND household_id = ?`,
		req.Name, req.Quantity, req.Unit, req.Category, req.ActiveIngredient,
		req.ExpirationDate, req.OpenedAt, req.DaysAfterOpening,
		req.StorageLocation, req.Barcode, req.Notes,
		now, now, id, householdID,
	)
	if err != nil {
		api.ServerError(w, "Update medicine: exec", err)
		return
	}

	var m models.Medicine
	if err := scanMedicine(h.DB.QueryRowContext(r.Context(),
		medicineSelectFrom+` WHERE id = ?`, id,
	), &m); err != nil {
		api.ServerError(w, "medicine read-back after write", err)
		return
	}

	if change, ok := classifyMedicineChange(&before, &m); ok {
		e := medicineEvent(r.Context(), householdID, change.action, &m)
		e.QuantityBefore, e.QuantityAfter = change.quantityBefore, change.quantityAfter
		WriteAudit(r.Context(), h.DB, e)
	}

	api.WriteJSON(w, http.StatusOK, m)
}

// Delete handles DELETE /api/medicines/{id}.
// Soft-delete: sets deleted_at so the record survives for sync propagation.
func (h *MedicineHandler) Delete(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())
	id := chi.URLParam(r, "id")

	// Capture the name first so the audit row can include it after the
	// soft-delete. SQLite supports RETURNING but mixing it with the
	// UPDATE complicates portability; one extra SELECT is fine here.
	var name string
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT name FROM medicines WHERE id = ? AND household_id = ? AND deleted_at IS NULL`,
		id, householdID,
	).Scan(&name); errors.Is(err, sql.ErrNoRows) {
		api.WriteError(w, http.StatusNotFound, models.CodeNotFound, "medicine not found")
		return
	} else if err != nil {
		api.ServerError(w, "Delete medicine: name lookup", err)
		return
	}

	now := time.Now().UTC()
	res, err := h.DB.ExecContext(r.Context(),
		`UPDATE medicines SET deleted_at = ?, updated_at = ?, synced_at = ?
		 WHERE id = ? AND household_id = ? AND deleted_at IS NULL`,
		now, now, now, id, householdID,
	)
	if err != nil {
		api.ServerError(w, "Delete medicine: exec", err)
		return
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		api.WriteError(w, http.StatusNotFound, models.CodeNotFound, "medicine not found")
		return
	}

	WriteAudit(r.Context(), h.DB, medicineEvent(r.Context(), householdID, "medicine.delete",
		&models.Medicine{ID: id, Name: name}))

	api.WriteJSON(w, http.StatusOK, nil)
}
