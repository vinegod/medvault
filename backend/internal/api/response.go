package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
)

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type envelope struct {
	Data  any       `json:"data"`
	Error *apiError `json:"error"`
}

// WriteJSON encodes data in the standard response envelope and writes it to w.
//
// Encoding happens to a buffer before WriteHeader so an encode failure can
// degrade to a 500 instead of a half-written body under whatever status was
// supplied.
func WriteJSON(w http.ResponseWriter, status int, data any) {
	writeEnvelope(w, status, envelope{Data: data, Error: nil})
}

// WriteError writes a structured error envelope. The message is safe to show
// to clients; never put internal details (stack traces, SQL) in message.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	writeEnvelope(w, status, envelope{
		Data:  nil,
		Error: &apiError{Code: code, Message: message},
	})
}

// ServerError is the canonical "log + 500" pair used by every handler that
// hits an unexpected DB or encoding failure. The op string identifies the
// call site in logs ("Login: query user"); the err is logged but never
// returned to the client.
func ServerError(w http.ResponseWriter, op string, err error) {
	slog.Error(op, "err", err)
	WriteError(w, http.StatusInternalServerError, "INTERNAL", "server error")
}

func writeEnvelope(w http.ResponseWriter, status int, env envelope) {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(env); err != nil {
		// Encoding failed before any header was sent: the caller asked us
		// to serialise something that doesn't round-trip. Surface a 500
		// envelope (always serialisable) instead of leaking the raw error.
		slog.Error("encode response", "err", err)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"data":null,"error":{"code":"INTERNAL","message":"server error"}}` + "\n"))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(buf.Bytes()); err != nil {
		slog.Error("write response", "err", err)
	}
}
