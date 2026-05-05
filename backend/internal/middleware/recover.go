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
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				// RequestID sets the header before any downstream code runs;
				// reading it here is more reliable than context because Recover
				// holds the original r whose context predates RequestID's injection.
				reqID := w.Header().Get("X-Request-Id")
				slog.Error("panic",
					"err", rec,
					"stack", string(debug.Stack()),
					"req_id", reqID,
					"method", r.Method,
					"path", r.URL.Path,
				)
				api.WriteError(w, http.StatusInternalServerError, models.CodeInternal, "server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
