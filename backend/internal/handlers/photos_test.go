package handlers_test

import (
	"bytes"
	"encoding/base64"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/vinegod/medvault/backend/internal/handlers"
	"github.com/vinegod/medvault/backend/internal/middleware"
	"github.com/vinegod/medvault/backend/internal/testutil"
)

// 1x1 transparent PNG.
const pngBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNkYPhfDwAChwGA60e6kgAAAABJRU5ErkJggg=="

func TestPhotos_ServedWithSniffedContentType(t *testing.T) {
	db := testutil.NewDB(t)
	user := testutil.MakeUser(t, db, "alice")
	token := testutil.AccessToken(t, user)

	photoH := &handlers.PhotoHandler{DB: db, PhotoDir: t.TempDir()}
	r := chi.NewRouter()
	r.Use(middleware.RequireAuth(testutil.JWTSecret), middleware.RequireHousehold(db))
	r.Get("/api/medicines/{id}/photo", photoH.Get)
	r.Post("/api/medicines/{id}/photo", photoH.Upload)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	id := uuid.NewString()
	now := time.Now().UTC()
	if _, err := db.Exec(`INSERT INTO medicines (id, household_id, created_by, name, created_at, updated_at)
		VALUES (?, ?, ?, 'Aspirin', ?, ?)`, id, user.HouseholdID, user.ID, now, now); err != nil {
		t.Fatalf("insert medicine: %v", err)
	}

	png, err := base64.StdEncoding.DecodeString(pngBase64)
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Disposition": {`form-data; name="photo"; filename="p.png"`},
		"Content-Type":        {"image/png"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(png); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	url := srv.URL + "/api/medicines/" + id + "/photo"
	req, _ := http.NewRequest(http.MethodPost, url, &body)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("upload status=%d", resp.StatusCode)
	}

	req, _ = http.NewRequest(http.MethodGet, url, http.NoBody)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	_ = resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q, want image/png", ct)
	}
}
