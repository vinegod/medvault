package models

import (
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	allowedUnits = map[string]bool{
		"tablets": true, "capsules": true, "ml": true, "mg": true,
		"g": true, "pieces": true, "drops": true, "sachets": true, "patches": true,
	}

	// Matches alphanumeric-only barcode strings.
	barcodeRE = regexp.MustCompile(`^[a-zA-Z0-9]*$`)
)

// validateName enforces length / content rules on an already-trimmed name.
// Pulled out to keep the cognitive complexity of validateMedicineFields low.
//
// Length limits here and below count characters, not bytes, to match the
// frontend's maxLength: a Cyrillic name takes two bytes per character.
func validateName(name string) error {
	if name == "" {
		return errors.New("name is required")
	}
	if utf8.RuneCountInString(name) > 200 {
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
// A nil ActiveIngredient on input means the client did not send it, and
// writers keep the stored value. DisposedAt is set only while DeletedAt is.
type Medicine struct {
	ID               string     `json:"id"`
	HouseholdID      string     `json:"-"`
	CreatedBy        string     `json:"-"`
	Name             string     `json:"name"`
	Quantity         int        `json:"quantity"`
	Unit             string     `json:"unit"`
	Category         string     `json:"category"`
	ActiveIngredient *string    `json:"active_ingredient"`
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
	DisposedAt       *time.Time `json:"disposed_at"`
}

// Validate runs the same field rules as Create/Update on a full Medicine
// record. Used by the sync handler so a misbehaving client can't bypass
// validation by routing through /api/sync.
func (m *Medicine) Validate() error {
	m.Name = strings.TrimSpace(m.Name)
	return (&medicineFields{
		name: m.Name, quantity: m.Quantity, unit: m.Unit, category: m.Category,
		activeIngredient: m.ActiveIngredient,
		expirationDate:   m.ExpirationDate, openedAt: m.OpenedAt, daysAfterOpening: m.DaysAfterOpening,
		storageLocation: m.StorageLocation, barcode: m.Barcode, notes: m.Notes,
	}).validate()
}

// MedicineRequest is the body of POST /api/medicines and PUT /api/medicines/{id}.
// A nil ActiveIngredient on update keeps the stored value.
type MedicineRequest struct {
	Name             string  `json:"name"`
	Quantity         int     `json:"quantity"`
	Unit             string  `json:"unit"`
	Category         string  `json:"category"`
	ActiveIngredient *string `json:"active_ingredient"`
	ExpirationDate   *string `json:"expiration_date"`
	OpenedAt         *string `json:"opened_at"`
	DaysAfterOpening *int    `json:"days_after_opening"`
	StorageLocation  string  `json:"storage_location"`
	Barcode          string  `json:"barcode"`
	Notes            string  `json:"notes"`
}

func (r *MedicineRequest) Validate() error {
	r.Name = strings.TrimSpace(r.Name)
	return (&medicineFields{
		name: r.Name, quantity: r.Quantity, unit: r.Unit, category: r.Category,
		activeIngredient: r.ActiveIngredient,
		expirationDate:   r.ExpirationDate, openedAt: r.OpenedAt, daysAfterOpening: r.DaysAfterOpening,
		storageLocation: r.StorageLocation, barcode: r.Barcode, notes: r.Notes,
	}).validate()
}

// medicineFields holds the user-editable medicine fields for validation.
type medicineFields struct {
	name             string
	quantity         int
	unit             string
	category         string
	activeIngredient *string
	expirationDate   *string
	openedAt         *string
	daysAfterOpening *int
	storageLocation  string
	barcode          string
	notes            string
}

func (f *medicineFields) validate() error {
	if err := validateName(f.name); err != nil {
		return err
	}
	if f.quantity < 0 || f.quantity > 99999 {
		return errors.New("quantity must be between 0 and 99999")
	}
	if !allowedUnits[f.unit] {
		return errors.New("unit must be one of: tablets, capsules, ml, mg, g, pieces, drops, sachets, patches")
	}
	if utf8.RuneCountInString(strings.TrimSpace(f.category)) > 50 {
		return errors.New("category must be 50 characters or fewer")
	}
	if err := validateActiveIngredient(f.activeIngredient); err != nil {
		return err
	}
	if err := validateOpeningFields(f.expirationDate, f.openedAt, f.daysAfterOpening); err != nil {
		return err
	}
	if utf8.RuneCountInString(f.storageLocation) > 100 {
		return errors.New("storage_location must be 100 characters or fewer")
	}
	if len(f.barcode) > 50 || !barcodeRE.MatchString(f.barcode) {
		return errors.New("barcode must be alphanumeric and at most 50 characters")
	}
	if utf8.RuneCountInString(f.notes) > 500 {
		return errors.New("notes must be 500 characters or fewer")
	}
	return nil
}

func validateActiveIngredient(ingredient *string) error {
	if ingredient == nil {
		return nil
	}
	if utf8.RuneCountInString(*ingredient) > 200 {
		return errors.New("active_ingredient must be 200 characters or fewer")
	}
	if strings.ContainsAny(*ingredient, "<>") {
		return errors.New("active_ingredient must not contain HTML tags")
	}
	return nil
}
