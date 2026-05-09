package middleware_test

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/vinegod/medvault/backend/internal/auth"
	"github.com/vinegod/medvault/backend/internal/middleware"
	"github.com/vinegod/medvault/backend/internal/models"
	"github.com/vinegod/medvault/backend/internal/testutil"
)

// runWithUserID invokes RequireHousehold(db) wrapped around a handler that
// captures the household_id and role it sees in context, and returns them
// alongside the HTTP response. This is the harness every test below uses.
type seen struct {
	householdID string
	role        string
	called      bool
}

func runWithUserID(t *testing.T, db *sql.DB, userID string) (*httptest.ResponseRecorder, *seen) {
	t.Helper()
	captured := &seen{}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.called = true
		captured.householdID = middleware.HouseholdIDFrom(r.Context())
		captured.role = middleware.RoleFrom(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	wrapped := middleware.RequireHousehold(db)(handler)

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	if userID != "" {
		// Mimic what RequireAuth would do upstream: stash user_id in ctx.
		req = req.WithContext(injectUserID(req.Context(), userID))
	}

	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)
	return rec, captured
}

// injectUserID returns a context with user_id set, as RequireAuth would do.
//
// userIDKey is package-private to middleware, so the only way to set it from
// outside the package is to actually run RequireAuth with a valid JWT. We
// mint one here and capture the resulting context.
func injectUserID(ctx context.Context, userID string) context.Context {
	tok, err := auth.GenerateAccessToken(testutil.JWTSecret, userID, "test-user", time.Hour)
	if err != nil {
		panic(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer "+tok)

	var out context.Context
	h := middleware.RequireAuth(testutil.JWTSecret)(http.HandlerFunc(
		func(_ http.ResponseWriter, r *http.Request) { out = r.Context() },
	))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if out == nil {
		panic("RequireAuth did not call next — token rejected")
	}
	return out
}

// --- tests ---

// TestRequireHousehold_OK is the happy path: a user with one membership
// gets household_id and role injected into ctx.
func TestRequireHousehold_OK(t *testing.T) {
	db := testutil.NewDB(t)
	u := testutil.MakeUser(t, db, "alice")

	rec, got := runWithUserID(t, db, u.ID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !got.called {
		t.Fatal("downstream handler not called")
	}
	if got.householdID != u.HouseholdID {
		t.Errorf("household_id = %q, want %q", got.householdID, u.HouseholdID)
	}
	if got.role != models.RoleOwner {
		t.Errorf("role = %q, want %q", got.role, models.RoleOwner)
	}
}

// TestRequireHousehold_NoMembership: a user without any membership row gets
// a 403 and the inner handler is never invoked.
func TestRequireHousehold_NoMembership(t *testing.T) {
	db := testutil.NewDB(t)

	// Insert a bare user — no household, no membership.
	loneID := uuid.NewString()
	if _, err := db.Exec(
		`INSERT INTO users (id, username, password_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		loneID, "lone", "x", time.Now(), time.Now(),
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}

	rec, got := runWithUserID(t, db, loneID)

	if rec.Code != http.StatusForbidden {
		t.Errorf("status = %d, want 403; body=%s", rec.Code, rec.Body.String())
	}
	if got.called {
		t.Error("downstream handler should NOT be called when 403")
	}
}

// TestRequireHousehold_MissingUserID: when RequireAuth hasn't run upstream,
// userID is empty and the middleware bails with 401.
func TestRequireHousehold_MissingUserID(t *testing.T) {
	db := testutil.NewDB(t)

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("downstream handler should NOT be called without userID")
		w.WriteHeader(http.StatusOK)
	})
	wrapped := middleware.RequireHousehold(db)(handler)

	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody) // no ctx user_id
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

// TestRequireHousehold_HonoursActivePointer: when the user has multiple
// memberships, the middleware reads users.active_household_id (set by
// Register / Switch / Join) instead of guessing.
func TestRequireHousehold_HonoursActivePointer(t *testing.T) {
	db := testutil.NewDB(t)

	old := time.Now().UTC().Add(-48 * time.Hour)
	alice := testutil.MakeUserAt(t, db, "alice", old)

	other := uuid.NewString()
	yesterday := time.Now().UTC().Add(-24 * time.Hour)
	if _, err := db.Exec(
		`INSERT INTO households (id, name, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		other, "Other House", alice.ID, yesterday, yesterday,
	); err != nil {
		t.Fatalf("insert other household: %v", err)
	}
	testutil.AddMembership(t, db, alice.ID, other, models.RoleMember, yesterday)

	// Explicitly point Alice at her personal household, even though
	// "other" was joined more recently. The pointer must win.
	if _, err := db.Exec(
		`UPDATE users SET active_household_id = ? WHERE id = ?`,
		alice.HouseholdID, alice.ID,
	); err != nil {
		t.Fatalf("set active: %v", err)
	}

	rec, got := runWithUserID(t, db, alice.ID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got.householdID != alice.HouseholdID {
		t.Errorf("household_id = %q, want %q (the explicit active pointer)",
			got.householdID, alice.HouseholdID)
	}
	if got.role != models.RoleOwner {
		t.Errorf("role = %q, want %q", got.role, models.RoleOwner)
	}
}

// TestRequireHousehold_FallsBackWhenActiveStale: if the active_household_id
// pointer references a household the user is no longer a member of (kicked
// from another device, household deleted), the middleware silently falls
// back to any remaining membership and persists the choice.
func TestRequireHousehold_FallsBackWhenActiveStale(t *testing.T) {
	db := testutil.NewDB(t)

	alice := testutil.MakeUser(t, db, "alice")

	// Point her at a household that doesn't exist (simulates the household
	// being deleted on another device after she fetched her token).
	bogus := uuid.NewString()
	if _, err := db.Exec(
		`INSERT INTO households (id, name, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		bogus, "Ghost", alice.ID, time.Now(), time.Now(),
	); err != nil {
		t.Fatalf("insert bogus: %v", err)
	}
	if _, err := db.Exec(
		`UPDATE users SET active_household_id = ? WHERE id = ?`,
		bogus, alice.ID,
	); err != nil {
		t.Fatalf("set bogus active: %v", err)
	}
	// No membership in `bogus` — pointer is now stale.

	rec, got := runWithUserID(t, db, alice.ID)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got.householdID != alice.HouseholdID {
		t.Errorf("fallback household = %q, want %q (alice's personal one)",
			got.householdID, alice.HouseholdID)
	}

	// And the pointer must have been persisted to the fallback so the
	// next request skips the recovery path.
	var stored sql.NullString
	if err := db.QueryRow(
		`SELECT active_household_id FROM users WHERE id = ?`, alice.ID,
	).Scan(&stored); err != nil {
		t.Fatalf("read active: %v", err)
	}
	if !stored.Valid || stored.String != alice.HouseholdID {
		t.Errorf("persisted active = %v, want %q", stored, alice.HouseholdID)
	}
}
