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

func TestRecover_RepanicsErrAbortHandler(t *testing.T) {
	chain := middleware.Recover(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	}))

	defer func() {
		if p := recover(); p != http.ErrAbortHandler { //nolint:errorlint // identity comparison, as net/http does
			t.Errorf("recovered %v, want http.ErrAbortHandler", p)
		}
	}()
	chain.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/test", http.NoBody))
}

func TestRecover_AbortsWhenResponseAlreadyStarted(t *testing.T) {
	chain := middleware.Recover(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":[`))
		panic("boom")
	}))

	w := httptest.NewRecorder()
	defer func() {
		if p := recover(); p != http.ErrAbortHandler { //nolint:errorlint // identity comparison, as net/http does
			t.Errorf("recovered %v, want http.ErrAbortHandler", p)
		}
		if body := w.Body.String(); body != `{"data":[` {
			t.Errorf("body = %q: a 500 envelope was appended to a started response", body)
		}
	}()
	chain.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/test", http.NoBody))
}
