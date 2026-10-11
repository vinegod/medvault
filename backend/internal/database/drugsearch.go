package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Drug is one row of a country drug DB. Pointer fields are omitted when
// null in the source DB.
type Drug struct {
	Name        string  `json:"name"`
	INN         *string `json:"inn,omitempty"`
	Ingredients *string `json:"ingredients,omitempty"`
	PharmaGroup *string `json:"pharma_group,omitempty"`
	Country     string  `json:"country"`
}

// LookupExact returns the drug whose name equals name (case-insensitive) in
// the given country's DB. (nil, nil) when nothing matches or the country
// isn't configured.
func (r *DrugRegistry) LookupExact(ctx context.Context, country, name string) (*Drug, error) {
	if r == nil || strings.TrimSpace(name) == "" {
		return nil, nil
	}
	// Uppercase so a lowercase user-typed name still matches the uppercase
	// Cyrillic trade names stored in the drug DB (SQLite COLLATE NOCASE is
	// ASCII-only; normalising in Go covers the full Unicode range).
	name = strings.ToUpper(strings.TrimSpace(name))
	db, err := r.Get(country)
	if err != nil {
		return nil, fmt.Errorf("drug registry get %q: %w", country, err)
	}
	if db == nil {
		return nil, nil
	}

	row := db.QueryRowContext(ctx,
		`SELECT name, inn, ingredients, pharma_group FROM drugs WHERE name = ? COLLATE NOCASE LIMIT 1`,
		name,
	)
	d := Drug{Country: country}
	if err := row.Scan(&d.Name, &d.INN, &d.Ingredients, &d.PharmaGroup); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan drug row: %w", err)
	}
	return &d, nil
}

// SearchDrugs runs the ranked search against one drug DB, matching q on
// name, INN, and ingredients.
//
// Ranking tiers (ORDER BY):
//
//	0 — name starts with q  (highest)
//	1 — INN or ingredients starts with q
//	2 — any substring match (lowest)
//
// Case strategy: trade names are uppercase Cyrillic in the source data, so q
// is uppercased for name comparisons. INN and ingredients are lowercase or
// mixed, so q is lowercased for those columns. SQLite's LIKE handles ASCII
// case-insensitively; Cyrillic requires the normalisation in Go.
func SearchDrugs(ctx context.Context, db *sql.DB, country, q string, limit int) ([]Drug, error) {
	qUp := strings.ToUpper(q)
	qLow := strings.ToLower(q)
	esc := escapeLike(qUp)
	escLow := escapeLike(qLow)

	rows, err := db.QueryContext(ctx, `
		SELECT name, inn, ingredients, pharma_group FROM drugs
		WHERE name LIKE ? ESCAPE '\'
		   OR inn LIKE ? ESCAPE '\'
		   OR ingredients LIKE ? ESCAPE '\'
		ORDER BY
			CASE
				WHEN name LIKE ? ESCAPE '\' THEN 0
				WHEN inn LIKE ? ESCAPE '\' OR ingredients LIKE ? ESCAPE '\' THEN 1
				ELSE 2
			END,
			name COLLATE NOCASE
		LIMIT ?`,
		"%"+esc+"%",    // name WHERE
		"%"+escLow+"%", // inn WHERE
		"%"+escLow+"%", // ingredients WHERE
		esc+"%",        // name ORDER prefix
		escLow+"%",     // inn ORDER prefix
		escLow+"%",     // ingredients ORDER prefix
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query drugs: %w", err)
	}
	return scanDrugs(rows, country)
}

// DrugsByGroup returns drugs sharing the given INN or pharma group (either
// may be empty, not both), excluding the drug named exclude.
func DrugsByGroup(
	ctx context.Context,
	db *sql.DB,
	country, inn, pharmaGroup, exclude string,
	limit int,
) ([]Drug, error) {
	inn = strings.ToLower(strings.TrimSpace(inn))
	rows, err := db.QueryContext(ctx, `
		SELECT name, inn, ingredients, pharma_group FROM drugs
		WHERE ((? != '' AND inn = ? COLLATE NOCASE) OR (? != '' AND pharma_group = ?))
		  AND name != ?
		ORDER BY CASE WHEN inn = ? COLLATE NOCASE THEN 0 ELSE 1 END, name
		LIMIT ?`,
		inn, inn, pharmaGroup, pharmaGroup, strings.ToUpper(exclude), inn, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query drugs by group: %w", err)
	}
	return scanDrugs(rows, country)
}

func scanDrugs(rows *sql.Rows, country string) ([]Drug, error) {
	defer rows.Close() //nolint:errcheck // error surfaced by rows.Err()

	var out []Drug
	for rows.Next() {
		d := Drug{Country: country}
		if err := rows.Scan(&d.Name, &d.INN, &d.Ingredients, &d.PharmaGroup); err != nil {
			return nil, fmt.Errorf("scan drug row: %w", err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate drug rows: %w", err)
	}
	return out, nil
}

// escapeLike escapes SQLite LIKE wildcards so user input is matched literally.
func escapeLike(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
