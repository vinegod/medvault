package api

import (
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
func WriteJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(envelope{Data: data, Error: nil}); err != nil {
		slog.Error("encode response", "err", err)
	}
}

// WriteError writes a structured error envelope. The message is safe to show
// to clients; never put internal details (stack traces, SQL) in message.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(envelope{
		Data:  nil,
		Error: &apiError{Code: code, Message: message},
	}); err != nil {
		slog.Error("encode error response", "err", err)
	}
}
