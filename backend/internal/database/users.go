package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrUserNotFound is returned when no user has the given username.
var ErrUserNotFound = errors.New("user not found")

// SetAdmin grants or revokes server admin rights for username.
func SetAdmin(ctx context.Context, db *sql.DB, username string, admin bool) error {
	res, err := db.ExecContext(ctx,
		`UPDATE users SET is_admin = ?, updated_at = ? WHERE username = ?`,
		admin, time.Now().UTC(), username,
	)
	if err != nil {
		return fmt.Errorf("update is_admin: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return ErrUserNotFound
	}
	return nil
}

// IsAdmin reports whether the user is a server admin. An unknown user is not.
func IsAdmin(ctx context.Context, db *sql.DB, userID string) (bool, error) {
	var admin bool
	err := db.QueryRowContext(ctx, `SELECT is_admin FROM users WHERE id = ?`, userID).Scan(&admin)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("query is_admin: %w", err)
	}
	return admin, nil
}

// SetPassword replaces the user's bcrypt hash and revokes all of their
// refresh tokens in one transaction.
func SetPassword(ctx context.Context, db *sql.DB, username, hash string) error {
	now := time.Now().UTC()

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit

	var userID string
	err = tx.QueryRowContext(ctx,
		`UPDATE users SET password_hash = ?, updated_at = ? WHERE username = ? RETURNING id`,
		hash, now, username,
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUserNotFound
	}
	if err != nil {
		return fmt.Errorf("update password: %w", err)
	}

	if _, err := tx.ExecContext(ctx,
		`UPDATE refresh_tokens SET revoked_at = ? WHERE user_id = ? AND revoked_at IS NULL`,
		now, userID,
	); err != nil {
		return fmt.Errorf("revoke refresh tokens: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
