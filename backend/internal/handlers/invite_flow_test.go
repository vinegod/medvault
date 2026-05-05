package handlers_test

// End-to-end test of the invite flow: register two users, owner generates a
// code, member redeems it, both see the same household-scoped medicine.
// Also covers the negative cases: re-redeem, wrong household, role gate.

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/vinegod/medvault/backend/internal/handlers"
	"github.com/vinegod/medvault/backend/internal/middleware"
	"github.com/vinegod/medvault/backend/internal/testutil"
)

// --- harness ---------------------------------------------------------------

type harness struct {
	t      *testing.T
	server *httptest.Server
	db     *sql.DB
}

func newHarness(t *testing.T) *harness {
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
	syncH := &handlers.SyncHandler{DB: db}

	r := chi.NewRouter()
	r.Post("/api/auth/register", authH.Register)
	r.Post("/api/auth/login", authH.Login)
	r.Post("/api/auth/refresh", authH.Refresh)
	r.Post("/api/auth/logout", authH.Logout)

	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(testutil.JWTSecret))

		// Per-user, no household middleware.
		r.Post("/api/auth/password", authH.ChangePassword)

		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireHousehold(db))

			r.Get("/api/medicines", medH.List)
			r.Post("/api/medicines", medH.Create)
			r.Get("/api/medicines/{id}", medH.Get)
			r.Put("/api/medicines/{id}", medH.Update)
			r.Delete("/api/medicines/{id}", medH.Delete)
			r.Post("/api/sync", syncH.Sync)

			r.Get("/api/households/current", hhH.GetCurrent)
			r.Post("/api/households/join", hhH.Join)
			r.Post("/api/households/{id}/invite", hhH.CreateInvite)
			r.Delete("/api/households/{id}/members/{user_id}", hhH.RemoveMember)
		})
	})

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return &harness{t: t, server: srv, db: db}
}

// do issues an HTTP request against the test server, decodes the standard
// envelope into a map, and returns (status, dataField, errorField).
//
// Using a map keeps this helper generic — every test case asserts only the
// fields it cares about. Bearer is the access token (or "" for unauth).
func (h *harness) do(method, path, bearer string, body any) (status int, data, errEnv map[string]any) {
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
		Data  any            `json:"data"`
		Error map[string]any `json:"error"`
	}
	if len(raw) > 0 {
		if err := json.Unmarshal(raw, &env); err != nil {
			h.t.Fatalf("decode envelope (status=%d body=%s): %v", resp.StatusCode, raw, err)
		}
	}

	dataMap, _ := env.Data.(map[string]any)
	return resp.StatusCode, dataMap, env.Error
}

// register hits POST /api/auth/register and returns the access token.
func (h *harness) register(username, password string) (token, userID string) {
	h.t.Helper()
	status, data, errEnv := h.do("POST", "/api/auth/register", "",
		map[string]string{"username": username, "password": password})
	if status != http.StatusCreated {
		h.t.Fatalf("register %s: status=%d err=%v", username, status, errEnv)
	}
	user, _ := data["user"].(map[string]any)
	return data["access_token"].(string), user["id"].(string)
}

// --- tests -----------------------------------------------------------------

// TestInviteFlow_HappyPath walks the full A-invites-B journey end-to-end and
// asserts that household scoping works in both directions: B sees A's
// medicine after joining, and the household roster contains both members
// with correct roles.
func TestInviteFlow_HappyPath(t *testing.T) {
	h := newHarness(t)

	aliceTok, aliceID := h.register("alice", "alicepass123")
	bobTok, bobID := h.register("bob", "bobpass123")

	// Alice creates Aspirin in her personal household.
	status, med, errEnv := h.do("POST", "/api/medicines", aliceTok, map[string]any{
		"name": "Aspirin", "quantity": 20, "unit": "tablets",
		"expiration_date": "2027-12-31",
	})
	if status != http.StatusCreated {
		t.Fatalf("create medicine: status=%d err=%v", status, errEnv)
	}
	medID := med["id"].(string)

	// Alice's household id (needed for the invite path param).
	status, currentA, _ := h.do("GET", "/api/households/current", aliceTok, nil)
	if status != http.StatusOK {
		t.Fatalf("current household (A): status=%d", status)
	}
	aliceHHID := currentA["id"].(string)

	// Alice generates an invite.
	status, inv, errEnv := h.do("POST", "/api/households/"+aliceHHID+"/invite", aliceTok, map[string]any{})
	if status != http.StatusCreated {
		t.Fatalf("create invite: status=%d err=%v", status, errEnv)
	}
	code := inv["code"].(string)
	if len(code) != 8 {
		t.Errorf("invite code length = %d, want 8 (charset %q)", len(code), code)
	}
	// created_at must not be the zero-time (regression — was buggy in Phase 15).
	if ts, _ := inv["created_at"].(string); ts == "" || strings.HasPrefix(ts, "0001-01-01") {
		t.Errorf("invite.created_at = %q, want a real timestamp", ts)
	}

	// Bob redeems.
	status, joined, errEnv := h.do("POST", "/api/households/join", bobTok, map[string]string{"code": code})
	if status != http.StatusOK {
		t.Fatalf("join: status=%d err=%v", status, errEnv)
	}
	if got := joined["household_id"]; got != aliceHHID {
		t.Errorf("join returned household_id=%v, want %s", got, aliceHHID)
	}

	// Bob now sees Aspirin (household scoping).
	status, _, _ = h.do("GET", "/api/medicines", bobTok, nil)
	if status != http.StatusOK {
		t.Fatalf("Bob list medicines: status=%d", status)
	}
	meds := listMedicines(t, h, bobTok)
	if len(meds) != 1 {
		t.Fatalf("Bob sees %d medicines, want 1", len(meds))
	}
	if meds[0]["id"] != medID {
		t.Errorf("Bob sees medicine id=%v, want %s", meds[0]["id"], medID)
	}

	// Bob's view of /current shows both members with the right roles.
	status, currentB, _ := h.do("GET", "/api/households/current", bobTok, nil)
	if status != http.StatusOK {
		t.Fatalf("current household (B): status=%d", status)
	}
	members, _ := currentB["members"].([]any)
	if len(members) != 2 {
		t.Fatalf("members = %d, want 2", len(members))
	}
	assertMemberRoles(t, members, map[string]string{
		aliceID: "owner",
		bobID:   "member",
	})
}

