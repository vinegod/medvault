package handlers_test

// Sync handler validates every record now (H1 fix). These tests lock that
// behavior in: a malformed change is rejected with 400, the DB is unchanged,
// and a soft-delete tombstone is exempt (so a client can sync a deletion
// for a record whose old fields would no longer validate).

import (
	"net/http"
	"testing"
)

func TestSync_RejectsInvalidName_HTML(t *testing.T) {
	h := newHarness(t)
	tok, _ := h.register("alice", "alicepass123")

	status, _, errEnv := h.do("POST", "/api/sync", tok, map[string]any{
		"last_sync": nil,
		"changes": []map[string]any{{
			"id":               "11111111-1111-1111-1111-111111111111",
			"name":             "<script>alert(1)</script>",
			"quantity":         5,
			"unit":             "tablets",
			"category":         "",
			"expiration_date":  nil,
			"storage_location": "",
			"barcode":          "",
			"notes":            "",
			"created_at":       "2026-04-26T00:00:00Z",
			"updated_at":       "2026-04-26T00:00:00Z",
			"deleted_at":       nil,
		}},
	})
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400; err=%v", status, errEnv)
	}

	// And the medicine must NOT have been inserted.
	if got := listMedicines(t, h, tok); len(got) != 0 {
		t.Errorf("got %d medicines after rejected sync, want 0", len(got))
	}
}

func TestSync_RejectsInvalidUnit(t *testing.T) {
	h := newHarness(t)
	tok, _ := h.register("alice", "alicepass123")

	status, _, _ := h.do("POST", "/api/sync", tok, map[string]any{
		"last_sync": nil,
		"changes": []map[string]any{{
			"id":               "22222222-2222-2222-2222-222222222222",
			"name":             "Aspirin",
			"quantity":         5,
			"unit":             "BOGUS",
			"category":         "",
			"expiration_date":  nil,
			"storage_location": "",
			"barcode":          "",
			"notes":            "",
			"created_at":       "2026-04-26T00:00:00Z",
			"updated_at":       "2026-04-26T00:00:00Z",
			"deleted_at":       nil,
		}},
	})
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for invalid unit", status)
	}
}

func TestSync_RejectsInvalidExpirationDate(t *testing.T) {
	h := newHarness(t)
	tok, _ := h.register("alice", "alicepass123")

	bad := "not-a-date"
	status, _, _ := h.do("POST", "/api/sync", tok, map[string]any{
		"last_sync": nil,
		"changes": []map[string]any{{
			"id":               "33333333-3333-3333-3333-333333333333",
			"name":             "Aspirin",
			"quantity":         5,
			"unit":             "tablets",
			"category":         "",
			"expiration_date":  bad,
			"storage_location": "",
			"barcode":          "",
			"notes":            "",
			"created_at":       "2026-04-26T00:00:00Z",
			"updated_at":       "2026-04-26T00:00:00Z",
			"deleted_at":       nil,
		}},
	})
	if status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for invalid expiration_date", status)
	}
}

// A valid change still works — make sure we didn't over-reject.
func TestSync_AcceptsValidRecord(t *testing.T) {
	h := newHarness(t)
	tok, _ := h.register("alice", "alicepass123")

	status, _, errEnv := h.do("POST", "/api/sync", tok, map[string]any{
		"last_sync": nil,
		"changes": []map[string]any{{
			"id":               "44444444-4444-4444-4444-444444444444",
			"name":             "Aspirin",
			"quantity":         20,
			"unit":             "tablets",
			"category":         "",
			"expiration_date":  "2027-12-31",
			"storage_location": "",
			"barcode":          "",
			"notes":            "",
			"created_at":       "2026-04-26T00:00:00Z",
			"updated_at":       "2026-04-26T00:00:00Z",
			"deleted_at":       nil,
		}},
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200; err=%v", status, errEnv)
	}
	if got := listMedicines(t, h, tok); len(got) != 1 {
		t.Errorf("got %d medicines after valid sync, want 1", len(got))
	}
}

// A tombstone (soft delete) skips validation: a client should be able to
// propagate the deletion of a record whose old data wouldn't pass today's
// rules — otherwise deletes get stuck forever after a validation rule
// tightens.
func TestSync_AcceptsTombstoneSkippingValidation(t *testing.T) {
	h := newHarness(t)
	tok, _ := h.register("alice", "alicepass123")

	deletedAt := "2026-04-26T00:00:00Z"
	status, _, errEnv := h.do("POST", "/api/sync", tok, map[string]any{
		"last_sync": nil,
		"changes": []map[string]any{{
			"id":               "55555555-5555-5555-5555-555555555555",
			"name":             "", // would normally fail "name is required"
			"quantity":         0,
			"unit":             "BOGUS", // would normally fail unit check
			"category":         "",
			"expiration_date":  nil,
			"storage_location": "",
			"barcode":          "",
			"notes":            "",
			"created_at":       "2026-04-26T00:00:00Z",
			"updated_at":       "2026-04-26T00:00:00Z",
			"deleted_at":       deletedAt,
		}},
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 for tombstone; err=%v", status, errEnv)
	}
}
