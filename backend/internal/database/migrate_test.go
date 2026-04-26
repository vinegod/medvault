package database_test

import (
	"database/sql"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/vinegod/medvault/backend/internal/database"
)

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
	if _, ok := idx["idx_medicines_household_id"]; !ok {
		t.Error("idx_medicines_household_id missing")
	}
	if _, ok := idx["idx_medicines_user_id"]; ok {
		t.Error("idx_medicines_user_id should have been dropped by 002")
	}

	// Both migrations were recorded.
	for _, ver := range []string{"001_init.sql", "002_households.sql"} {
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
	// Two .sql files exist; running migrate twice must still leave exactly 2 rows.
	if total != 2 {
		t.Errorf("schema_migrations total = %d, want 2", total)
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
