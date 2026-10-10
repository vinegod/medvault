package handlers

import (
	"net/http"
	"testing"
	"time"

	"github.com/vinegod/medvault/backend/internal/models"
)

func TestClassifyMedicineChange(t *testing.T) {
	now := time.Now()
	ibuprofen := "ibuprofen"
	base := func() *models.Medicine {
		return &models.Medicine{Name: "Nurofen", Quantity: 10, Unit: "tablets", ActiveIngredient: &ibuprofen}
	}
	with := func(f func(m *models.Medicine)) *models.Medicine {
		m := base()
		f(m)
		return m
	}
	deleted := with(func(m *models.Medicine) { m.DeletedAt = &now })

	tests := []struct {
		name          string
		before, after *models.Medicine
		want          string
		wantOK        bool
	}{
		{"insert", nil, base(), "medicine.create", true},
		{"no change", base(), base(), "", false},
		{"ingredient omitted", base(), with(func(m *models.Medicine) { m.ActiveIngredient = nil }), "", false},
		{"quantity only", base(), with(func(m *models.Medicine) { m.Quantity = 7 }), "medicine.quantity", true},
		{"other field", base(), with(func(m *models.Medicine) { m.Notes = "after meals" }), "medicine.update", true},
		{"delete", base(), deleted, "medicine.delete", true},
		{
			"dispose",
			base(),
			with(func(m *models.Medicine) { m.DeletedAt, m.DisposedAt = &now, &now }),
			"medicine.dispose",
			true,
		},
		{"restore", deleted, base(), "medicine.restore", true},
		{
			"edit while deleted",
			deleted,
			with(func(m *models.Medicine) { m.DeletedAt, m.Quantity = &now, 1 }),
			"",
			false,
		},
	}
	for _, tt := range tests {
		got, ok := classifyMedicineChange(tt.before, tt.after)
		if got.action != tt.want || ok != tt.wantOK {
			t.Errorf(
				"%s: classifyMedicineChange = (%q, %v), want (%q, %v)",
				tt.name,
				got.action,
				ok,
				tt.want,
				tt.wantOK,
			)
		}
	}
}

func TestClassifyMedicineChange_UpdateCarriesQuantities(t *testing.T) {
	before := &models.Medicine{Name: "A", Quantity: 10}
	after := &models.Medicine{Name: "B", Quantity: 4}

	got, _ := classifyMedicineChange(before, after)
	if got.action != "medicine.update" || *got.quantityBefore != 10 || *got.quantityAfter != 4 {
		t.Errorf("rename + quantity = %+v, want medicine.update 10 -> 4", got)
	}
}

func TestRegistrationBlocked(t *testing.T) {
	tests := []struct {
		mode       string
		firstUser  bool
		hasInvite  bool
		wantStatus int
	}{
		{RegistrationOpen, false, false, 0},
		{"", false, false, 0},
		{RegistrationInvite, false, false, http.StatusBadRequest},
		{RegistrationInvite, false, true, 0},
		{RegistrationClosed, false, true, http.StatusForbidden},
		{RegistrationClosed, true, false, 0},
		{RegistrationInvite, true, false, 0},
	}
	for _, tt := range tests {
		status, _, _, blocked := registrationBlocked(tt.mode, tt.firstUser, tt.hasInvite)
		if status != tt.wantStatus || blocked != (tt.wantStatus != 0) {
			t.Errorf("registrationBlocked(%q, first=%v, invite=%v) = %d, want %d",
				tt.mode, tt.firstUser, tt.hasInvite, status, tt.wantStatus)
		}
	}
}
