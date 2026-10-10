package handlers_test

// Sync-time enrichment: when the server receives a brand-new medicine record
// with an empty `active_ingredient` or `notes`, it looks the trade name up in
// the configured drug DB and fills the missing fields. `category` is the
// user's own grouping and is never filled. These tests pin down:
//   - both fields fill on first insert when the lookup succeeds
//   - already-populated fields are never overwritten
//   - update of an existing id never re-fires enrichment
//   - records whose name has no exact drug match are left untouched
//   - the registry being unconfigured is a no-op (all fields stay empty)

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/vinegod/medvault/backend/internal/database"
	"github.com/vinegod/medvault/backend/internal/handlers"
	"github.com/vinegod/medvault/backend/internal/middleware"
	"github.com/vinegod/medvault/backend/internal/testutil"
)

// enrichHarness wires a SyncHandler with both the medicines DB and a fixture
// drug DB. drugCountry can be set to "" to simulate "registry knows about the
// country but the active country is unset" — sync still runs but enrichment is
// a no-op.
type enrichHarness struct {
	t      *testing.T
	server *httptest.Server
	db     *sql.DB
	user   testutil.User
	token  string
}

func newEnrichHarness(t *testing.T, drugCountry string, drugSeed [][4]string) *enrichHarness {
	t.Helper()
	db := testutil.NewDB(t)
	user := testutil.MakeUser(t, db, "alice")
	token := testutil.AccessToken(t, user)

	drugDir := t.TempDir()
	if drugSeed != nil {
		makeFixtureDrugDB(t, drugDir, "ua", drugSeed)
	}
	reg := database.NewDrugRegistry(drugDir)

	syncH := &handlers.SyncHandler{
		DB:           db,
		DrugRegistry: reg,
		DrugCountry:  drugCountry,
	}

	r := chi.NewRouter()
	r.Group(func(r chi.Router) {
		r.Use(middleware.RequireAuth(testutil.JWTSecret))
		r.Use(middleware.RequireHousehold(db))
		r.Post("/api/sync", syncH.Sync)
	})
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	return &enrichHarness{t: t, server: srv, db: db, user: user, token: token}
}

func (h *enrichHarness) postSync(changes []map[string]any) int {
	h.t.Helper()
	body, _ := json.Marshal(map[string]any{"last_sync": nil, "changes": changes})
	req, _ := http.NewRequest(http.MethodPost, h.server.URL+"/api/sync", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+h.token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatalf("post sync: %v", err)
	}
	defer resp.Body.Close() //nolint:errcheck // best-effort cleanup in tests
	return resp.StatusCode
}

// fetchMedicine returns (active_ingredient, notes) for id from the medicines
// table. A NULL ingredient reads as "".
func (h *enrichHarness) fetchMedicine(id string) (ingredient, notes string) {
	h.t.Helper()
	if err := h.db.QueryRow(
		`SELECT COALESCE(active_ingredient, ''), notes FROM medicines WHERE id = ?`, id,
	).Scan(&ingredient, &notes); err != nil {
		h.t.Fatalf("fetch %s: %v", id, err)
	}
	return ingredient, notes
}

// changeRecord builds a minimal valid sync change. Caller can tweak fields.
func changeRecord(id, name string) map[string]any {
	now := time.Now().UTC().Format(time.RFC3339)
	return map[string]any{
		"id":               id,
		"name":             name,
		"quantity":         5,
		"unit":             "tablets",
		"category":         "",
		"expiration_date":  nil,
		"storage_location": "",
		"barcode":          "",
		"notes":            "",
		"created_at":       now,
		"updated_at":       now,
		"deleted_at":       nil,
	}
}

const seedName = "ENRICHME"

var enrichSeed = [][4]string{
	{seedName, "amoxicillin", "Amoxicillin; Clavulanic acid", "Antibacterial agents"},
}

func TestSyncEnrich_FillsBothEmptyFields(t *testing.T) {
	h := newEnrichHarness(t, "ua", enrichSeed)
	id := uuid.NewString()

	if status := h.postSync([]map[string]any{changeRecord(id, seedName)}); status != http.StatusOK {
		t.Fatalf("sync status: got %d want 200", status)
	}

	ingredient, notes := h.fetchMedicine(id)
	if ingredient != "amoxicillin" {
		t.Errorf("active_ingredient: got %q want INN %q", ingredient, "amoxicillin")
	}
	if notes != "Antibacterial agents" {
		t.Errorf("notes: got %q want pharma group", notes)
	}
	var category string
	if err := h.db.QueryRow(`SELECT category FROM medicines WHERE id = ?`, id).Scan(&category); err != nil {
		t.Fatalf("fetch category: %v", err)
	}
	if category != "" {
		t.Errorf("category must not be filled, got %q", category)
	}
}

