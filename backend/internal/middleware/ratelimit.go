package middleware

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/vinegod/medvault/backend/internal/api"
	"github.com/vinegod/medvault/backend/internal/models"
)

// bucket is one token-bucket entry in the limiter map.
//
// Token-bucket primer: tokens drip in at `rate` per second up to `burst`.
// Each request costs 1 token. If the bucket is empty, the request is
// rejected with 429. This smooths bursts but enforces a long-run average.
type bucket struct {
	tokens     float64
	lastRefill time.Time
}

// Limiter caps requests per key. A key is whatever ExtractKey returns —
// typically the client IP or the authenticated user id.
type Limiter struct {
	rate    float64 // tokens per second
	burst   float64 // max tokens the bucket can hold
	extract func(r *http.Request) string

	mu      sync.Mutex
	buckets map[string]*bucket
}

// NewLimiter builds a limiter where `perMinute` is the long-run cap and
// the bucket is sized to absorb bursts up to `perMinute` requests too.
// extract picks the rate-limit key from each request.
func NewLimiter(perMinute int, extract func(r *http.Request) string) *Limiter {
	l := &Limiter{
		rate:    float64(perMinute) / 60.0,
		burst:   float64(perMinute),
		extract: extract,
		buckets: make(map[string]*bucket),
	}
	go l.gc()
	return l
}

// allow refills the key's bucket and consumes one token.
// Returns false if the bucket is empty (request should be 429'd).
func (l *Limiter) allow(key string) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	b, ok := l.buckets[key]
	if !ok {
		// First request for this key — start with a full bucket so legit
		// users aren't penalised on their first call.
		l.buckets[key] = &bucket{tokens: l.burst - 1, lastRefill: now}
		return true
	}

	elapsed := now.Sub(b.lastRefill).Seconds()
	b.tokens += elapsed * l.rate
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.lastRefill = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// gc periodically removes idle buckets so the map doesn't grow forever.
// A bucket is "idle" if it's been full for >10 minutes.
func (l *Limiter) gc() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		cutoff := time.Now().Add(-10 * time.Minute)
		l.mu.Lock()
		for k, b := range l.buckets {
			if b.lastRefill.Before(cutoff) {
				delete(l.buckets, k)
			}
		}
		l.mu.Unlock()
	}
}

// Middleware wraps an http.Handler with this limiter. Rejected requests
// get 429 with a RATE_LIMITED error envelope.
func (l *Limiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := l.extract(r)
		if key == "" {
			// No key extracted — can't enforce a limit, let it through.
			next.ServeHTTP(w, r)
			return
		}
		if !l.allow(key) {
			api.WriteError(w, http.StatusTooManyRequests, models.CodeRateLimited, "rate limit exceeded")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ByIP keys on the client IP — use for unauthenticated endpoints.
// Strips the port so 1.2.3.4:55001 and 1.2.3.4:55002 share a bucket.
func ByIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ByUser keys on the authenticated user id — use for protected endpoints.
// Returns "" if no user is in context, which the limiter treats as "skip".
func ByUser(r *http.Request) string {
	return UserIDFrom(r.Context())
}
