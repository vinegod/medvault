package main

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vinegod/medvault/backend/internal/auth"
	"github.com/vinegod/medvault/backend/internal/database"
	"github.com/vinegod/medvault/backend/internal/testutil"
)

func TestRunAdmin_ResetPassword(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "admin.db")
	db, err := database.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := database.Migrate(db, "./migrations"); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	user := testutil.MakeUser(t, db, "alice")
	if _, err := db.Exec(
		`INSERT INTO refresh_tokens (id, user_id, token_hash, expires_at) VALUES ('t1', ?, 'h', ?)`,
		user.ID, time.Now().Add(time.Hour),
	); err != nil {
		t.Fatalf("insert token: %v", err)
	}

	env := map[string]string{"DB_PATH": dbPath, "MIGRATIONS_DIR": "./migrations"}
	var out bytes.Buffer
	if err := runAdmin(context.Background(), []string{"reset-password", "alice"},
		func(k string) string { return env[k] }, &out); err != nil {
		t.Fatalf("runAdmin: %v", err)
	}

	password := strings.TrimSpace(out.String()[strings.LastIndex(out.String(), ":")+1:])
	var hash string
	var revoked *string
	if err := db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, user.ID).Scan(&hash); err != nil {
		t.Fatalf("read hash: %v", err)
	}
	if !auth.CheckPassword(password, hash) {
		t.Errorf("printed password %q does not match the stored hash", password)
	}
	if err := db.QueryRow(`SELECT revoked_at FROM refresh_tokens WHERE id = 't1'`).Scan(&revoked); err != nil {
		t.Fatalf("read token: %v", err)
	}
	if revoked == nil {
		t.Error("refresh token not revoked")
	}
}

func TestRunAdmin_Errors(t *testing.T) {
	env := map[string]string{"DB_PATH": filepath.Join(t.TempDir(), "x.db"), "MIGRATIONS_DIR": "./migrations"}
	getenv := func(k string) string { return env[k] }

	for _, args := range [][]string{{"bogus"}, {"reset-password"}, {"reset-password", "nobody"}} {
		if err := runAdmin(context.Background(), args, getenv, &bytes.Buffer{}); err == nil {
			t.Errorf("runAdmin(%q) = nil, want error", args)
		}
	}
}
