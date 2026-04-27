package handlers_test

// Tests for the household-management endpoints added in Phase 1:
//   - GET    /api/households            (list)
//   - POST   /api/households            (create)
//   - POST   /api/households/switch
//   - PATCH  /api/households/{id}       (rename)
//   - DELETE /api/households/{id}/membership   (leave)
//   - POST   /api/households/{id}/transfer
//   - GET    /api/households/{id}/invites      (list active)
//   - DELETE /api/households/{id}/invites/{code} (revoke)

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vinegod/medvault/backend/internal/handlers"
	"github.com/vinegod/medvault/backend/internal/middleware"
	"github.com/vinegod/medvault/backend/internal/testutil"
)

// newMgmtHarness wires every household-management route onto a real
// httptest server so the tests below exercise the same chi + middleware
// stack production uses.
func newMgmtHarness(t *testing.T) *harness {
	t.Helper()
	db := testutil.NewDB(t)

	authH := &handlers.AuthHandler{
		DB:         db,
		JWTSecret:  testutil.JWTSecret,
		AccessTTL:  time.Hour,
		RefreshTTL: 24 * time.Hour,
	}
	medH := &handlers.MedicineHandler{DB: db}
	hhH := &handlers.HouseholdHandler{DB: db}

	r := chi.NewRouter()
	r.Post("/api/auth/register", authH.Register)
	r.Post("/api/auth/login", authH.Login)

	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(testutil.JWTSecret))

		// No household middleware: must work after the user has just lost
		// their last membership.
		r.Get("/api/households", hhH.List)
		r.Post("/api/households", hhH.Create)
		r.Post("/api/households/switch", hhH.Switch)

		// Routes that require an active household.
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireHousehold(db))

			r.Get("/api/medicines", medH.List)
			r.Post("/api/medicines", medH.Create)

			r.Get("/api/households/current", hhH.GetCurrent)
			r.Post("/api/households/join", hhH.Join)
			r.Patch("/api/households/{id}", hhH.Rename)
			r.Delete("/api/households/{id}/membership", hhH.Leave)
			r.Post("/api/households/{id}/transfer", hhH.Transfer)
			r.Post("/api/households/{id}/invite", hhH.CreateInvite)
			r.Get("/api/households/{id}/invites", hhH.ListInvites)
			r.Delete("/api/households/{id}/invites/{code}", hhH.RevokeInvite)
		})
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return &harness{t: t, server: srv, db: db}
}

