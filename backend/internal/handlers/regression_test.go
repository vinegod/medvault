package handlers_test

// Regression tests for bugs found in the October 2026 review. Each test
// failed against the code before its fix.

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/vinegod/medvault/backend/internal/auth"
)

func TestAuth_RefreshTokenRejectedAsAccessToken(t *testing.T) {
	h := newHarness(t)
	status, data, _ := h.do("POST", "/api/auth/register", "", map[string]string{
		"username": "alice", "password": "alicepass123",
	})
	if status != http.StatusCreated {
		t.Fatalf("register status=%d", status)
	}
	refresh := data["refresh_token"].(string)
	access := data["access_token"].(string)

	if status, _, _ := h.do("GET", "/api/medicines", refresh, nil); status != http.StatusUnauthorized {
		t.Errorf("refresh token as bearer: status=%d, want 401", status)
	}
	if status, _, _ := h.do("POST", "/api/auth/refresh", "", map[string]string{
		"refresh_token": access,
	}); status != http.StatusUnauthorized {
		t.Errorf("access token as refresh token: status=%d, want 401", status)
	}
}

func TestAuth_TokensUniqueWithinSameSecond(t *testing.T) {
	a, err := auth.GenerateRefreshToken("secret", "user-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	b, err := auth.GenerateRefreshToken("secret", "user-1", time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Error("back-to-back refresh tokens are identical; logout on one device would revoke both")
	}
}

func TestAuth_Register_RejectsOverlongPassword(t *testing.T) {
	h := newHarness(t)
	status, _, _ := h.do("POST", "/api/auth/register", "", map[string]string{
		"username": "alice", "password": strings.Repeat("a", 73),
	})
	if status != http.StatusBadRequest {
		t.Errorf("status=%d, want 400 (bcrypt rejects > 72 bytes)", status)
	}
}

func TestMedicines_Create_LimitsCountCharactersNotBytes(t *testing.T) {
	h := newHarness(t)
	tok, _ := h.register("alice", "alicepass123")

	// 150 Cyrillic characters = 300 bytes; within the 200-character limit.
	status, _, errEnv := h.do("POST", "/api/medicines", tok, map[string]any{
		"name": strings.Repeat("Ж", 150), "quantity": 1, "unit": "tablets",
		"notes": strings.Repeat("ж", 400),
	})
	if status != http.StatusCreated {
		t.Errorf("status=%d err=%v, want 201", status, errEnv)
	}
}

// syncChange builds a full sync record with explicit timestamps.
func syncChange(id string, quantity int, updatedAt string) map[string]any {
	return map[string]any{
		"id": id, "name": "Aspirin", "quantity": quantity, "unit": "tablets",
		"category": "", "expiration_date": nil, "storage_location": "",
		"barcode": "", "notes": "",
		"created_at": "2026-01-01T00:00:00Z", "updated_at": updatedAt,
		"deleted_at": nil,
	}
}

// syncRecords posts a sync and returns (status, records, server_time).
func syncRecords(
	t *testing.T, h *harness, tok string, lastSync any, changes []map[string]any,
) (status int, records []any, serverTime string) {
	t.Helper()
	if changes == nil {
		changes = []map[string]any{}
	}
	status, data, _ := h.do("POST", "/api/sync", tok, map[string]any{
		"last_sync": lastSync, "changes": changes,
	})
	records, _ = data["records"].([]any)
	serverTime, _ = data["server_time"].(string)
	return status, records, serverTime
}

func TestSync_LastSyncFromServerTimeIsHonoured(t *testing.T) {
	h := newHarness(t)
	tok, _ := h.register("alice", "alicepass123")
	syncRecords(t, h, tok, nil, []map[string]any{syncChange(uuid.NewString(), 1, "2026-01-01T00:00:00Z")})

	// server_time has second precision; step past the write's second.
	time.Sleep(1100 * time.Millisecond)
	_, all, cursor := syncRecords(t, h, tok, nil, nil)
	if len(all) != 1 {
		t.Fatalf("full sync: got %d records, want 1", len(all))
	}

	status, delta, _ := syncRecords(t, h, tok, cursor, nil)
	if status != http.StatusOK {
		t.Fatalf("delta sync status=%d", status)
	}
	if len(delta) != 0 {
		t.Errorf("delta sync with last_sync=%s returned %d records, want 0", cursor, len(delta))
	}
}

func TestSync_RejectsMalformedLastSync(t *testing.T) {
	h := newHarness(t)
	tok, _ := h.register("alice", "alicepass123")
	if status, _, _ := syncRecords(t, h, tok, "yesterday", nil); status != http.StatusBadRequest {
		t.Errorf("status=%d, want 400", status)
	}
}

func TestSync_LateOfflineEditReachesOtherDevices(t *testing.T) {
	h := newHarness(t)
	tok, _ := h.register("alice", "alicepass123")
	id := uuid.NewString()
	syncRecords(t, h, tok, nil, []map[string]any{syncChange(id, 1, "2026-01-01T00:00:00Z")})

	time.Sleep(1100 * time.Millisecond)
	_, _, cursorA := syncRecords(t, h, tok, nil, nil) // device A is now up to date

	// Device B edited offline before cursorA (by its clock) and uploads now.
	syncRecords(t, h, tok, cursorA, []map[string]any{syncChange(id, 7, "2026-01-02T00:00:00Z")})

	_, delta, _ := syncRecords(t, h, tok, cursorA, nil)
	if len(delta) != 1 {
		t.Fatalf("device A delta: got %d records, want the late offline edit", len(delta))
	}
	if q := delta[0].(map[string]any)["quantity"]; q != float64(7) {
		t.Errorf("quantity=%v, want 7", q)
	}
}

func TestSync_LWWComparesAcrossTimezoneOffsets(t *testing.T) {
	h := newHarness(t)
	tok, _ := h.register("alice", "alicepass123")
	id := uuid.NewString()

	syncRecords(t, h, tok, nil, []map[string]any{syncChange(id, 1, "2026-05-01T12:00:00Z")})
	// 14:00+03:00 is 11:00Z: older, must lose.
	syncRecords(t, h, tok, nil, []map[string]any{syncChange(id, 2, "2026-05-01T14:00:00+03:00")})

	got := listMedicines(t, h, tok)
	if len(got) != 1 || got[0]["quantity"] != float64(1) {
		t.Errorf("older edit with +03:00 offset overwrote newer one: %v", got)
	}
}

func TestSyncEnrich_SkipsValuesExceedingFieldLimits(t *testing.T) {
	longINN := strings.Repeat("x", 201) // active_ingredient limit is 200
	h := newEnrichHarness(t, "ua", [][4]string{{seedName, longINN, "ingredients", "group"}})
	id := uuid.NewString()

	if status := h.postSync([]map[string]any{changeRecord(id, seedName)}); status != http.StatusOK {
		t.Fatalf("sync status: got %d want 200", status)
	}
	if ingredient, _ := h.fetchMedicine(id); ingredient != "" {
		t.Errorf(
			"active_ingredient=%q: over-limit enrichment stored; later edits of this record would fail validation",
			ingredient,
		)
	}
}
