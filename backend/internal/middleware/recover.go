package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/vinegod/medvault/backend/internal/api"
	"github.com/vinegod/medvault/backend/internal/models"
)

// Recover catches any panic from a downstream handler, logs it with the
// stack trace and request id, and writes a 500 envelope so the client
// always gets a structured response instead of a dropped connection.
//
// Must be installed BEFORE every other middleware so it also covers
// panics in those (with the documented exception that a panic in
// RequestID itself will produce a log line without a req_id).
//
// http.ErrAbortHandler is re-panicked untouched: it is net/http's sentinel
// for "abort this response silently". If the handler already started the
// response, a 500 can no longer be sent, so the connection is aborted
// instead: a truncated body must not look like a complete success.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w}
		defer func() {
			if p := recover(); p != nil {
				if p == http.ErrAbortHandler { //nolint:errorlint // sentinel is compared by identity, as net/http does
					panic(p)
				}
				// RequestID sets the header before any downstream code runs;
				// reading it here is more reliable than context because Recover
				// holds the original r whose context predates RequestID's injection.
				reqID := w.Header().Get("X-Request-Id")
				slog.Error("panic",
					"err", p,
					"stack", string(debug.Stack()),
					"req_id", reqID,
					"method", r.Method,
					"path", r.URL.Path,
				)
				if rec.status != 0 {
					panic(http.ErrAbortHandler)
				}
				api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
			}
		}()
		next.ServeHTTP(rec, r)
	})
}
