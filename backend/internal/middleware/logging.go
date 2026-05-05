package middleware

import (
	"log/slog"
	"net"
	"net/http"
	"time"
)

// statusRecorder wraps ResponseWriter so we can read the status code back
// out after the handler returns. http.ResponseWriter alone has no getter.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err //nolint:wrapcheck // transparent pass-through; wrapping would obscure the real net/http error
}

// Logging emits one structured log line per request: method, path, status,
// duration, remote IP. We do NOT log the request body or response body —
// SECURITY.md forbids logging tokens, passwords, or medicine data.
func Logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}

		next.ServeHTTP(rec, r)

		// Strip the ephemeral source port so 1.2.3.4:55001 and 1.2.3.4:55002
		// share the same log key — matches the grouping the rate limiter uses.
		remote := r.RemoteAddr
		if host, _, err := net.SplitHostPort(remote); err == nil {
			remote = host
		}

		slog.Info("request",
			"req_id", RequestIDFrom(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
			"remote", remote,
		)
	})
}
