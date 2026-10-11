package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/vinegod/medvault/backend/internal/assistant"
	"github.com/vinegod/medvault/backend/internal/database"
	"github.com/vinegod/medvault/backend/internal/testutil"
)

// A user whose last household is gone must still be able to redeem an
// invite: Join is wired outside RequireHousehold.
func TestRouter_JoinWithoutAnyHousehold(t *testing.T) {
	db := testutil.NewDB(t)
	cfg := config{
		jwtSecret:  testutil.JWTSecret,
		accessTTL:  time.Hour,
		refreshTTL: time.Hour,
		staticDir:  t.TempDir(),
		photoDir:   t.TempDir(),
		ai:         assistant.Options{Secret: []byte(testutil.JWTSecret)},
	}
	srv := httptest.NewServer(buildRouter(db, database.NewDrugRegistry(""), &cfg))
	t.Cleanup(srv.Close)

	owner := testutil.MakeUser(t, db, "owner")
	joiner := testutil.MakeUser(t, db, "joiner")
	if _, err := db.Exec(`DELETE FROM households WHERE created_by = ?`, joiner.ID); err != nil {
		t.Fatalf("delete joiner household: %v", err)
	}

	var invite struct {
		Data struct {
			Code string `json:"code"`
		} `json:"data"`
	}
	resp := post(t, srv.URL+"/api/households/"+owner.HouseholdID+"/invite", testutil.AccessToken(t, owner), nil)
	if err := json.NewDecoder(resp.Body).Decode(&invite); err != nil {
		t.Fatalf("decode invite: %v", err)
	}

	resp = post(t, srv.URL+"/api/households/join", testutil.AccessToken(t, joiner),
		map[string]string{"code": invite.Data.Code})
	if resp.StatusCode != http.StatusOK {
		t.Errorf("join status=%d, want 200", resp.StatusCode)
	}
}

func post(t *testing.T, url, token string, body any) *http.Response {
	t.Helper()
	buf, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}
