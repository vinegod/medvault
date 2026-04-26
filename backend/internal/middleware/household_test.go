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

	req := httptest.NewRequest(http.MethodGet, "/", nil)
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

	req := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx)
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

	req := httptest.NewRequest(http.MethodGet, "/", nil) // no ctx user_id
	rec := httptest.NewRecorder()
	wrapped.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

// TestRequireHousehold_PicksMostRecentlyJoined exercises the active-household
// heuristic: a user with two memberships gets the more recently joined one.
// This is the contract that lets a freshly-redeemed invite immediately
// switch the user's active household.
func TestRequireHousehold_PicksMostRecentlyJoined(t *testing.T) {
	db := testutil.NewDB(t)

	// Alice's personal household: joined long ago.
	old := time.Now().UTC().Add(-48 * time.Hour)
	alice := testutil.MakeUserAt(t, db, "alice", old)

	// A second household, joined yesterday — should win.
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

	rec, got := runWithUserID(t, db, alice.ID)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got.householdID != other {
		t.Errorf("household_id = %q, want most-recently-joined %q (got the older one instead)",
			got.householdID, other)
	}
	if got.role != models.RoleMember {
		t.Errorf("role = %q, want %q (role of the active household)", got.role, models.RoleMember)
	}
}
