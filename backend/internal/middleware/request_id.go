package middleware

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// RequestID assigns a UUID to every incoming request, stores it on the
// request context, and echoes it back as the X-Request-Id response header.
// Downstream handlers (and Logging) read it via RequestIDFrom so a single
// request can be traced through every log line it produces.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := uuid.NewString()
		w.Header().Set("X-Request-Id", id)
		ctx := context.WithValue(r.Context(), requestIDKey, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
