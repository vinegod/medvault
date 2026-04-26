package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
		&m.ID, &m.Name, &m.Quantity, &m.Unit, &m.Category,
		&m.ExpirationDate, &m.StorageLocation, &m.Barcode, &m.Notes,
		&m.CreatedAt, &m.UpdatedAt, &m.DeletedAt,
	); err != nil {
		return fmt.Errorf("scan medicine: %w", err)
	}
	return nil
}

// medicineSelectFrom is the standard projection used by every read.
// Includes the FROM clause so callers just append a WHERE.
const medicineSelectFrom = `SELECT
	id, name, quantity, unit, category,
	expiration_date, storage_location, barcode, notes,
	created_at, updated_at, deleted_at
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
		slog.Error("List medicines: query", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}
	defer rows.Close() //nolint:errcheck // error surfaced by rows.Err()

	medicines := []models.Medicine{}
	for rows.Next() {
		var m models.Medicine
		if err := scanMedicine(rows, &m); err != nil {
			slog.Error("List medicines: scan", "err", err)
			api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
			return
		}
		medicines = append(medicines, m)
	}
	if err := rows.Err(); err != nil {
		slog.Error("List medicines: rows", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
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
		slog.Error("Get medicine: query", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	api.WriteJSON(w, http.StatusOK, m)
}

// Create handles POST /api/medicines.
func (h *MedicineHandler) Create(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())
	callerID := middleware.UserIDFrom(r.Context())

	var req models.CreateMedicineRequest
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
		 (id, household_id, created_by, name, quantity, unit, category,
		  expiration_date, storage_location, barcode, notes, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, householdID, callerID,
		req.Name, req.Quantity, req.Unit, req.Category,
		req.ExpirationDate, req.StorageLocation, req.Barcode, req.Notes,
		now, now,
	)
	if err != nil {
		slog.Error("Create medicine: insert", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	var m models.Medicine
	if err := scanMedicine(h.DB.QueryRowContext(r.Context(),
		medicineSelectFrom+` WHERE id = ?`, id,
	), &m); err != nil {
		slog.Error("medicine read-back after write", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	api.WriteJSON(w, http.StatusCreated, m)
}

// Update handles PUT /api/medicines/{id}.
// Any household member may update any medicine in their household.
func (h *MedicineHandler) Update(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())
	id := chi.URLParam(r, "id")

	// Confirm the medicine exists and belongs to this household before decoding
	// the body — gives a clean 404 rather than a silent no-op on a bad id.
	var exists int
	if err := h.DB.QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM medicines WHERE id = ? AND household_id = ? AND deleted_at IS NULL`,
		id, householdID,
	).Scan(&exists); err != nil {
		slog.Error("Update medicine: existence check", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}
	if exists == 0 {
		api.WriteError(w, http.StatusNotFound, models.CodeNotFound, "medicine not found")
		return
	}

	var req models.UpdateMedicineRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, "invalid JSON")
		return
	}
	if err := req.Validate(); err != nil {
		api.WriteError(w, http.StatusBadRequest, models.CodeValidation, err.Error())
		return
	}

	now := time.Now().UTC()
	_, err := h.DB.ExecContext(r.Context(),
		`UPDATE medicines
		 SET name=?, quantity=?, unit=?, category=?,
		     expiration_date=?, storage_location=?, barcode=?, notes=?,
		     updated_at=?
		 WHERE id = ? AND household_id = ?`,
		req.Name, req.Quantity, req.Unit, req.Category,
		req.ExpirationDate, req.StorageLocation, req.Barcode, req.Notes,
		now, id, householdID,
	)
	if err != nil {
		slog.Error("Update medicine: exec", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	var m models.Medicine
	if err := scanMedicine(h.DB.QueryRowContext(r.Context(),
		medicineSelectFrom+` WHERE id = ?`, id,
	), &m); err != nil {
		slog.Error("medicine read-back after write", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	api.WriteJSON(w, http.StatusOK, m)
}

// Delete handles DELETE /api/medicines/{id}.
// Soft-delete: sets deleted_at so the record survives for sync propagation.
func (h *MedicineHandler) Delete(w http.ResponseWriter, r *http.Request) {
	householdID := middleware.HouseholdIDFrom(r.Context())
	id := chi.URLParam(r, "id")

	res, err := h.DB.ExecContext(r.Context(),
		`UPDATE medicines SET deleted_at = ?, updated_at = ?
		 WHERE id = ? AND household_id = ? AND deleted_at IS NULL`,
		time.Now().UTC(), time.Now().UTC(), id, householdID,
	)
	if err != nil {
		slog.Error("Delete medicine: exec", "err", err)
		api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
		return
	}

	n, _ := res.RowsAffected()
	if n == 0 {
		api.WriteError(w, http.StatusNotFound, models.CodeNotFound, "medicine not found")
		return
	}

	api.WriteJSON(w, http.StatusOK, nil)
}
