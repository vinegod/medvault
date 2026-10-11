package assistant_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/vinegod/medvault/backend/internal/assistant"
	"github.com/vinegod/medvault/backend/internal/assistant/assistanttest"
	"github.com/vinegod/medvault/backend/internal/models"
	"github.com/vinegod/medvault/backend/internal/testutil"
)

const secret = "test-ai-secret"

func newAssistant(t *testing.T, db *sql.DB, def *assistant.Config) *assistant.Assistant {
	t.Helper()
	if def.Provider == "" {
		def.Provider = models.AIProviderOpenAI
	}
	a, err := assistant.New(db, nil, &assistant.Options{
		Default: *def,
		Secret:  []byte(secret),
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return a
}

type medicine struct {
	name, ingredient, barcode, expiration string
}

func addMedicine(t *testing.T, db *sql.DB, u testutil.User, m medicine) string {
	t.Helper()
	id := uuid.NewString()
	now := time.Now().UTC()
	var expiration any
	if m.expiration != "" {
		expiration = m.expiration
	}
	if _, err := db.Exec(`
		INSERT INTO medicines (id, household_id, created_by, name, active_ingredient, barcode,
			expiration_date, quantity, unit, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, 10, 'tablets', ?, ?)`,
		id, u.HouseholdID, u.ID, m.name, m.ingredient, m.barcode, expiration, now, now,
	); err != nil {
		t.Fatalf("insert medicine: %v", err)
	}
	return id
}

func userMessage(text string) []models.ChatMessage {
	return []models.ChatMessage{{Role: models.ChatRoleUser, Content: text}}
}

// toolOutput returns the content of the last tool message in a request.
func toolOutput(t *testing.T, req map[string]any) string {
	t.Helper()
	msgs, _ := req["messages"].([]any)
	for i := len(msgs) - 1; i >= 0; i-- {
		m, _ := msgs[i].(map[string]any)
		if m["role"] == "tool" {
			content, _ := json.Marshal(m["content"])
			return string(content)
		}
	}
	t.Fatalf("no tool message in request")
	return ""
}

func TestChat_ToolsStayInHousehold(t *testing.T) {
	db := testutil.NewDB(t)
	alice := testutil.MakeUser(t, db, "alice")
	bob := testutil.MakeUser(t, db, "bob")
	paracetamolID := addMedicine(t, db, alice, medicine{name: "Paracetamol", ingredient: "paracetamol"})
	addMedicine(t, db, bob, medicine{name: "Secretol"})

	srv := assistanttest.NewServer(t,
		assistanttest.Reply{ToolName: "search_inventory", ToolArgs: `{}`},
		assistanttest.Reply{Text: "You can take Paracetamol."},
	)
	a := newAssistant(t, db, &assistant.Config{BaseURL: srv.URL, Model: "test-model"})

	reply, err := a.Chat(context.Background(), alice.HouseholdID, userMessage("headache"))
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}

	reqs := srv.Requests()
	if len(reqs) != 2 {
		t.Fatalf("model requests = %d, want 2", len(reqs))
	}
	out := toolOutput(t, reqs[1])
	if !strings.Contains(out, "Paracetamol") || strings.Contains(out, "Secretol") {
		t.Errorf("tool output = %s, want only alice's medicines", out)
	}
	if reply.Text != "You can take Paracetamol." {
		t.Errorf("reply text = %q", reply.Text)
	}
	if len(reply.MedicineIDs) != 1 || reply.MedicineIDs[0] != paracetamolID {
		t.Errorf("medicine ids = %v, want [%s]", reply.MedicineIDs, paracetamolID)
	}
}

func TestChat_WarningDoesNotReplaceAnswer(t *testing.T) {
	db := testutil.NewDB(t)
	alice := testutil.MakeUser(t, db, "alice")
	srv := assistanttest.NewServer(t,
		assistanttest.Reply{Text: "WARNING: chest pain needs a doctor\nYou have Aspirin in stock."},
	)
	a := newAssistant(t, db, &assistant.Config{BaseURL: srv.URL, Model: "test-model"})

	reply, err := a.Chat(context.Background(), alice.HouseholdID, userMessage("chest pain"))
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if reply.Warning != "chest pain needs a doctor" || reply.Text != "You have Aspirin in stock." {
		t.Errorf("reply = %+v", reply)
	}
}

func TestChat_StopsAtTurnLimit(t *testing.T) {
	db := testutil.NewDB(t)
	alice := testutil.MakeUser(t, db, "alice")
	replies := make([]assistanttest.Reply, 0, 8)
	for range 8 {
		replies = append(replies, assistanttest.Reply{ToolName: "search_inventory", ToolArgs: `{}`})
	}
	srv := assistanttest.NewServer(t, replies...)
	a := newAssistant(t, db, &assistant.Config{BaseURL: srv.URL, Model: "test-model"})

	_, err := a.Chat(context.Background(), alice.HouseholdID, userMessage("loop"))
	if !errors.Is(err, assistant.ErrUpstream) {
		t.Fatalf("Chat err = %v, want ErrUpstream", err)
	}
	if n := len(srv.Requests()); n > 6 {
		t.Errorf("model requests = %d, want at most 6", n)
	}
}

func TestScan_DraftAndMatches(t *testing.T) {
	db := testutil.NewDB(t)
	alice := testutil.MakeUser(t, db, "alice")
	existingID := addMedicine(t, db, alice, medicine{name: "Nurofen", barcode: "4820001"})

	srv := assistanttest.NewServer(
		t,
		assistanttest.Reply{Text: `{"name":"Nurofen Forte","active_ingredient":"ibuprofen",
		"quantity":-5,"unit":"Boxes","expiration_date":"05/2027","barcode":"4820001"}`},
	)
	a := newAssistant(t, db, &assistant.Config{BaseURL: srv.URL, Model: "test-model"})

	res, err := a.Scan(context.Background(), alice.HouseholdID,
		assistant.Image{MIMEType: "image/png", Data: []byte("png")})
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}

	d := res.Draft
	if d.Name != "Nurofen Forte" || d.Quantity != 0 || d.Unit != "tablets" {
		t.Errorf("draft = %+v, want name kept, invalid quantity and unit reset", d)
	}
	if d.ExpirationDate == nil || *d.ExpirationDate != "2027-05-31" {
		t.Errorf("expiration = %v, want 2027-05-31", d.ExpirationDate)
	}
	if len(res.Matches) != 1 || res.Matches[0].ID != existingID || res.Matches[0].Reason != "barcode" {
		t.Errorf("matches = %+v, want barcode match on %s", res.Matches, existingID)
	}

	body, err := json.Marshal(srv.Requests()[0])
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(body, []byte("data:image/png;base64,")) {
		t.Errorf("request has no image data URI: %s", body)
	}
}