// TestInviteFlow_RoleGate: only owners can generate invites. A member who
// just joined cannot turn around and invite a third party.
func TestInviteFlow_RoleGate(t *testing.T) {
	h := newHarness(t)

	aliceTok, _ := h.register("alice", "alicepass123")
	bobTok, _ := h.register("bob", "bobpass123")

	_, currentA, _ := h.do("GET", "/api/households/current", aliceTok, nil)
	aliceHHID := currentA["id"].(string)

	_, inv, _ := h.do("POST", "/api/households/"+aliceHHID+"/invite", aliceTok, map[string]any{})
	code := inv["code"].(string)

	if status, _, _ := h.do(
		"POST",
		"/api/households/join",
		bobTok,
		map[string]string{"code": code},
	); status != http.StatusOK {
		t.Fatalf("join setup: status=%d", status)
	}

	// Bob (now a member of Alice's household) tries to issue an invite.
	status, _, errEnv := h.do("POST", "/api/households/"+aliceHHID+"/invite", bobTok, map[string]any{})
	if status != http.StatusForbidden {
		t.Errorf("status = %d, want 403", status)
	}
	if errEnv == nil || errEnv["code"] != "FORBIDDEN" {
		t.Errorf("error code = %v, want FORBIDDEN", errEnv)
	}
}

// TestInviteFlow_HouseholdIsolation: a third user in a different household
// must not see Alice's medicines, and must not be able to call
// invite/{aliceHH} as if it were her own.
func TestInviteFlow_HouseholdIsolation(t *testing.T) {
	h := newHarness(t)

	aliceTok, _ := h.register("alice", "alicepass123")
	carolTok, _ := h.register("carol", "carolpass123")

	// Alice creates a medicine.
	if status, _, _ := h.do("POST", "/api/medicines", aliceTok, map[string]any{
		"name": "Ibuprofen", "quantity": 30, "unit": "tablets", "expiration_date": "2027-06-30",
	}); status != http.StatusCreated {
		t.Fatalf("create medicine: status=%d", status)
	}

	// Carol's medicine list should be empty — she's in her own household.
	if got := listMedicines(t, h, carolTok); len(got) != 0 {
		t.Errorf("carol sees %d medicines, want 0 (household isolation broken)", len(got))
	}

	// Carol tries to issue an invite for Alice's household by guessing the id.
	_, currentA, _ := h.do("GET", "/api/households/current", aliceTok, nil)
	aliceHHID := currentA["id"].(string)

	status, _, errEnv := h.do("POST", "/api/households/"+aliceHHID+"/invite", carolTok, map[string]any{})
	if status != http.StatusForbidden {
		t.Errorf("status = %d, want 403 (carol must not invite into alice's household)", status)
	}
	if errEnv == nil || errEnv["code"] != "FORBIDDEN" {
		t.Errorf("error code = %v, want FORBIDDEN", errEnv)
	}
}

// TestInviteFlow_CodeReuse: an invite is a one-shot. The DB sets used_at on
// redemption, so the second redeemer must be rejected.
func TestInviteFlow_CodeReuse(t *testing.T) {
	h := newHarness(t)

	aliceTok, _ := h.register("alice", "alicepass123")
	bobTok, _ := h.register("bob", "bobpass123")
	carolTok, _ := h.register("carol", "carolpass123")

	_, currentA, _ := h.do("GET", "/api/households/current", aliceTok, nil)
	_, inv, _ := h.do("POST", "/api/households/"+currentA["id"].(string)+"/invite", aliceTok, map[string]any{})
	code := inv["code"].(string)

	if status, _, _ := h.do(
		"POST",
		"/api/households/join",
		bobTok,
		map[string]string{"code": code},
	); status != http.StatusOK {
		t.Fatalf("first join: status=%d", status)
	}

	// Carol tries to use the same code — should be rejected as used.
	status, _, errEnv := h.do("POST", "/api/households/join", carolTok, map[string]string{"code": code})
	if status != http.StatusBadRequest {
		t.Errorf("second join status = %d, want 400; err=%v", status, errEnv)
	}
}

