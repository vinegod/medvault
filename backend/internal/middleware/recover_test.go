package middleware_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/vinegod/medvault/backend/internal/middleware"
)

func TestRecover_CatchesPanic(t *testing.T) {
	panicHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})

	chain := middleware.Recover(middleware.RequestID(panicHandler))

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	w := httptest.NewRecorder()
	chain.ServeHTTP(w, req)

	res := w.Result()
	if res.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d, want 500", res.StatusCode)
	}

	reqID := res.Header.Get("X-Request-Id")
	if reqID == "" {
		t.Error("X-Request-Id header missing")
	}

	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(res.Body).Decode(&envelope); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if envelope.Error.Code != "INTERNAL" {
		t.Errorf("error.code=%q, want INTERNAL", envelope.Error.Code)
	}
}

func TestRecover_PassthroughOnNoPanic(t *testing.T) {
	okHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	chain := middleware.Recover(middleware.RequestID(okHandler))

	req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
	w := httptest.NewRecorder()
	chain.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d, want 200", w.Code)
	}
}