func TestSyncEnrich_PreservesNonEmptyFields(t *testing.T) {
	h := newEnrichHarness(t, "ua", enrichSeed)
	id := uuid.NewString()

	rec := changeRecord(id, seedName)
	rec["active_ingredient"] = "Already set"
	rec["notes"] = "use after meals"
	if status := h.postSync([]map[string]any{rec}); status != http.StatusOK {
		t.Fatalf("sync: got %d", status)
	}

	ingredient, notes := h.fetchMedicine(id)
	if ingredient != "Already set" || notes != "use after meals" {
		t.Errorf("user values overwritten: ingredient=%q notes=%q", ingredient, notes)
	}
}

func TestSyncEnrich_FillsOnlyEmptySide(t *testing.T) {
	h := newEnrichHarness(t, "ua", enrichSeed)
	id := uuid.NewString()

	rec := changeRecord(id, seedName)
	rec["notes"] = "user typed this"
	if status := h.postSync([]map[string]any{rec}); status != http.StatusOK {
		t.Fatalf("sync: got %d", status)
	}

	ingredient, notes := h.fetchMedicine(id)
	if ingredient != "amoxicillin" {
		t.Errorf("active_ingredient should fill: got %q", ingredient)
	}
	if notes != "user typed this" {
		t.Errorf("notes should be preserved: got %q", notes)
	}
}

func TestSyncEnrich_NotFiredOnUpdate(t *testing.T) {
	h := newEnrichHarness(t, "ua", enrichSeed)
	id := uuid.NewString()

	// First insert WITHOUT triggering enrichment by giving a non-matching name.
	first := changeRecord(id, "UNKNOWN-NAME")
	if status := h.postSync([]map[string]any{first}); status != http.StatusOK {
		t.Fatalf("first sync: got %d", status)
	}

	// Now simulate an update: same id, but the user renamed to a name that
	// WOULD match a drug. Since the id already exists, enrichment must NOT fire.
	updated := changeRecord(id, seedName)
	updated["updated_at"] = time.Now().UTC().Add(time.Minute).Format(time.RFC3339) // newer
	if status := h.postSync([]map[string]any{updated}); status != http.StatusOK {
		t.Fatalf("second sync: got %d", status)
	}

	ingredient, notes := h.fetchMedicine(id)
	if ingredient != "" || notes != "" {
		t.Errorf("update should not enrich: ingredient=%q notes=%q", ingredient, notes)
	}
}

func TestSyncEnrich_NoMatch_LeavesEmpty(t *testing.T) {
	h := newEnrichHarness(t, "ua", enrichSeed)
	id := uuid.NewString()

	if status := h.postSync([]map[string]any{changeRecord(id, "TOTALLY-UNKNOWN")}); status != http.StatusOK {
		t.Fatalf("sync: got %d", status)
	}

	ingredient, notes := h.fetchMedicine(id)
	if ingredient != "" || notes != "" {
		t.Errorf("unmatched name should not fill: ingredient=%q notes=%q", ingredient, notes)
	}
}

func TestSyncEnrich_RegistryEmpty_NoOp(t *testing.T) {
	h := newEnrichHarness(t, "ua", nil) // empty drug dir → no countries
	id := uuid.NewString()

	if status := h.postSync([]map[string]any{changeRecord(id, seedName)}); status != http.StatusOK {
		t.Fatalf("sync: got %d", status)
	}

	ingredient, notes := h.fetchMedicine(id)
	if ingredient != "" || notes != "" {
		t.Errorf("no DB → no fill, but got ingredient=%q notes=%q", ingredient, notes)
	}
}

func TestSyncEnrich_CountryUnset_NoOp(t *testing.T) {
	h := newEnrichHarness(t, "", enrichSeed) // registry has rows but no active country
	id := uuid.NewString()

	if status := h.postSync([]map[string]any{changeRecord(id, seedName)}); status != http.StatusOK {
		t.Fatalf("sync: got %d", status)
	}

	ingredient, notes := h.fetchMedicine(id)
	if ingredient != "" || notes != "" {
		t.Errorf("country unset → no fill, but got ingredient=%q notes=%q", ingredient, notes)
	}
}
