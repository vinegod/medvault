package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/vinegod/medvault/backend/internal/middleware"
)

func TestRealIP(t *testing.T) {
	trusted := []netip.Prefix{
		netip.MustParsePrefix("10.0.0.0/8"),
		netip.MustParsePrefix("::1/128"),
	}

	tests := []struct {
		name    string
		trusted []netip.Prefix
		remote  string
		xff     []string
		want    string
	}{
		{"no trusted proxies ignores header", nil, "10.0.0.1:5000", []string{"1.2.3.4"}, "10.0.0.1:5000"},
		{"untrusted peer ignores header", trusted, "8.8.8.8:5000", []string{"1.2.3.4"}, "8.8.8.8:5000"},
		{"trusted peer uses client", trusted, "10.0.0.1:5000", []string{"1.2.3.4"}, "1.2.3.4:5000"},
		{"spoofed left entry ignored", trusted, "10.0.0.1:5000", []string{"6.6.6.6, 1.2.3.4"}, "1.2.3.4:5000"},
		{"trusted hops skipped", trusted, "10.0.0.1:5000", []string{"1.2.3.4, 10.0.0.2"}, "1.2.3.4:5000"},
		{"multiple header lines", trusted, "10.0.0.1:5000", []string{"1.2.3.4", "10.0.0.2"}, "1.2.3.4:5000"},
		{"ipv6 proxy", trusted, "[::1]:5000", []string{"2001:db8::1"}, "[2001:db8::1]:5000"},
		{"garbage entry keeps peer", trusted, "10.0.0.1:5000", []string{"1.2.3.4, nope"}, "10.0.0.1:5000"},
		{"no header keeps peer", trusted, "10.0.0.1:5000", nil, "10.0.0.1:5000"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got string
			h := middleware.RealIP(tt.trusted)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				got = r.RemoteAddr
			}))
			req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
			req.RemoteAddr = tt.remote
			for _, v := range tt.xff {
				req.Header.Add("X-Forwarded-For", v)
			}
			h.ServeHTTP(httptest.NewRecorder(), req)
			if got != tt.want {
				t.Errorf("RemoteAddr = %q, want %q", got, tt.want)
			}
		})
	}
}
