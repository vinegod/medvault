package handlers

import (
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
		api.WriteJSON(w, http.StatusOK, []database.Drug{})
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
	results := make([]database.Drug, 0, limit)
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
		rows, err := database.SearchDrugs(r.Context(), db, country, q, remaining)
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
