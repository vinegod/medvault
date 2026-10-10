package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"

	"github.com/vinegod/medvault/backend/internal/auth"
	"github.com/vinegod/medvault/backend/internal/database"
)

const adminUsage = "usage: medvault reset-password <username>"

// runAdmin runs an admin subcommand against the database at DB_PATH.
// Supported: reset-password <username>, which sets a random password,
// revokes the user's sessions and prints the new password to stdout.
func runAdmin(ctx context.Context, args []string, getenv func(string) string, stdout io.Writer) error {
	if len(args) != 2 || args[0] != "reset-password" {
		return errors.New(adminUsage)
	}
	username := args[1]

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
