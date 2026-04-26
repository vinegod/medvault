// Package testutil provides shared helpers for backend integration tests:
// a fresh, fully-migrated SQLite database, factory functions for common
// fixtures (users with personal households), and JWT minting.
//
// Importing this package from non-test code is a mistake — it is built only
// for use from *_test.go files and assumes go test's working directory.
package testutil

import (
	"context"
	"database/sql"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/vinegod/medvault/backend/internal/auth"
	"github.com/vinegod/medvault/backend/internal/database"
	"github.com/vinegod/medvault/backend/internal/models"
)

// JWTSecret is the secret tests use when minting tokens. Tests that build a
// router or middleware with RequireAuth must use this same secret.
const JWTSecret = "test-secret-do-not-use-in-prod"

// migrationsDir resolves to backend/migrations regardless of which package's
// test is running. It walks up from this source file's directory.
func migrationsDir() string {
	_, file, _, _ := runtime.Caller(0) // .../backend/internal/testutil/testutil.go
	return filepath.Join(filepath.Dir(file), "..", "..", "migrations")
}

// NewDB returns a fresh SQLite database with all migrations applied. The
// underlying file lives in t.TempDir() and is cleaned up automatically.
func NewDB(t *testing.T) *sql.DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := database.Migrate(db, migrationsDir()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// User is the minimal fixture returned by MakeUser: a user, their personal
// household, and an owner membership in that household — exactly what the
// real Register handler creates.
type User struct {
	ID          string
	Username    string
	HouseholdID string
}

// MakeUser inserts a user + personal household + owner membership in one tx,
// mirroring AuthHandler.Register. Use it in tests that need a "ready" user
// without going through the full HTTP flow.
func MakeUser(t *testing.T, db *sql.DB, username string) User {
	t.Helper()
	return makeUserAt(t, db, username, time.Now().UTC())
}

// MakeUserAt is MakeUser with an explicit joined_at — useful when the test
// needs to control the most-recently-joined ordering used by RequireHousehold.
func MakeUserAt(t *testing.T, db *sql.DB, username string, joinedAt time.Time) User {
	t.Helper()
	return makeUserAt(t, db, username, joinedAt)
}

func makeUserAt(t *testing.T, db *sql.DB, username string, joinedAt time.Time) User {
	t.Helper()
	userID := uuid.NewString()
	householdID := uuid.NewString()

	hash, err := auth.HashPassword("test-password-123")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}

	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(
		`INSERT INTO users (id, username, password_hash, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		userID, username, hash, joinedAt, joinedAt,
	); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO households (id, name, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?)`,
		householdID, username+"'s Household", userID, joinedAt, joinedAt,
	); err != nil {
		t.Fatalf("insert household: %v", err)
	}
	if _, err := tx.Exec(
		`INSERT INTO memberships (user_id, household_id, role, joined_at)
		 VALUES (?, ?, ?, ?)`,
		userID, householdID, models.RoleOwner, joinedAt,
	); err != nil {
		t.Fatalf("insert membership: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}

	return User{ID: userID, Username: username, HouseholdID: householdID}
}

// AddMembership joins an existing user to an existing household with the
// given role and joined_at. Returns nothing because both ids are already
// known to the caller.
func AddMembership(t *testing.T, db *sql.DB, userID, householdID, role string, joinedAt time.Time) {
	t.Helper()
	if _, err := db.Exec(
		`INSERT INTO memberships (user_id, household_id, role, joined_at)
		 VALUES (?, ?, ?, ?)`,
		userID, householdID, role, joinedAt,
	); err != nil {
		t.Fatalf("insert membership: %v", err)
	}
}

// AccessToken mints a valid access JWT for u using JWTSecret. The TTL is
// generous (1 h) so a test won't accidentally race against expiry.
func AccessToken(t *testing.T, u User) string {
	t.Helper()
	tok, err := auth.GenerateAccessToken(JWTSecret, u.ID, u.Username, time.Hour)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return tok
}