func TestSettings_SavedConfigNeverUsesDefaultKey(t *testing.T) {
	db := testutil.NewDB(t)
	alice := testutil.MakeUser(t, db, "alice")
	envSrv := assistanttest.NewServer(t)
	webSrv := assistanttest.NewServer(t, assistanttest.Reply{Text: "ok"})
	a := newAssistant(t, db, &assistant.Config{BaseURL: envSrv.URL, APIKey: "env-key", Model: "env-model"})

	if err := a.SaveSettings(context.Background(), alice.ID, &models.AISettingsRequest{
		Provider: models.AIProviderOpenAI, BaseURL: webSrv.URL, Model: "web-model",
	}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if _, err := a.Chat(context.Background(), alice.HouseholdID, userMessage("hi")); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if n := len(envSrv.Requests()); n != 0 {
		t.Errorf("env endpoint got %d requests, want 0", n)
	}
	for _, h := range webSrv.AuthHeaders() {
		if strings.Contains(h, "env-key") {
			t.Errorf("saved endpoint received the env API key")
		}
	}
	if model := webSrv.Requests()[0]["model"]; model != "web-model" {
		t.Errorf("model = %v, want web-model", model)
	}

	if err := a.ResetSettings(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s, _ := a.Settings(context.Background()); s.Source != "env" || s.Model != "env-model" {
		t.Errorf("settings after reset = %+v, want env default", s)
	}
}

func TestSettings_APIKeyEncryptedAndWriteOnly(t *testing.T) {
	db := testutil.NewDB(t)
	alice := testutil.MakeUser(t, db, "alice")
	srv := assistanttest.NewServer(t, assistanttest.Reply{Text: "ok"})
	a := newAssistant(t, db, &assistant.Config{})
	key := "sk-saved-secret"

	if err := a.SaveSettings(context.Background(), alice.ID, &models.AISettingsRequest{
		Provider: models.AIProviderOpenAI, BaseURL: srv.URL, Model: "m", APIKey: &key,
	}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	var stored []byte
	if err := db.QueryRow(`SELECT api_key_enc FROM ai_settings`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(stored, []byte(key)) {
		t.Errorf("api key stored in plaintext")
	}

	s, err := a.Settings(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !s.APIKeySet || s.Source != "web" {
		t.Errorf("settings = %+v, want api_key_set and web source", s)
	}

	if _, err := a.Chat(context.Background(), alice.HouseholdID, userMessage("hi")); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := srv.AuthHeaders()[0]; got != "Bearer "+key {
		t.Errorf("Authorization = %q, want the decrypted key", got)
	}

	// Saving without api_key keeps the stored key.
	if err := a.SaveSettings(context.Background(), alice.ID, &models.AISettingsRequest{
		Provider: models.AIProviderOpenAI, BaseURL: srv.URL, Model: "m2",
	}); err != nil {
		t.Fatal(err)
	}
	if s, _ := a.Settings(context.Background()); !s.APIKeySet {
		t.Errorf("api key dropped by an update without api_key")
	}
}

func TestSettings_WrongSecretReportsUnreadableKey(t *testing.T) {
	db := testutil.NewDB(t)
	alice := testutil.MakeUser(t, db, "alice")
	key := "sk-x"
	a := newAssistant(t, db, &assistant.Config{})
	if err := a.SaveSettings(context.Background(), alice.ID, &models.AISettingsRequest{
		Provider: models.AIProviderOpenAI, Model: "m", APIKey: &key,
	}); err != nil {
		t.Fatal(err)
	}

	other, err := assistant.New(db, nil, &assistant.Options{Secret: []byte("rotated")})
	if err != nil {
		t.Fatal(err)
	}
	st, err := other.Status(context.Background())
	if err != nil || st.Enabled {
		t.Errorf("Status = %+v, %v; want disabled without error", st, err)
	}
	_, err = other.Chat(context.Background(), alice.HouseholdID, userMessage("hi"))
	if !errors.Is(err, assistant.ErrKeyUnreadable) {
		t.Errorf("Chat err = %v, want ErrKeyUnreadable", err)
	}
}

func TestStatus_DisabledWithoutModel(t *testing.T) {
	db := testutil.NewDB(t)
	alice := testutil.MakeUser(t, db, "alice")
	a := newAssistant(t, db, &assistant.Config{})

	st, err := a.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.Enabled || st.Source != "none" {
		t.Errorf("Status = %+v, want disabled with source none", st)
	}
	_, err = a.Chat(context.Background(), alice.HouseholdID, userMessage("hi"))
	if !errors.Is(err, assistant.ErrDisabled) {
		t.Errorf("Chat err = %v, want ErrDisabled", err)
	}
}

func TestChat_InventoryIncludesExpiredAndFallsBack(t *testing.T) {
	db := testutil.NewDB(t)
	alice := testutil.MakeUser(t, db, "alice")
	addMedicine(t, db, alice, medicine{name: "Loratadine", ingredient: "loratadine", expiration: "2020-01-01"})

	srv := assistanttest.NewServer(t,
		assistanttest.Reply{ToolName: "search_inventory", ToolArgs: `{"text":"алергія"}`},
		assistanttest.Reply{Text: "Loratadine is expired."},
	)
	a := newAssistant(t, db, &assistant.Config{BaseURL: srv.URL, Model: "test-model"})

	if _, err := a.Chat(context.Background(), alice.HouseholdID, userMessage("allergy")); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	out := toolOutput(t, srv.Requests()[1])
	if !strings.Contains(out, "Loratadine") || !strings.Contains(out, `\"expired\":true`) {
		t.Errorf("tool output = %s, want the expired item flagged", out)
	}
}
