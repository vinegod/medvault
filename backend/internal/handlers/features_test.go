package handlers_test

import (
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/vinegod/medvault/backend/internal/handlers"
)

// householdID returns the caller's active household id.
func (h *harness) householdID(tok string) string {
	h.t.Helper()
	status, cur, errEnv := h.do("GET", "/api/households/current", tok, nil)
	if status != http.StatusOK {
		h.t.Fatalf("current household: status=%d err=%v", status, errEnv)
	}
	return cur["id"].(string)
}

// inviteCode creates an invite to the caller's household and returns it.
func (h *harness) inviteCode(tok string) string {
	h.t.Helper()
	status, inv, errEnv := h.do("POST", "/api/households/"+h.householdID(tok)+"/invite", tok, map[string]any{})
	if status != http.StatusCreated {
		h.t.Fatalf("create invite: status=%d err=%v", status, errEnv)
	}
	return inv["code"].(string)
}

// history returns the actions of a medicine's history, oldest first.
func (h *harness) history(tok, id string) []map[string]any {
	h.t.Helper()
	status, items := h.doArray("GET", "/api/medicines/"+id+"/history", tok, nil)
	if status != http.StatusOK {
		h.t.Fatalf("history: status=%d", status)
	}
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	return items
}

func actions(entries []map[string]any) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e["action"].(string)
	}
	return out
}

func TestSyncAudit_Lifecycle(t *testing.T) {
	h := newHarness(t)
	tok, _ := h.register("alice", "alicepass123")
	id := uuid.NewString()

	steps := []struct {
		name    string
		change  map[string]any
		wantLog []string
	}{
		{
			"create", syncChange(id, 10, "2026-05-01T10:00:00Z"),
			[]string{"medicine.create"},
		},
		{
			"quantity", syncChange(id, 7, "2026-05-01T11:00:00Z"),
			[]string{"medicine.create", "medicine.quantity"},
		},
		{
			"no-op save", syncChange(id, 7, "2026-05-01T12:00:00Z"),
			[]string{"medicine.create", "medicine.quantity"},
		},
		{
			"lost LWW", syncChange(id, 1, "2026-05-01T09:00:00Z"),
			[]string{"medicine.create", "medicine.quantity"},
		},
		{"dispose", withFields(syncChange(id, 7, "2026-05-01T13:00:00Z"), map[string]any{
			"deleted_at": "2026-05-01T13:00:00Z", "disposed_at": "2026-05-01T13:00:00Z",
		}), []string{"medicine.create", "medicine.quantity", "medicine.dispose"}},
		{"restore", withFields(syncChange(id, 7, "2026-05-01T14:00:00Z"), map[string]any{
			"disposed_at": "2026-05-01T13:00:00Z",
		}), []string{"medicine.create", "medicine.quantity", "medicine.dispose", "medicine.restore"}},
	}
	for _, s := range steps {
		if status, _, _ := syncRecords(t, h, tok, nil, []map[string]any{s.change}); status != http.StatusOK {
			t.Fatalf("%s: sync status %d", s.name, status)
		}
		got := actions(h.history(tok, id))
		if len(got) != len(s.wantLog) {
			t.Fatalf("%s: history = %v, want %v", s.name, got, s.wantLog)
		}
		for i := range got {
			if got[i] != s.wantLog[i] {
				t.Fatalf("%s: history = %v, want %v", s.name, got, s.wantLog)
			}
		}
	}

	qty := h.history(tok, id)[1]
	if qty["quantity_before"] != float64(10) || qty["quantity_after"] != float64(7) {
		t.Errorf("quantity row = %v -> %v, want 10 -> 7", qty["quantity_before"], qty["quantity_after"])
	}

	var disposed *string
	if err := h.db.QueryRow(`SELECT disposed_at FROM medicines WHERE id = ?`, id).Scan(&disposed); err != nil {
		t.Fatalf("read disposed_at: %v", err)
	}
	if disposed != nil {
		t.Errorf("disposed_at = %q after restore, want NULL", *disposed)
	}
}

func withFields(m, extra map[string]any) map[string]any {
	for k, v := range extra {
		m[k] = v
	}
	return m
}

func TestSync_OmittedActiveIngredientIsKept(t *testing.T) {
	h := newHarness(t)
	tok, _ := h.register("alice", "alicepass123")
	id := uuid.NewString()

	first := withFields(syncChange(id, 5, "2026-05-01T10:00:00Z"), map[string]any{"active_ingredient": "ibuprofen"})
	syncRecords(t, h, tok, nil, []map[string]any{first})
	syncRecords(t, h, tok, nil, []map[string]any{syncChange(id, 4, "2026-05-01T11:00:00Z")})

	got := listMedicines(t, h, tok)
	if len(got) != 1 || got[0]["active_ingredient"] != "ibuprofen" {
		t.Errorf("active_ingredient after update without the field = %v, want ibuprofen", got[0]["active_ingredient"])
	}
}

func TestMedicineHistory_OtherHouseholdIsEmpty(t *testing.T) {
	h := newHarness(t)
	aliceTok, _ := h.register("alice", "alicepass123")
	bobTok, _ := h.register("bob", "bobpass123")
	id := uuid.NewString()
	syncRecords(t, h, aliceTok, nil, []map[string]any{syncChange(id, 5, "2026-05-01T10:00:00Z")})

	if got := h.history(bobTok, id); len(got) != 0 {
		t.Errorf("bob sees alice's history: %v", got)
	}
}

