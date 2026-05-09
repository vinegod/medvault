package database

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// DrugRegistry tracks which country drug DBs exist and opens each one
// lazily on its first request, caching the handle for the process lifetime.
//
// The motivation is that drug-DB usage is sparse — most requests don't touch
// it — so paying the open cost only on first hit avoids holding file handles
// open for installations that never autofill.
type DrugRegistry struct {
	paths map[string]string

	mu     sync.Mutex
	opened map[string]*registryEntry
}

type registryEntry struct {
	db  *sql.DB
	err error
}

// NewDrugRegistry scans <dir>/<country>/drugs.db and records the paths.
// No DB is opened here — first call to Get(country) does that. A missing
// dir is treated as "no countries registered" and returns an empty registry.
func NewDrugRegistry(dir string) *DrugRegistry {
	r := &DrugRegistry{
		paths:  make(map[string]string),
		opened: make(map[string]*registryEntry),
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return r
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), "drugs.db")
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			r.paths[strings.ToLower(e.Name())] = path
		}
	}
	return r
}

// Countries returns the registered country codes in deterministic order.
func (r *DrugRegistry) Countries() []string {
	out := make([]string, 0, len(r.paths))
	for c := range r.paths {
		out = append(out, c)
	}
	sort.Strings(out)
	return out
}

// Get returns the lazily-opened *sql.DB for the given country.
//   - (nil, nil)   → country is not registered (no DB file at boot).
//   - (nil, error) → first open failed; the error is cached so subsequent
//     calls return immediately without retrying.
//   - (db,  nil)   → ready to query.
func (r *DrugRegistry) Get(country string) (*sql.DB, error) {
	country = strings.ToLower(country)

	path, ok := r.paths[country]
	if !ok {
		return nil, nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if entry, ok := r.opened[country]; ok {
		return entry.db, entry.err
	}

	db, err := openReadOnly(path)
	r.opened[country] = &registryEntry{db: db, err: err}
	return db, err
}

// Close shuts every opened DB handle.
func (r *DrugRegistry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var errs []string
	for country, entry := range r.opened {
		if entry.db != nil {
			if err := entry.db.Close(); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", country, err))
			}
		}
	}
	if len(errs) > 0 {
		return errors.New("close drug DBs: " + strings.Join(errs, "; "))
	}
	return nil
}

func openReadOnly(path string) (*sql.DB, error) {
	dsn := fmt.Sprintf("file:%s?mode=ro&cache=shared", path)
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping %s: %w", path, err)
	}
	return db, nil
}

// JoinDrugNotes is the canonical formatter for the medicine `notes` field
// when it's filled from a drug DB row. Same shape on client and server:
// INN on the first line, pharma group on the second, either part may be
// omitted if missing.
func JoinDrugNotes(inn, pharmaGroup string) string {
	parts := make([]string, 0, 2)
	if s := strings.TrimSpace(inn); s != "" {
		parts = append(parts, s)
	}
	if s := strings.TrimSpace(pharmaGroup); s != "" {
		parts = append(parts, s)
	}
	return strings.Join(parts, "\n")
}
