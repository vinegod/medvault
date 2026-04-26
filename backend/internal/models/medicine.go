package models

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

var (
	allowedUnits = map[string]bool{
		"tablets": true, "capsules": true, "ml": true, "mg": true,
		"g": true, "pieces": true, "drops": true, "sachets": true, "patches": true,
	}

	// Matches alphanumeric-only barcode strings.
	barcodeRE = regexp.MustCompile(`^[a-zA-Z0-9]*$`)
)

// Medicine mirrors the medicines table. HouseholdID and CreatedBy are
// internal — they are never serialised to the client (json:"-").
type Medicine struct {
	ID              string     `json:"id"`
	HouseholdID     string     `json:"-"`
	CreatedBy       string     `json:"-"`
	Name            string     `json:"name"`
	Quantity        int        `json:"quantity"`
	Unit            string     `json:"unit"`
	Category        string     `json:"category"`
	ExpirationDate  *string    `json:"expiration_date"`
	StorageLocation string     `json:"storage_location"`
	Barcode         string     `json:"barcode"`
	Notes           string     `json:"notes"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
	DeletedAt       *time.Time `json:"deleted_at"`
}

// Validate runs the same field rules as Create/Update on a full Medicine
// record. Used by the sync handler so a misbehaving client can't bypass
// validation by routing through /api/sync.
func (m *Medicine) Validate() error {
	return validateMedicineFields(
		m.Name, m.Quantity, m.Unit, m.Category,
		m.ExpirationDate, m.StorageLocation, m.Barcode, m.Notes,
	)
}

type CreateMedicineRequest struct {
	Name            string  `json:"name"`
	Quantity        int     `json:"quantity"`
	Unit            string  `json:"unit"`
	Category        string  `json:"category"`
	ExpirationDate  *string `json:"expiration_date"`
	StorageLocation string  `json:"storage_location"`
	Barcode         string  `json:"barcode"`
	Notes           string  `json:"notes"`
}

func (r *CreateMedicineRequest) Validate() error {
	return validateMedicineFields(
		r.Name, r.Quantity, r.Unit, r.Category,
		r.ExpirationDate, r.StorageLocation, r.Barcode, r.Notes,
	)
}

type UpdateMedicineRequest struct {
	Name            string  `json:"name"`
	Quantity        int     `json:"quantity"`
	Unit            string  `json:"unit"`
	Category        string  `json:"category"`
	ExpirationDate  *string `json:"expiration_date"`
	StorageLocation string  `json:"storage_location"`
	Barcode         string  `json:"barcode"`
	Notes           string  `json:"notes"`
}

func (r *UpdateMedicineRequest) Validate() error {
	return validateMedicineFields(
		r.Name, r.Quantity, r.Unit, r.Category,
		r.ExpirationDate, r.StorageLocation, r.Barcode, r.Notes,
	)
}

func validateMedicineFields(
	name string, quantity int, unit, category string,
	expirationDate *string, storageLocation, barcode, notes string,
) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("name is required")
	}
	if len(name) > 200 {
		return errors.New("name must be 200 characters or fewer")
	}
	if strings.ContainsAny(name, "<>") {
		return errors.New("name must not contain HTML tags")
	}

	if quantity < 0 || quantity > 99999 {
		return errors.New("quantity must be between 0 and 99999")
	}

	if !allowedUnits[unit] {
		return errors.New("unit must be one of: tablets, capsules, ml, mg, g, pieces, drops, sachets, patches")
	}

	if len(strings.TrimSpace(category)) > 50 {
		return errors.New("category must be 50 characters or fewer")
	}

	if expirationDate != nil {
		_, err := time.Parse("2006-01-02", *expirationDate)
		if err != nil {
			return errors.New("expiration_date must be a valid date in YYYY-MM-DD format")
		}
	}

	if len(storageLocation) > 100 {
		return errors.New("storage_location must be 100 characters or fewer")
	}

	if len(barcode) > 50 || !barcodeRE.MatchString(barcode) {
		return errors.New("barcode must be alphanumeric and at most 50 characters")
	}

	if len(notes) > 500 {
		return errors.New("notes must be 500 characters or fewer")
	}

	return nil
}
