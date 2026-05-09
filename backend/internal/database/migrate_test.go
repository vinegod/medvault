package database_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/vinegod/medvault/backend/internal/database"
)

// migrationFileCount returns the number of *.sql files in dir, so tests
// that count schema_migrations rows don't drift each time we add a migration.
func migrationFileCount(t *testing.T, dir string) int {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read migrations dir: %v", err)
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			n++
		}
	}
	return n
}

// migrationsDir resolves to backend/migrations from this test file's location.
func migrationsDir(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0) // .../backend/internal/database/migrate_test.go
	return filepath.Join(filepath.Dir(file), "..", "..", "migrations")
}

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	db, err := database.Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestMigrate_FreshInstall runs every migration on an empty DB and asserts
// the resulting schema matches what handlers expect.
func TestMigrate_FreshInstall(t *testing.T) {
	db := openDB(t)
	dir := migrationsDir(t)

	if err := database.Migrate(db, dir); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// All required tables exist.
	for _, table := range []string{
		"users", "medicines", "refresh_tokens",
		"households", "memberships", "household_invites",
		"schema_migrations",
	} {
		if !tableExists(t, db, table) {
			t.Errorf("expected table %q to exist", table)
		}
	}

	// medicines has the new columns; user_id is gone.
	cols := columnSet(t, db, "medicines")
	for _, want := range []string{"household_id", "created_by"} {
		if _, ok := cols[want]; !ok {
			t.Errorf("medicines.%s missing — 002_households.sql did not apply correctly", want)
		}
	}
	if _, stillThere := cols["user_id"]; stillThere {
		t.Error("medicines.user_id should have been renamed to created_by by 002")
	}

	// Index swap: by-user index removed, by-household added.
	idx := indexSet(t, db, "medicines")
	if _, ok := idx["idx_medicines_household_updated"]; !ok {
		t.Error("idx_medicines_household_updated missing")
	}
	if _, ok := idx["idx_medicines_user_id"]; ok {
		t.Error("idx_medicines_user_id should have been dropped by 002")
	}

	// 003 adds users.active_household_id; 004 adds household_invites.revoked_at.
	if _, ok := columnSet(t, db, "users")["active_household_id"]; !ok {
		t.Error("users.active_household_id missing — 003 did not apply")
	}
	if _, ok := columnSet(t, db, "household_invites")["revoked_at"]; !ok {
		t.Error("household_invites.revoked_at missing — 004 did not apply")
	}

	// Every migration file present on disk was recorded.
	for _, ver := range []string{
		"001_init.sql", "002_households.sql",
		"003_active_household.sql", "004_invite_revocation.sql",
	} {
		var n int
		if err := db.QueryRow(
			`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, ver,
		).Scan(&n); err != nil {
			t.Fatalf("query schema_migrations: %v", err)
		}
		if n != 1 {
			t.Errorf("schema_migrations row count for %s = %d, want 1", ver, n)
		}
	}
}

// TestMigrate_Idempotent runs Migrate twice and asserts the second call is
// a no-op (no errors, no duplicate version rows).
func TestMigrate_Idempotent(t *testing.T) {
	db := openDB(t)
	dir := migrationsDir(t)

	if err := database.Migrate(db, dir); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	if err := database.Migrate(db, dir); err != nil {
		t.Fatalf("second migrate: %v", err)
	}

	var total int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&total); err != nil {
		t.Fatalf("count: %v", err)
	}
	want := migrationFileCount(t, dir)
	if total != want {
		t.Errorf("schema_migrations total = %d, want %d (one row per *.sql file)", total, want)
	}
}

// TestMigrate_ForeignKeysEnforced checks the DSN actually turned on FK
// enforcement, since the household-cascade behavior depends on it.
func TestMigrate_ForeignKeysEnforced(t *testing.T) {
	db := openDB(t)
	if err := database.Migrate(db, migrationsDir(t)); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	// Inserting a membership for a user that doesn't exist must fail.
	_, err := db.Exec(
		`INSERT INTO memberships (user_id, household_id, role, joined_at)
		 VALUES (?, ?, 'owner', datetime('now'))`,
		"ghost-user", "ghost-household",
	)
	if err == nil {
		t.Fatal("expected FK violation, got nil error")
	}
}

// --- helpers ---

func tableExists(t *testing.T, db *sql.DB, name string) bool {
	t.Helper()
	var got string
	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, name,
	).Scan(&got)
	if err == sql.ErrNoRows {
		return false
	}
	if err != nil {
		t.Fatalf("table lookup %s: %v", name, err)
	}
	return got == name
}

func columnSet(t *testing.T, db *sql.DB, table string) map[string]struct{} {
	t.Helper()
	// PRAGMA can't be parameterised with ?, but `table` here is a hard-coded
	// test fixture, not user input.
	rows, err := db.Query(`PRAGMA table_info(` + table + `)`)
	if err != nil {
		t.Fatalf("pragma table_info: %v", err)
	}
	defer rows.Close() //nolint:errcheck // error surfaced by rows.Err()

	cols := map[string]struct{}{}
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notnull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan column: %v", err)
		}
		cols[name] = struct{}{}
	}
	return cols
}

func indexSet(t *testing.T, db *sql.DB, table string) map[string]struct{} {
	t.Helper()
	rows, err := db.Query(`PRAGMA index_list(` + table + `)`)
	if err != nil {
		t.Fatalf("pragma index_list: %v", err)
	}
	defer rows.Close() //nolint:errcheck // error surfaced by rows.Err()

	idx := map[string]struct{}{}
	for rows.Next() {
		var (
			seq     int
			name    string
			unique  int
			origin  string
			partial int
		)
		if err := rows.Scan(&seq, &name, &unique, &origin, &partial); err != nil {
			t.Fatalf("scan index: %v", err)
		}
		idx[name] = struct{}{}
	}
	return idx
}
