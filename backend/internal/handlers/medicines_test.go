package handlers_test

// Happy- and unhappy-path coverage for /api/medicines CRUD. Household
// scoping (the security boundary) gets its own test so a regression that
// leaks data across households fails loudly.

import (
	"net/http"
	"testing"
)

func TestMedicines_CreateAndList(t *testing.T) {
	h := newHarness(t)
	tok, _ := h.register("alice", "alicepass123")

	if got := listMedicines(t, h, tok); len(got) != 0 {
		t.Fatalf("initial list = %d, want 0", len(got))
	}

	status, med, errEnv := h.do("POST", "/api/medicines", tok, map[string]any{
		"name": "Aspirin", "quantity": 30, "unit": "tablets",
		"expiration_date": "2027-12-31",
	})
	if status != http.StatusCreated {
		t.Fatalf("create status=%d err=%v", status, errEnv)
	}
	if med["id"] == "" || med["id"] == nil {
		t.Errorf("created medicine missing id: %v", med)
	}
	if med["name"] != "Aspirin" {
		t.Errorf("name = %v, want Aspirin", med["name"])
	}

	got := listMedicines(t, h, tok)
	if len(got) != 1 || got[0]["id"] != med["id"] {
		t.Fatalf("after create, list = %v", got)
	}
}

func TestMedicines_Create_RejectsInvalid(t *testing.T) {
	h := newHarness(t)
	tok, _ := h.register("alice", "alicepass123")

	// Missing name.
	status, _, _ := h.do("POST", "/api/medicines", tok, map[string]any{
		"quantity": 5, "unit": "tablets",
	})
	if status != http.StatusBadRequest {
		t.Errorf("missing name: status=%d, want 400", status)
	}

	// Bogus unit.
	status, _, _ = h.do("POST", "/api/medicines", tok, map[string]any{
		"name": "X", "quantity": 5, "unit": "BOGUS",
	})
	if status != http.StatusBadRequest {
		t.Errorf("bad unit: status=%d, want 400", status)
	}

	// HTML in name (XSS guard).
	status, _, _ = h.do("POST", "/api/medicines", tok, map[string]any{
		"name": "<script>alert(1)</script>", "quantity": 1, "unit": "tablets",
	})
	if status != http.StatusBadRequest {
		t.Errorf("HTML name: status=%d, want 400", status)
	}
}

func TestMedicines_Get_HappyAnd404(t *testing.T) {
	h := newHarness(t)
	tok, _ := h.register("alice", "alicepass123")

	_, med, _ := h.do("POST", "/api/medicines", tok, map[string]any{
		"name": "Ibuprofen", "quantity": 12, "unit": "tablets",
	})
	id := med["id"].(string)

	status, got, _ := h.do("GET", "/api/medicines/"+id, tok, nil)
	if status != http.StatusOK {
		t.Fatalf("get status=%d", status)
	}
	if got["name"] != "Ibuprofen" {
		t.Errorf("name = %v, want Ibuprofen", got["name"])
	}

	status, _, errEnv := h.do("GET", "/api/medicines/00000000-0000-0000-0000-000000000000", tok, nil)
	if status != http.StatusNotFound {
		t.Errorf("get unknown: status=%d, want 404 (err=%v)", status, errEnv)
	}
}

func TestMedicines_Update_HappyAnd404(t *testing.T) {
	h := newHarness(t)
	tok, _ := h.register("alice", "alicepass123")

	_, med, _ := h.do("POST", "/api/medicines", tok, map[string]any{
		"name": "Aspirin", "quantity": 10, "unit": "tablets",
	})
	id := med["id"].(string)

	status, updated, errEnv := h.do("PUT", "/api/medicines/"+id, tok, map[string]any{
		"name": "Aspirin", "quantity": 99, "unit": "tablets",
	})
	if status != http.StatusOK {
		t.Fatalf("update status=%d err=%v", status, errEnv)
	}
	if q, _ := updated["quantity"].(float64); q != 99 {
		t.Errorf("quantity = %v, want 99", updated["quantity"])
	}

	status, _, _ = h.do("PUT", "/api/medicines/00000000-0000-0000-0000-000000000000", tok, map[string]any{
		"name": "X", "quantity": 1, "unit": "tablets",
	})
	if status != http.StatusNotFound {
		t.Errorf("update unknown: status=%d, want 404", status)
	}
}

func TestMedicines_Delete_SoftDeleteAnd404(t *testing.T) {
	h := newHarness(t)
	tok, _ := h.register("alice", "alicepass123")

	_, med, _ := h.do("POST", "/api/medicines", tok, map[string]any{
		"name": "Ibuprofen", "quantity": 5, "unit": "tablets",
	})
	id := med["id"].(string)

	status, _, _ := h.do("DELETE", "/api/medicines/"+id, tok, nil)
	if status != http.StatusOK {
		t.Fatalf("delete status=%d", status)
	}

	if got := listMedicines(t, h, tok); len(got) != 0 {
		t.Errorf("after delete, list len = %d, want 0", len(got))
	}

	// Re-deleting the same id is a 404 because the soft-delete predicate
	// (deleted_at IS NULL) excludes it.
	status, _, _ = h.do("DELETE", "/api/medicines/"+id, tok, nil)
	if status != http.StatusNotFound {
		t.Errorf("re-delete: status=%d, want 404", status)
	}
}

// Household scoping is the only access-control boundary — make sure
// alice can't read, update, or delete bob's medicine even when she
// guesses its id.
func TestMedicines_HouseholdScoping(t *testing.T) {
	h := newHarness(t)
	aliceTok, _ := h.register("alice", "alicepass123")
	bobTok, _ := h.register("bob", "bobpass1234")

	_, bobMed, _ := h.do("POST", "/api/medicines", bobTok, map[string]any{
		"name": "Bob's pills", "quantity": 1, "unit": "tablets",
	})
	id := bobMed["id"].(string)

	// Alice can't see it in her list.
	if got := listMedicines(t, h, aliceTok); len(got) != 0 {
		t.Errorf("alice sees bob's medicine in list: %v", got)
	}
	// Alice can't get it.
	if status, _, _ := h.do("GET", "/api/medicines/"+id, aliceTok, nil); status != http.StatusNotFound {
		t.Errorf("alice GET bob's medicine: status=%d, want 404", status)
	}
	// Alice can't update it.
	status, _, _ := h.do("PUT", "/api/medicines/"+id, aliceTok, map[string]any{
		"name": "stolen", "quantity": 0, "unit": "tablets",
	})
	if status != http.StatusNotFound {
		t.Errorf("alice PUT bob's medicine: status=%d, want 404", status)
	}
	// Alice can't delete it.
	if status, _, _ := h.do("DELETE", "/api/medicines/"+id, aliceTok, nil); status != http.StatusNotFound {
		t.Errorf("alice DELETE bob's medicine: status=%d, want 404", status)
	}
	// Bob still sees his medicine intact.
	if got := listMedicines(t, h, bobTok); len(got) != 1 || got[0]["name"] != "Bob's pills" {
		t.Errorf("bob's medicine corrupted by alice's attempts: %v", got)
	}
}
