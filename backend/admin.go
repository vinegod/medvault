package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"fmt"
	"io"

	"github.com/vinegod/medvault/backend/internal/auth"
	"github.com/vinegod/medvault/backend/internal/database"
)

const adminUsage = "usage: medvault reset-password|grant-admin|revoke-admin <username>"

// runAdmin runs an admin subcommand against the database at DB_PATH.
// Supported, each taking a username:
//   - reset-password: sets a random password, revokes the user's sessions
//     and prints the new password to stdout.
//   - grant-admin, revoke-admin: change server admin rights.
func runAdmin(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	if len(args) != 2 {
		return errors.New(adminUsage)
	}
	cmd, username := args[0], args[1]
	switch cmd {
	case "reset-password", "grant-admin", "revoke-admin":
	default:
		return errors.New(adminUsage)
	}

	dbPath := getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./data/medvault.db"
	}
	migrationsDir := getenv("MIGRATIONS_DIR")
	if migrationsDir == "" {
		migrationsDir = "./migrations"
	}

	db, err := database.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close() //nolint:errcheck // read-mostly CLI; nothing to flush

	if err := database.Migrate(db, migrationsDir); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	if cmd == "reset-password" {
		return resetPassword(ctx, db, username, stdout)
	}
	admin := cmd == "grant-admin"
	if err := database.SetAdmin(ctx, db, username, admin); err != nil {
		return fmt.Errorf("%s %q: %w", cmd, username, err)
	}
	msg := "%s is now a server admin\n"
	if !admin {
		msg = "%s is no longer a server admin\n"
	}
	if _, err := fmt.Fprintf(stdout, msg, username); err != nil {
		return fmt.Errorf("print result: %w", err)
	}
	return nil
}

func resetPassword(ctx context.Context, db *sql.DB, username string, stdout io.Writer) error {
	password := rand.Text()[:16]
	hash, err := auth.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	if err := database.SetPassword(ctx, db, username, hash); err != nil {
		return fmt.Errorf("reset password for %q: %w", username, err)
	}

	if _, err := fmt.Fprintf(stdout, "New password for %s: %s\n", username, password); err != nil {
		return fmt.Errorf("print password: %w", err)
	}
	return nil
}
