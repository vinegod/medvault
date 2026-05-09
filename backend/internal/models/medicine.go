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

// validateName trims the name in place and enforces length / content rules.
// Pulled out to keep the cognitive complexity of validateMedicineFields low.
func validateName(name string) error {
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
	return nil
}

// validateOpeningFields covers expiration_date / opened_at / days_after_opening
// together because they're conceptually one cluster (the "expiration tracking"
// inputs).
func validateOpeningFields(expirationDate, openedAt *string, daysAfterOpening *int) error {
	if expirationDate != nil {
		if _, err := time.Parse("2006-01-02", *expirationDate); err != nil {
			return errors.New("expiration_date must be a valid date in YYYY-MM-DD format")
		}
	}
	if openedAt != nil {
		if _, err := time.Parse("2006-01-02", *openedAt); err != nil {
			return errors.New("opened_at must be a valid date in YYYY-MM-DD format")
		}
	}
	if daysAfterOpening != nil {
		// 1..3650 covers "use within a day" up to ~10 years (implausible
		// but rejecting the realistic range is worse than the outlier).
		if *daysAfterOpening < 1 || *daysAfterOpening > 3650 {
			return errors.New("days_after_opening must be between 1 and 3650")
		}
	}
	return nil
}

// Medicine mirrors the medicines table. HouseholdID and CreatedBy are
// internal — they are never serialised to the client (json:"-").
//
// OpenedAt + DaysAfterOpening together produce an "effective expiration":
// min(ExpirationDate, OpenedAt + DaysAfterOpening). Both are optional;
// when nil, the printed ExpirationDate is the effective one. PhotoHash
// references a blob served out-of-band by the photos handler.
type Medicine struct {
	ID               string     `json:"id"`
	HouseholdID      string     `json:"-"`
	CreatedBy        string     `json:"-"`
	Name             string     `json:"name"`
	Quantity         int        `json:"quantity"`
	Unit             string     `json:"unit"`
	Category         string     `json:"category"`
	ExpirationDate   *string    `json:"expiration_date"`
	OpenedAt         *string    `json:"opened_at"`
	DaysAfterOpening *int       `json:"days_after_opening"`
	StorageLocation  string     `json:"storage_location"`
	Barcode          string     `json:"barcode"`
	Notes            string     `json:"notes"`
	PhotoHash        *string    `json:"photo_hash"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
	DeletedAt        *time.Time `json:"deleted_at"`
}

// Validate runs the same field rules as Create/Update on a full Medicine
// record. Used by the sync handler so a misbehaving client can't bypass
// validation by routing through /api/sync.
func (m *Medicine) Validate() error {
	return validateMedicineFields(
		m.Name, m.Quantity, m.Unit, m.Category,
		m.ExpirationDate, m.OpenedAt, m.DaysAfterOpening,
		m.StorageLocation, m.Barcode, m.Notes,
	)
}

type CreateMedicineRequest struct {
	Name             string  `json:"name"`
	Quantity         int     `json:"quantity"`
	Unit             string  `json:"unit"`
	Category         string  `json:"category"`
	ExpirationDate   *string `json:"expiration_date"`
	OpenedAt         *string `json:"opened_at"`
	DaysAfterOpening *int    `json:"days_after_opening"`
	StorageLocation  string  `json:"storage_location"`
	Barcode          string  `json:"barcode"`
	Notes            string  `json:"notes"`
}

func (r *CreateMedicineRequest) Validate() error {
	return validateMedicineFields(
		r.Name, r.Quantity, r.Unit, r.Category,
		r.ExpirationDate, r.OpenedAt, r.DaysAfterOpening,
		r.StorageLocation, r.Barcode, r.Notes,
	)
}

type UpdateMedicineRequest struct {
	Name             string  `json:"name"`
	Quantity         int     `json:"quantity"`
	Unit             string  `json:"unit"`
	Category         string  `json:"category"`
	ExpirationDate   *string `json:"expiration_date"`
	OpenedAt         *string `json:"opened_at"`
	DaysAfterOpening *int    `json:"days_after_opening"`
	StorageLocation  string  `json:"storage_location"`
	Barcode          string  `json:"barcode"`
	Notes            string  `json:"notes"`
}

func (r *UpdateMedicineRequest) Validate() error {
	return validateMedicineFields(
		r.Name, r.Quantity, r.Unit, r.Category,
		r.ExpirationDate, r.OpenedAt, r.DaysAfterOpening,
		r.StorageLocation, r.Barcode, r.Notes,
	)
}

func validateMedicineFields(
	name string, quantity int, unit, category string,
	expirationDate, openedAt *string, daysAfterOpening *int,
	storageLocation, barcode, notes string,
) error {
	if err := validateName(name); err != nil {
		return err
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
	if err := validateOpeningFields(expirationDate, openedAt, daysAfterOpening); err != nil {
		return err
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