// TestInviteFlow_AlreadyMember: redeeming a fresh code for a household you
// already belong to returns 409 — the DB COUNT(*) check fires before the
// membership INSERT would violate the PK.
func TestInviteFlow_AlreadyMember(t *testing.T) {
	h := newHarness(t)

	aliceTok, _ := h.register("alice", "alicepass123")
	bobTok, _ := h.register("bob", "bobpass123")

	_, currentA, _ := h.do("GET", "/api/households/current", aliceTok, nil)
	aliceHHID := currentA["id"].(string)

	// First invite + join: succeeds.
	_, inv1, _ := h.do("POST", "/api/households/"+aliceHHID+"/invite", aliceTok, map[string]any{})
	if status, _, _ := h.do("POST", "/api/households/join", bobTok,
		map[string]string{"code": inv1["code"].(string)}); status != http.StatusOK {
		t.Fatalf("first join: status=%d", status)
	}

	// Alice issues a fresh code; Bob tries to join again — already a member.
	_, inv2, _ := h.do("POST", "/api/households/"+aliceHHID+"/invite", aliceTok, map[string]any{})
	status, _, errEnv := h.do("POST", "/api/households/join", bobTok,
		map[string]string{"code": inv2["code"].(string)})
	if status != http.StatusConflict {
		t.Errorf("status = %d, want 409; err=%v", status, errEnv)
	}
}

// TestInviteFlow_BadCode: the join handler treats unknown, malformed, and
// expired codes uniformly with a 400 — never leaking which case it was.
func TestInviteFlow_BadCode(t *testing.T) {
	h := newHarness(t)
	bobTok, _ := h.register("bob", "bobpass123")

	cases := []struct {
		name string
		code string
	}{
		{"empty", ""},               // fails Validate before DB
		{"unknown", "ZZZZZZZZ"},     // 8 chars, not in table
		{"too-short", "AB"},         // passes Validate, no DB row
		{"weird-chars", "!!!!!!!!"}, // no DB row
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			status, _, errEnv := h.do("POST", "/api/households/join", bobTok,
				map[string]string{"code": c.code})
			if status != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; err=%v", status, errEnv)
			}
		})
	}

	// Expired code: seed a real invite with expires_at in the past.
	aliceTok, _ := h.register("alice", "alicepass123")
	_, currentA, _ := h.do("GET", "/api/households/current", aliceTok, nil)
	const expiredCode = "EXPIRED1"
	if _, err := h.db.Exec(
		`INSERT INTO household_invites
		 (code, household_id, invited_by, expires_at, created_at)
		 VALUES (?, ?, (SELECT created_by FROM households WHERE id=?), ?, ?)`,
		expiredCode, currentA["id"], currentA["id"],
		time.Now().UTC().Add(-1*time.Hour), time.Now().UTC().Add(-2*time.Hour),
	); err != nil {
		t.Fatalf("seed expired invite: %v", err)
	}
	status, _, errEnv := h.do("POST", "/api/households/join", bobTok,
		map[string]string{"code": expiredCode})
	if status != http.StatusBadRequest {
		t.Errorf("expired code status = %d, want 400; err=%v", status, errEnv)
	}
}

// TestInviteFlow_AuthRequired: every protected route must reject calls
// without a Bearer token.
func TestInviteFlow_AuthRequired(t *testing.T) {
	h := newHarness(t)
	for _, path := range []string{
		"/api/medicines",
		"/api/households/current",
	} {
		status, _, _ := h.do("GET", path, "", nil)
		if status != http.StatusUnauthorized {
			t.Errorf("%s without auth: status=%d, want 401", path, status)
		}
	}
}

// --- helpers ---------------------------------------------------------------

func assertMemberRoles(t *testing.T, members []any, want map[string]string) {
	t.Helper()
	got := map[string]string{}
	for _, m := range members {
		mm := m.(map[string]any)
		got[mm["user_id"].(string)] = mm["role"].(string)
	}
	for userID, wantRole := range want {
		if got[userID] != wantRole {
			t.Errorf("user %s role = %q, want %q", userID, got[userID], wantRole)
		}
	}
}

func listMedicines(t *testing.T, h *harness, bearer string) []map[string]any {
	t.Helper()
	req, _ := http.NewRequest("GET", h.server.URL+"/api/medicines", http.NoBody)
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("list medicines: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort drain for test HTTP responses

	var env struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list status = %d", resp.StatusCode)
	}
	return env.Data
}
