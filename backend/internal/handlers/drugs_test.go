package handlers_test

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/go-chi/chi/v5"
	_ "github.com/mattn/go-sqlite3"
	"github.com/vinegod/medvault/backend/internal/database"
	"github.com/vinegod/medvault/backend/internal/handlers"
)

// makeFixtureDrugDB writes a small drugs.db at <dir>/<country>/drugs.db with
// the rows in `seed`. Each tuple is (name, inn, ingredients, pharma_group);
// "" maps to NULL on insert.
func makeFixtureDrugDB(t *testing.T, dir, country string, seed [][4]string) {
	t.Helper()
	countryDir := filepath.Join(dir, country)
	if err := os.MkdirAll(countryDir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	db, err := sql.Open("sqlite3", filepath.Join(countryDir, "drugs.db"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer db.Close() //nolint:errcheck // best-effort cleanup in tests
	if _, err := db.Exec(`
		CREATE TABLE drugs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			inn TEXT,
			ingredients TEXT,
			pharma_group TEXT
		);
		CREATE INDEX idx_drugs_name ON drugs(name COLLATE NOCASE);
	`); err != nil {
		t.Fatalf("schema: %v", err)
	}
	for _, row := range seed {
		args := make([]any, 4)
		for i, v := range row {
			if v == "" {
				args[i] = nil
			} else {
				args[i] = v
			}
		}
		if _, err := db.Exec(
			`INSERT INTO drugs (name, inn, ingredients, pharma_group) VALUES (?, ?, ?, ?)`,
			args...,
		); err != nil {
			t.Fatalf("insert: %v", err)
		}
	}
}

func newDrugRouter(t *testing.T, reg *database.DrugRegistry) *httptest.Server {
	t.Helper()
	h := &handlers.DrugHandler{Registry: reg}
	r := chi.NewRouter()
	r.Get("/api/drugs/search", h.Search)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

func decodeSearch(t *testing.T, body io.Reader) []map[string]any {
	t.Helper()
	var env struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.NewDecoder(body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return env.Data
}

func TestDrugsSearch_EmptyRegistry_404(t *testing.T) {
	reg := database.NewDrugRegistry(t.TempDir()) // empty dir → no countries
	srv := newDrugRouter(t, reg)

	res, err := http.Get(srv.URL + "/api/drugs/search?q=абикл")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close() //nolint:errcheck // best-effort cleanup in tests
	if res.StatusCode != http.StatusNotFound {
		t.Fatalf("status: got %d want 404", res.StatusCode)
	}
}

func TestDrugsSearch_ShortQuery_EmptyList(t *testing.T) {
	dir := t.TempDir()
	makeFixtureDrugDB(t, dir, "ua", [][4]string{
		{"АБИКЛАВ", "амоксицилін", "Амоксицилін; Клавуланова кислота", "Антибактеріальні засоби"},
	})
	reg := database.NewDrugRegistry(dir)
	srv := newDrugRouter(t, reg)

	res, err := http.Get(srv.URL + "/api/drugs/search?q=а")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close() //nolint:errcheck // best-effort cleanup in tests
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status: got %d want 200", res.StatusCode)
	}
	if got := decodeSearch(t, res.Body); len(got) != 0 {
		t.Fatalf("results: got %d want 0", len(got))
	}
}

func TestDrugsSearch_PrefixWinsOverSubstring(t *testing.T) {
	dir := t.TempDir()
	makeFixtureDrugDB(t, dir, "ua", [][4]string{
		{"ZZ-IBUPROFEN", "ibuprofen", "Ibuprofen", "NSAIDs"},  // substring
		{"IBUPROFEN-100", "ibuprofen", "Ibuprofen", "NSAIDs"}, // prefix
	})
	reg := database.NewDrugRegistry(dir)
	srv := newDrugRouter(t, reg)

	res, err := http.Get(srv.URL + "/api/drugs/search?q=IBUPROFEN")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close() //nolint:errcheck // best-effort cleanup in tests
	rows := decodeSearch(t, res.Body)
	if len(rows) != 2 {
		t.Fatalf("rows: got %d want 2", len(rows))
	}
	if rows[0]["name"] != "IBUPROFEN-100" {
		t.Fatalf("first row: got %q want IBUPROFEN-100 (prefix should win)", rows[0]["name"])
	}
}

func TestDrugsSearch_AllSchemaColumnsSurfaced(t *testing.T) {
	dir := t.TempDir()
	makeFixtureDrugDB(t, dir, "ua", [][4]string{
		{"АБИКЛАВ", "амоксицилін", "Амоксицилін; Клавуланова кислота", "Антибактеріальні засоби"},
	})
	reg := database.NewDrugRegistry(dir)
	srv := newDrugRouter(t, reg)

	res, err := http.Get(srv.URL + "/api/drugs/search?q=АБИКЛАВ")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close() //nolint:errcheck // best-effort cleanup in tests
	rows := decodeSearch(t, res.Body)
	if len(rows) != 1 {
		t.Fatalf("rows: got %d want 1", len(rows))
	}
	row := rows[0]
	for _, k := range []string{"name", "inn", "ingredients", "pharma_group", "country"} {
		if _, ok := row[k]; !ok {
			t.Errorf("missing field %q in result", k)
		}
	}
	if row["country"] != "ua" {
		t.Errorf("country: got %v want ua", row["country"])
	}
}

func TestDrugsSearch_MultiCountryMerge(t *testing.T) {
	dir := t.TempDir()
	makeFixtureDrugDB(t, dir, "ua", [][4]string{
		{"ASPIRIN-UA", "ацетилсаліцилова кислота", "ASA", "NSAIDs"},
	})
	makeFixtureDrugDB(t, dir, "gb", [][4]string{
		{"ASPIRIN-GB", "acetylsalicylic acid", "ASA", "NSAIDs"},
	})
	reg := database.NewDrugRegistry(dir)
	srv := newDrugRouter(t, reg)

	res, err := http.Get(srv.URL + "/api/drugs/search?q=ASPIRIN")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close() //nolint:errcheck // best-effort cleanup in tests
	rows := decodeSearch(t, res.Body)
	if len(rows) != 2 {
		t.Fatalf("rows: got %d want 2", len(rows))
	}
	countries := map[string]bool{}
	for _, r := range rows {
		countries[r["country"].(string)] = true
	}
	if !countries["ua"] || !countries["gb"] {
		t.Fatalf("countries: got %v want both ua and gb", countries)
	}
}

func TestDrugsSearch_CountryFilter(t *testing.T) {
	dir := t.TempDir()
	makeFixtureDrugDB(t, dir, "ua", [][4]string{{"ASPIRIN-UA", "", "", ""}})
	makeFixtureDrugDB(t, dir, "gb", [][4]string{{"ASPIRIN-GB", "", "", ""}})
	reg := database.NewDrugRegistry(dir)
	srv := newDrugRouter(t, reg)

	res, err := http.Get(srv.URL + "/api/drugs/search?q=ASPIRIN&country=gb")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close() //nolint:errcheck // best-effort cleanup in tests
	rows := decodeSearch(t, res.Body)
	if len(rows) != 1 || rows[0]["name"] != "ASPIRIN-GB" {
		t.Fatalf("filter: got %v want exactly ASPIRIN-GB", rows)
	}
}

func TestDrugsSearch_INNMatchesCyrillicLowercase(t *testing.T) {
	dir := t.TempDir()
	// INN stored in lowercase Cyrillic as the UA register has it.
	makeFixtureDrugDB(t, dir, "ua", [][4]string{
		{"АБИКЛАВ", "амоксицилін", "Амоксицилін; Клавуланова кислота", "Антибактеріальні засоби"},
		{"ПАРАЦЕТАМОЛ", "парацетамол", "Парацетамол", "Аналгетики"},
	})
	reg := database.NewDrugRegistry(dir)
	srv := newDrugRouter(t, reg)

	res, err := http.Get(srv.URL + "/api/drugs/search?q=амокс")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close() //nolint:errcheck // best-effort cleanup in tests
	rows := decodeSearch(t, res.Body)
	if len(rows) != 1 {
		t.Fatalf("INN Cyrillic: got %d rows, want 1", len(rows))
	}
	if rows[0]["name"] != "АБИКЛАВ" {
		t.Fatalf("INN Cyrillic: got %q want АБИКЛАВ", rows[0]["name"])
	}
}

func TestDrugsSearch_INNMatchesLatin(t *testing.T) {
	dir := t.TempDir()
	makeFixtureDrugDB(t, dir, "ua", [][4]string{
		{"IBUPROFEN-200", "ibuprofen", "Ibuprofen 200mg", "NSAIDs"},
		{"PARACETAMOL", "paracetamol", "Paracetamol 500mg", "Analgesics"},
	})
	reg := database.NewDrugRegistry(dir)
	srv := newDrugRouter(t, reg)

	// Search by INN using uppercase (should still match lowercase INN).
	res, err := http.Get(srv.URL + "/api/drugs/search?q=IBUP")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close() //nolint:errcheck // best-effort cleanup in tests
	rows := decodeSearch(t, res.Body)
	if len(rows) != 1 || rows[0]["name"] != "IBUPROFEN-200" {
		t.Fatalf("INN Latin: got %v want IBUPROFEN-200", rows)
	}
}

func TestDrugsSearch_NameRanksAboveINN(t *testing.T) {
	dir := t.TempDir()
	// IBUPROFEN-100 matches by name prefix; ZZ-DRUG matches only by INN.
	// Name-prefix match must rank first even though both have the same INN.
	makeFixtureDrugDB(t, dir, "ua", [][4]string{
		{"ZZ-DRUG", "ibuprofen", "Ibuprofen", "NSAIDs"},
		{"IBUPROFEN-100", "ibuprofen", "Ibuprofen", "NSAIDs"},
	})
	reg := database.NewDrugRegistry(dir)
	srv := newDrugRouter(t, reg)

	res, err := http.Get(srv.URL + "/api/drugs/search?q=ibuprofen")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer res.Body.Close() //nolint:errcheck // best-effort cleanup in tests
	rows := decodeSearch(t, res.Body)
	if len(rows) != 2 {
		t.Fatalf("rows: got %d want 2", len(rows))
	}
	if rows[0]["name"] != "IBUPROFEN-100" {
		t.Fatalf("ranking: name prefix must beat INN match, got first=%q", rows[0]["name"])
	}
}
