package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/vinegod/medvault/backend/internal/api"
	"github.com/vinegod/medvault/backend/internal/database"
	"github.com/vinegod/medvault/backend/internal/models"
)

// DrugHandler answers /api/drugs/search against the lazy DrugRegistry.
type DrugHandler struct {
	Registry *database.DrugRegistry
}

// DrugResult is the shape of one /api/drugs/search row. Pointer fields
// are omitted when null in the source DB.
type DrugResult struct {
	Name        string  `json:"name"`
	INN         *string `json:"inn,omitempty"`
	Ingredients *string `json:"ingredients,omitempty"`
	PharmaGroup *string `json:"pharma_group,omitempty"`
	Country     string  `json:"country"`
}

const (
	drugSearchLimit    = 10 // default cap when no ?limit= is given
	drugSearchMaxLimit = 50 // hard ceiling regardless of caller request
)

// parseLimit reads the optional ?limit= query parameter and clamps it to
// [1, drugSearchMaxLimit]. Missing or unparseable values fall back to the
// default. The cap protects the server from a caller asking for thousands
// of rows in one request.
func parseLimit(raw string) int {
	if raw == "" {
		return drugSearchLimit
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return drugSearchLimit
	}
	if n > drugSearchMaxLimit {
		return drugSearchMaxLimit
	}
	return n
}

// Search handles GET /api/drugs/search?q=Q[&country=CC].
//   - q < 2 chars → 200 with empty list (no DB hit).
//   - registry empty → 404 NOT_FOUND ("drug database not configured").
//   - country=XX where XX isn't registered → 404 NOT_FOUND ("country not configured").
//   - all configured DBs fail to open → 503 SERVICE_UNAVAILABLE.
//   - results merged across countries (or restricted when country= is given),
//     prefix matches first, then substring, total capped at drugSearchLimit.
func (h *DrugHandler) Search(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if utf8.RuneCountInString(q) < 2 {
		api.WriteJSON(w, http.StatusOK, []DrugResult{})
		return
	}

	registered := h.Registry.Countries()
	if len(registered) == 0 {
		api.WriteError(w, http.StatusNotFound, models.CodeNotFound, "drug database not configured")
		return
	}

	countries := registered
	if requested := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("country"))); requested != "" {
		// Validate the requested country is one we know about. Without this
		// check we'd silently produce an empty result set or fall through to
		// the 503 path, both of which mislead the caller.
		known := false
		for _, c := range registered {
			if c == requested {
				known = true
				break
			}
		}
		if !known {
			api.WriteError(w, http.StatusNotFound, models.CodeNotFound, "country not configured")
			return
		}
		countries = []string{requested}
	}

	limit := parseLimit(r.URL.Query().Get("limit"))
	results := make([]DrugResult, 0, limit)
	var anyServed bool

	for _, country := range countries {
		db, err := h.Registry.Get(country)
		if err != nil {
			slog.Warn("drug registry get", "country", country, "err", err)
			continue
		}
		if db == nil {
			continue
		}
		anyServed = true

		remaining := limit - len(results)
		if remaining <= 0 {
			break
		}
		rows, err := queryDrugs(r.Context(), db, q, country, remaining)
		if err != nil {
			slog.Error("drug search query", "country", country, "err", err)
			api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
			return
		}
		results = append(results, rows...)
	}

	if !anyServed {
		// Registered countries exist but every Get() returned an error: the
		// data is configured but currently unusable. 503 communicates "try
		// again later" rather than the misleading 500 "server bug".
		api.WriteError(
			w,
			http.StatusServiceUnavailable,
			"UNAVAILABLE",
			"drug database is currently unavailable",
		)
		return
	}

	api.WriteJSON(w, http.StatusOK, results)
}

// LookupExact returns the single drug row whose `name` equals `name`
// (case-insensitive) in the given country's DB. (nil, nil) when nothing
// matches or the country isn't configured. Used by the sync enrichment path.
func LookupExact(ctx context.Context, reg *database.DrugRegistry, country, name string) (*DrugResult, error) {
	if reg == nil || strings.TrimSpace(name) == "" {
		return nil, nil
	}
	// Uppercase so a lowercase user-typed name still matches the uppercase
	// Cyrillic trade names stored in the drug DB (SQLite COLLATE NOCASE is
	// ASCII-only; normalising in Go covers the full Unicode range).
	name = strings.ToUpper(strings.TrimSpace(name))
	db, err := reg.Get(country)
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
	var d DrugResult
	d.Country = country
	if err := row.Scan(&d.Name, &d.INN, &d.Ingredients, &d.PharmaGroup); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan drug row: %w", err)
	}
	return &d, nil
}

// queryDrugs runs the ranked search against one DB, matching on name, INN,
// and ingredients.
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
func queryDrugs(ctx context.Context, db *sql.DB, q, country string, limit int) ([]DrugResult, error) {
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
	defer rows.Close() //nolint:errcheck // error surfaced by rows.Err()

	var out []DrugResult
	for rows.Next() {
		var d DrugResult
		d.Country = country
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
