package middleware

import "net/http"

// SecurityHeaders adds defensive HTTP response headers on every response.
//
// What each one does and why we picked the value:
//
//   X-Content-Type-Options: nosniff
//     Stops browsers from second-guessing the Content-Type. Without it, an
//     HTML response served as text/plain could still be rendered as HTML.
//
//   X-Frame-Options: DENY
//     Legacy clickjacking guard. Modern browsers prefer the CSP
//     frame-ancestors directive (also set below), but DENY is one byte
//     cheaper to send and works on every browser.
//
//   Referrer-Policy: same-origin
//     Don't send the path/query of the current page to other origins. The
//     URLs in this app contain household and medicine ids; we don't want
//     them in referer headers to third parties.
//
//   Content-Security-Policy: see csp constant below.
//     The single most important header. Defines what the page is allowed
//     to load. With this in place, even if an attacker manages to inject
//     a string somewhere, they can't load external scripts, styles, or
//     fonts — and can't get the page framed.
//
// API-only responses get the same headers — they're cheap and never wrong.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("Content-Security-Policy", csp)
		next.ServeHTTP(w, r)
	})
}

// csp is the Content-Security-Policy applied to every response.
//
// Decisions:
//   - default-src 'self'                         everything defaults to same origin
//   - script-src  'self'                         no inline JS, no eval, no third-party scripts
//   - style-src   'self' 'unsafe-inline'         React style={{...}} props compile to inline
//                                                style attributes; without 'unsafe-inline'
//                                                the entire UI loses its styling
//   - img-src     'self' data:                   icons + any data: URLs (e.g. exported blobs)
//   - connect-src 'self'                         fetch only to our own backend
//   - worker-src  'self'                         service worker registers from /sw.js
//   - manifest-src 'self'                        PWA manifest
//   - font-src    'self'                         no Google Fonts etc.
//   - object-src  'none'                         no <object>/<embed>/<applet>
//   - base-uri    'self'                         can't change <base href>
//   - form-action 'self'                         forms only POST to us
//   - frame-ancestors 'none'                     this page can never be iframed
const csp = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data:; " +
	"connect-src 'self'; " +
	"worker-src 'self'; " +
	"manifest-src 'self'; " +
	"font-src 'self'; " +
	"object-src 'none'; " +
	"base-uri 'self'; " +
	"form-action 'self'; " +
	"frame-ancestors 'none'"
