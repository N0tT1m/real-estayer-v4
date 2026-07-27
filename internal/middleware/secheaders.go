package middleware

import (
	"net/http"
	"strings"
)

// contentSecurityPolicy is assembled once at package init rather than per
// request — it never varies, and string-joining it on every response is pure
// waste on a server-rendered app.
//
// Honest ceiling: this policy contains 'unsafe-inline' and 'unsafe-eval', so
// it does NOT stop injected script from executing. It cannot, given the
// current front-end: every page template carries inline <script> blocks,
// Alpine evaluates its x- attributes through new Function(), and the Tailwind
// CDN compiles at runtime the same way. What it does buy is real but narrower
// — script can only be *loaded* from origins we name, exfiltration is bounded
// by connect-src, and framing, <base> hijacking, and plugin embedding are shut
// off outright. Tightening to nonces means threading a per-request nonce
// through all 29 templates and dropping the Tailwind CDN for a built stylesheet;
// 'unsafe-eval' would still have to stay for Alpine.
var contentSecurityPolicy = strings.Join([]string{
	"default-src 'self'",
	// cdn.tailwindcss.com compiles styles at runtime; jsdelivr serves Alpine;
	// unpkg serves MapLibre.
	"script-src 'self' 'unsafe-inline' 'unsafe-eval' https://cdn.tailwindcss.com https://cdn.jsdelivr.net https://unpkg.com",
	"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com https://unpkg.com https://cdn.jsdelivr.net",
	"font-src 'self' data: https://fonts.gstatic.com",
	// Listing and destination photos come from whatever CDN the upstream
	// happens to use (Airbnb's muscache, Unsplash, Wikimedia), so this cannot
	// be an allowlist without breaking images the scraper legitimately found.
	"img-src 'self' data: blob: https:",
	// Map tiles and geocoding are fetched straight from the browser against
	// CARTO / MapTiler, which likewise varies by configuration.
	"connect-src 'self' https:",
	"object-src 'none'",
	"base-uri 'self'",
	"form-action 'self'",
	"frame-ancestors 'none'",
}, "; ")

// SecurityHeaders sets the response headers the app was missing entirely.
//
// isProduction gates only Strict-Transport-Security. HSTS is ignored by
// browsers when served over plain HTTP, but sending it from a LAN deployment
// that later gets a hostname is how you pin yourself into an outage, so it is
// withheld until the deployment claims to be production.
func SecurityHeaders(isProduction bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()

			// Set before next.ServeHTTP: once a handler writes its status the
			// header map is already on the wire and further edits are lost.
			h.Set("X-Content-Type-Options", "nosniff")
			h.Set("X-Frame-Options", "DENY")
			h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
			h.Set("Cross-Origin-Opener-Policy", "same-origin")
			// The around-me page asks for coordinates; nothing wants the
			// camera, microphone, or payment APIs.
			h.Set("Permissions-Policy", "geolocation=(self), camera=(), microphone=(), payment=()")
			h.Set("Content-Security-Policy", contentSecurityPolicy)

			if isProduction {
				h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
			}

			next.ServeHTTP(w, r)
		})
	}
}