func TestRegister_Modes(t *testing.T) {
	t.Run("closed refuses after the first user", func(t *testing.T) {
		h := newHarnessWithMode(t, handlers.RegistrationClosed)
		h.register("admin", "adminpass123")

		status, _, errEnv := h.do("POST", "/api/auth/register", "",
			map[string]string{"username": "eve", "password": "evepass123"})
		if status != http.StatusForbidden || errEnv["code"] != "REGISTRATION_CLOSED" {
			t.Errorf("register = %d %v, want 403 REGISTRATION_CLOSED", status, errEnv)
		}
	})

	t.Run("invite mode", func(t *testing.T) {
		h := newHarnessWithMode(t, handlers.RegistrationInvite)
		aliceTok, _ := h.register("alice", "alicepass123")

		status, _, errEnv := h.do("POST", "/api/auth/register", "",
			map[string]string{"username": "bob", "password": "bobpass123"})
		if status != http.StatusBadRequest || errEnv["code"] != "INVITE_REQUIRED" {
			t.Errorf("no code: %d %v, want 400 INVITE_REQUIRED", status, errEnv)
		}

		status, _, _ = h.do("POST", "/api/auth/register", "",
			map[string]string{"username": "bob", "password": "bobpass123", "invite_code": "BADCODE1"})
		if status != http.StatusBadRequest {
			t.Errorf("bad code: status %d, want 400", status)
		}

		status, data, errEnv := h.do("POST", "/api/auth/register", "", map[string]string{
			"username": "bob", "password": "bobpass123", "invite_code": h.inviteCode(aliceTok),
		})
		if status != http.StatusCreated {
			t.Fatalf("valid code: status %d %v", status, errEnv)
		}
		bobTok := data["access_token"].(string)
		if got, want := h.householdID(bobTok), h.householdID(aliceTok); got != want {
			t.Errorf("bob's household = %s, want alice's %s", got, want)
		}
		var memberships int
		if err := h.db.QueryRow(`SELECT COUNT(*) FROM memberships m JOIN users u ON u.id = m.user_id
		                          WHERE u.username = 'bob'`).Scan(&memberships); err != nil {
			t.Fatalf("count memberships: %v", err)
		}
		if memberships != 1 {
			t.Errorf("bob has %d memberships, want only the invited household", memberships)
		}
	})
}

func TestLogoutOthers_KeepsCurrentSession(t *testing.T) {
	h := newHarness(t)
	h.register("alice", "alicepass123")
	login := func() (access, refresh string) {
		_, data, _ := h.do("POST", "/api/auth/login", "",
			map[string]string{"username": "alice", "password": "alicepass123"})
		return data["access_token"].(string), data["refresh_token"].(string)
	}
	access, keep := login()
	_, other := login()

	if status, _, errEnv := h.do("POST", "/api/auth/logout-others", access,
		map[string]string{"refresh_token": keep}); status != http.StatusOK {
		t.Fatalf("logout-others: %d %v", status, errEnv)
	}

	if status, _, _ := h.do(
		"POST",
		"/api/auth/refresh",
		"",
		map[string]string{"refresh_token": keep},
	); status != http.StatusOK {
		t.Errorf("current refresh token: status %d, want 200", status)
	}
	if status, _, _ := h.do(
		"POST",
		"/api/auth/refresh",
		"",
		map[string]string{"refresh_token": other},
	); status != http.StatusUnauthorized {
		t.Errorf("other refresh token: status %d, want 401", status)
	}
}

func TestDeleteAccount(t *testing.T) {
	h := newHarness(t)
	aliceTok, aliceID := h.register("alice", "alicepass123")
	bobTok, bobID := h.register("bob", "bobpass123")
	bobPersonal := h.householdID(bobTok)

	if status, _, errEnv := h.do("POST", "/api/households/join", bobTok,
		map[string]string{"code": h.inviteCode(aliceTok)}); status != http.StatusOK {
		t.Fatalf("join: %d %v", status, errEnv)
	}
	status, med, _ := h.do("POST", "/api/medicines", bobTok,
		map[string]any{"name": "Aspirin", "quantity": 3, "unit": "tablets"})
	if status != http.StatusCreated {
		t.Fatalf("create medicine: %d", status)
	}

	if status, _, _ := h.do("DELETE", "/api/auth/account", bobTok,
		map[string]string{"password": "wrong-password"}); status != http.StatusUnauthorized {
		t.Errorf("wrong password: status %d, want 401", status)
	}
	if status, _, errEnv := h.do("DELETE", "/api/auth/account", aliceTok,
		map[string]string{"password": "alicepass123"}); status != http.StatusConflict ||
		errEnv["code"] != "OWNER_TRANSFER_REQUIRED" {
		t.Errorf("owner with members: %d %v, want 409 OWNER_TRANSFER_REQUIRED", status, errEnv)
	}

	if status, _, errEnv := h.do("DELETE", "/api/auth/account", bobTok,
		map[string]string{"password": "bobpass123"}); status != http.StatusOK {
		t.Fatalf("delete bob: %d %v", status, errEnv)
	}

	var createdBy string
	if err := h.db.QueryRow(`SELECT created_by FROM medicines WHERE id = ?`, med["id"]).Scan(&createdBy); err != nil {
		t.Fatalf("bob's medicine in alice's household is gone: %v", err)
	}
	if createdBy != aliceID {
		t.Errorf("created_by = %s, want reassigned to owner %s", createdBy, aliceID)
	}
	var count int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM households WHERE id = ?`, bobPersonal).Scan(&count); err != nil {
		t.Fatalf("count households: %v", err)
	}
	if count != 0 {
		t.Error("bob's sole-member household was not deleted")
	}
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM users WHERE id = ?`, bobID).Scan(&count); err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 0 {
		t.Error("bob's user row still exists")
	}
}