// doArray sends a request whose response data field is a JSON array of
// objects, returning (status, decoded). The existing harness.do assumes
// data is an object — this is the array-returning sibling.
func (h *harness) doArray(method, path, bearer string, body any) (status int, items []map[string]any) {
	h.t.Helper()
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			h.t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, h.server.URL+path, reader)
	if err != nil {
		h.t.Fatalf("new request: %v", err)
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if reader != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatalf("do request: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort drain for test HTTP responses

	raw, _ := io.ReadAll(resp.Body)
	var env struct {
		Data []map[string]any `json:"data"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &env); err != nil {
			h.t.Fatalf("decode array (status=%d body=%s): %v", resp.StatusCode, raw, err)
		}
	}
	return resp.StatusCode, env.Data
}

// --- Tests -----------------------------------------------------------------

// TestList_FreshUser: a freshly registered user has exactly one household
// (their personal one), flagged active.
func TestList_FreshUser(t *testing.T) {
	h := newMgmtHarness(t)
	tok, _ := h.register("alice", "alicepass123")

	status, items := h.doArray("GET", "/api/households", tok, nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if len(items) != 1 {
		t.Fatalf("got %d households, want 1", len(items))
	}
	if items[0]["is_active"] != true {
		t.Errorf("is_active = %v, want true", items[0]["is_active"])
	}
	if items[0]["role"] != "owner" {
		t.Errorf("role = %v, want owner", items[0]["role"])
	}
}

// TestSwitch_Roundtrip: Bob joins Alice's via invite (auto-switches to it),
// then explicitly switches back to his personal household.
func TestSwitch_Roundtrip(t *testing.T) {
	h := newMgmtHarness(t)

	aliceTok, _ := h.register("alice", "alicepass123")
	bobTok, _ := h.register("bob", "bobpass123")

	_, currentA, _ := h.do("GET", "/api/households/current", aliceTok, nil)
	aliceHHID := currentA["id"].(string)
	_, inv, _ := h.do("POST", "/api/households/"+aliceHHID+"/invite",
		aliceTok, map[string]any{})

	_, currentB, _ := h.do("GET", "/api/households/current", bobTok, nil)
	bobPersonalID := currentB["id"].(string)

	if status, _, _ := h.do("POST", "/api/households/join", bobTok,
		map[string]string{"code": inv["code"].(string)}); status != http.StatusOK {
		t.Fatalf("join failed")
	}

	// After join, Bob's active is Alice's household.
	_, after, _ := h.do("GET", "/api/households/current", bobTok, nil)
	if after["id"] != aliceHHID {
		t.Errorf("after join, active = %v, want %s", after["id"], aliceHHID)
	}

	// Switch back to his personal household.
	if status, _, errEnv := h.do("POST", "/api/households/switch", bobTok,
		map[string]string{"household_id": bobPersonalID}); status != http.StatusOK {
		t.Fatalf("switch: status=%d err=%v", status, errEnv)
	}
	_, again, _ := h.do("GET", "/api/households/current", bobTok, nil)
	if again["id"] != bobPersonalID {
		t.Errorf("after switch, active = %v, want %s", again["id"], bobPersonalID)
	}
}

// TestSwitch_NonMember: switching to a household you don't belong to is 403.
func TestSwitch_NonMember(t *testing.T) {
	h := newMgmtHarness(t)
	aliceTok, _ := h.register("alice", "alicepass123")
	bobTok, _ := h.register("bob", "bobpass123")

	_, currentA, _ := h.do("GET", "/api/households/current", aliceTok, nil)

	status, _, _ := h.do("POST", "/api/households/switch", bobTok,
		map[string]string{"household_id": currentA["id"].(string)})
	if status != http.StatusForbidden {
		t.Errorf("status = %d, want 403", status)
	}
}

// TestCreate_BecomesActive: creating a household promotes the caller to
// owner and switches active to the new one.
func TestCreate_BecomesActive(t *testing.T) {
	h := newMgmtHarness(t)
	tok, _ := h.register("alice", "alicepass123")

	status, hh, errEnv := h.do("POST", "/api/households", tok,
		map[string]string{"name": "Beach House"})
	if status != http.StatusCreated {
		t.Fatalf("create: status=%d err=%v", status, errEnv)
	}
	newID := hh["id"].(string)

	_, current, _ := h.do("GET", "/api/households/current", tok, nil)
	if current["id"] != newID {
		t.Errorf("after create, active = %v, want %s", current["id"], newID)
	}
	if current["name"] != "Beach House" {
		t.Errorf("name = %v, want Beach House", current["name"])
	}
}

// TestRename_OwnerOnly: owner can rename; an outsider gets 403.
func TestRename_OwnerOnly(t *testing.T) {
	h := newMgmtHarness(t)
	aliceTok, _ := h.register("alice", "alicepass123")
	carolTok, _ := h.register("carol", "carolpass123")

	_, currentA, _ := h.do("GET", "/api/households/current", aliceTok, nil)
	aliceHHID := currentA["id"].(string)

	if status, _, errEnv := h.do("PATCH", "/api/households/"+aliceHHID, aliceTok,
		map[string]string{"name": "Renamed"}); status != http.StatusOK {
		t.Fatalf("owner rename: status=%d err=%v", status, errEnv)
	}
	_, after, _ := h.do("GET", "/api/households/current", aliceTok, nil)
	if after["name"] != "Renamed" {
		t.Errorf("name not updated; got %v", after["name"])
	}

	status, _, _ := h.do("PATCH", "/api/households/"+aliceHHID, carolTok,
		map[string]string{"name": "Hijacked"})
	if status != http.StatusForbidden {
		t.Errorf("cross-household rename status=%d, want 403", status)
	}
}

// TestLeave_SoleMember: leaving as the only member deletes the household
// and cascades medicines.
func TestLeave_SoleMember(t *testing.T) {
	h := newMgmtHarness(t)
	tok, _ := h.register("alice", "alicepass123")

	_, current, _ := h.do("GET", "/api/households/current", tok, nil)
	hhID := current["id"].(string)

	if status, _, _ := h.do("POST", "/api/medicines", tok, map[string]any{
		"name": "Aspirin", "quantity": 10, "unit": "tablets",
	}); status != http.StatusCreated {
		t.Fatalf("seed medicine failed")
	}

	if status, _, errEnv := h.do("DELETE", "/api/households/"+hhID+"/membership",
		tok, nil); status != http.StatusOK {
		t.Fatalf("leave: status=%d err=%v", status, errEnv)
	}

	var hhCount, medCount int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM households WHERE id = ?`, hhID).
		Scan(&hhCount); err != nil {
		t.Fatalf("count households: %v", err)
	}
	if hhCount != 0 {
		t.Errorf("household still present after sole-member leave")
	}
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM medicines WHERE household_id = ?`, hhID).
		Scan(&medCount); err != nil {
		t.Fatalf("count medicines: %v", err)
	}
	if medCount != 0 {
		t.Errorf("medicines = %d, want 0 (cascade delete)", medCount)
	}
}

// TestLeave_OwnerTransferRequired: a sole owner of a multi-member household
// must transfer first, then can leave.
func TestLeave_OwnerTransferRequired(t *testing.T) {
	h := newMgmtHarness(t)
	aliceTok, _ := h.register("alice", "alicepass123")
	bobTok, bobID := h.register("bob", "bobpass123")

	_, currentA, _ := h.do("GET", "/api/households/current", aliceTok, nil)
	aliceHHID := currentA["id"].(string)
	_, inv, _ := h.do("POST", "/api/households/"+aliceHHID+"/invite",
		aliceTok, map[string]any{})
	if status, _, _ := h.do("POST", "/api/households/join", bobTok,
		map[string]string{"code": inv["code"].(string)}); status != http.StatusOK {
		t.Fatalf("setup: bob join failed")
	}

	status, _, errEnv := h.do("DELETE", "/api/households/"+aliceHHID+"/membership",
		aliceTok, nil)
	if status != http.StatusConflict {
		t.Fatalf("status=%d, want 409; err=%v", status, errEnv)
	}
	if errEnv == nil || errEnv["code"] != "OWNER_TRANSFER_REQUIRED" {
		t.Errorf("err code = %v, want OWNER_TRANSFER_REQUIRED", errEnv)
	}

	if status, _, errEnv := h.do("POST", "/api/households/"+aliceHHID+"/transfer",
		aliceTok, map[string]string{"to_user_id": bobID}); status != http.StatusOK {
		t.Fatalf("transfer: status=%d err=%v", status, errEnv)
	}
	if status, _, errEnv := h.do("DELETE", "/api/households/"+aliceHHID+"/membership",
		aliceTok, nil); status != http.StatusOK {
		t.Fatalf("post-transfer leave: status=%d err=%v", status, errEnv)
	}

	// Alice has no remaining household → /current is 403.
	if status, _, _ := h.do("GET", "/api/households/current", aliceTok, nil); status != http.StatusForbidden {
		t.Errorf("post-leave /current status=%d, want 403", status)
	}

	// And her active_household_id pointer was cleared.
	var active sql.NullString
	if err := h.db.QueryRow(
		`SELECT u.active_household_id FROM users u
		   JOIN refresh_tokens rt ON rt.user_id = u.id
		  WHERE u.username = ?`, "alice",
	).Scan(&active); err != nil {
		t.Fatalf("read active: %v", err)
	}
	if active.Valid {
		t.Errorf("active_household_id = %q, want NULL", active.String)
	}
}

// TestInvites_ListAndRevoke: owner creates two codes, lists them, revokes
// one. Revoked one is no longer listed and rejected by Join.
func TestInvites_ListAndRevoke(t *testing.T) {
	h := newMgmtHarness(t)
	aliceTok, _ := h.register("alice", "alicepass123")
	bobTok, _ := h.register("bob", "bobpass123")

	_, currentA, _ := h.do("GET", "/api/households/current", aliceTok, nil)
	aliceHHID := currentA["id"].(string)

	_, inv1, _ := h.do("POST", "/api/households/"+aliceHHID+"/invite",
		aliceTok, map[string]any{})
	_, inv2, _ := h.do("POST", "/api/households/"+aliceHHID+"/invite",
		aliceTok, map[string]any{})

	status, items := h.doArray("GET", "/api/households/"+aliceHHID+"/invites",
		aliceTok, nil)
	if status != http.StatusOK {
		t.Fatalf("list invites: status=%d", status)
	}
	if len(items) != 2 {
		t.Fatalf("invites listed = %d, want 2", len(items))
	}

	if status, _, errEnv := h.do("DELETE",
		"/api/households/"+aliceHHID+"/invites/"+inv1["code"].(string),
		aliceTok, nil); status != http.StatusOK {
		t.Fatalf("revoke: status=%d err=%v", status, errEnv)
	}

	if status, _, _ := h.do("POST", "/api/households/join", bobTok,
		map[string]string{"code": inv1["code"].(string)}); status != http.StatusBadRequest {
		t.Errorf("redeem revoked code status=%d, want 400", status)
	}
	if status, _, _ := h.do("POST", "/api/households/join", bobTok,
		map[string]string{"code": inv2["code"].(string)}); status != http.StatusOK {
		t.Errorf("redeem unrevoked code status=%d, want 200", status)
	}
}

// TestTransfer_NonMemberTarget: target must be a member of the household.
func TestTransfer_NonMemberTarget(t *testing.T) {
	h := newMgmtHarness(t)
	aliceTok, _ := h.register("alice", "alicepass123")
	_, carolID := h.register("carol", "carolpass123")

	_, currentA, _ := h.do("GET", "/api/households/current", aliceTok, nil)
	aliceHHID := currentA["id"].(string)

	status, _, _ := h.do("POST", "/api/households/"+aliceHHID+"/transfer",
		aliceTok, map[string]string{"to_user_id": carolID})
	if status != http.StatusNotFound {
		t.Errorf("status=%d, want 404", status)
	}
}
