package database

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Migrate applies any unapplied *.sql files from migrationsDir in
// lexicographic order. Applied versions are tracked in schema_migrations
// so the function is safe to call on every startup.
func Migrate(db *sql.DB, migrationsDir string) error {
	if err := ensureMigrationsTable(db); err != nil {
		return err
	}

	files, err := sqlFiles(migrationsDir)
	if err != nil {
		return err
	}

	for _, name := range files {
		applied, err := isApplied(db, name)
		if err != nil {
			return err
		}
		if applied {
			continue
		}

		content, err := os.ReadFile(filepath.Join(migrationsDir, name))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}

		if err := apply(db, name, string(content)); err != nil {
			return err
		}
	}

	return nil
}

func ensureMigrationsTable(db *sql.DB) error {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version    TEXT     PRIMARY KEY,
		applied_at DATETIME NOT NULL DEFAULT (datetime('now'))
	)`)
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	return nil
}

func sqlFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	return names, nil
}

func isApplied(db *sql.DB, version string) (bool, error) {
	var n int
	err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("check migration %s: %w", version, err)
	}

	return n > 0, nil
}

// apply splits the SQL file into PRAGMA statements (which SQLite forbids
// inside a transaction) and everything else (DDL, run inside a transaction
// for atomicity). On success it records the version in schema_migrations.
func apply(db *sql.DB, name, content string) error {
	pragmas, ddl := splitByKind(content)

	for _, stmt := range pragmas {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("migration %s pragma: %w", name, err)
		}
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}

	for _, stmt := range ddl {
		if _, err := tx.Exec(stmt); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}

	if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, name); err != nil {
		_ = tx.Rollback()
		return fmt.Errorf("record migration %s: %w", name, err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration %s: %w", name, err)
	}

	return nil
}

// splitByKind parses SQL content into PRAGMA and non-PRAGMA statements,
// correctly ignoring semicolons that appear inside -- line comments.
func splitByKind(content string) (pragmas, rest []string) {
	for _, stmt := range splitStatements(content) {
		if strings.HasPrefix(strings.ToUpper(stmt), "PRAGMA") {
			pragmas = append(pragmas, stmt)
		} else {
			rest = append(rest, stmt)
		}
	}

	return pragmas, rest
}

// splitStatements splits SQL by semicolons while ignoring semicolons inside
// -- line comments. Each returned string is a trimmed, non-empty statement.
func splitStatements(content string) []string {
	var stmts []string
	var current strings.Builder
	inLineComment := false

	for i := 0; i < len(content); i++ {
		ch := content[i]

		if inLineComment {
			if ch == '\n' {
				inLineComment = false
				current.WriteByte(ch)
			}
			// drop comment content — SQLite would accept it too, but keeping
			// it out keeps the statements clean for the PRAGMA prefix check.
			continue
		}

		// Detect start of -- line comment.
		if ch == '-' && i+1 < len(content) && content[i+1] == '-' {
			inLineComment = true
			i++ // skip second '-'
			continue
		}

		if ch == ';' {
			if stmt := strings.TrimSpace(current.String()); stmt != "" {
				stmts = append(stmts, stmt)
			}
			current.Reset()
			continue
		}

		current.WriteByte(ch)
	}

	if stmt := strings.TrimSpace(current.String()); stmt != "" {
		stmts = append(stmts, stmt)
	}

	return stmts
}
