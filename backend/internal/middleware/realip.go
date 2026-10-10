package middleware

import (
	"net/http"
	"net/netip"
	"strings"
)

// RealIP rewrites r.RemoteAddr to the client address carried in
// X-Forwarded-For, but only when the direct peer is a trusted proxy.
// Without it, every request behind a reverse proxy shares the proxy's IP,
// so the per-IP rate limits apply to all users combined.
//
// The header is walked right to left, skipping trusted hops; the first
// untrusted address is the client. Entries left of it are client-supplied
// and never trusted. With no trusted proxies configured this is a no-op,
// so a directly-exposed server can't be fooled by a forged header.
func RealIP(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if len(trusted) == 0 {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			peer, err := netip.ParseAddrPort(r.RemoteAddr)
			if err == nil && isTrusted(peer.Addr(), trusted) {
				if client, ok := forwardedClient(r.Header.Values("X-Forwarded-For"), trusted); ok {
					r.RemoteAddr = netip.AddrPortFrom(client, peer.Port()).String()
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func forwardedClient(headers []string, trusted []netip.Prefix) (netip.Addr, bool) {
	hops := strings.Split(strings.Join(headers, ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		addr, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return netip.Addr{}, false
		}
		if !isTrusted(addr, trusted) {
			return addr.Unmap(), true
		}
	}
	return netip.Addr{}, false
}

func isTrusted(addr netip.Addr, trusted []netip.Prefix) bool {
	addr = addr.Unmap()
	for _, p := range trusted {
		if p.Contains(addr) {
			return true
		}
	}
	return false
}
