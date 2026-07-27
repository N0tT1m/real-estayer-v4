package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serveWithHeaders(t *testing.T, isProd bool) *httptest.ResponseRecorder {
	t.Helper()
	h := SecurityHeaders(isProd)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	return rr
}

func TestSecurityHeadersAlwaysSet(t *testing.T) {
	rr := serveWithHeaders(t, false)

	want := map[string]string{
		"X-Content-Type-Options":     "nosniff",
		"X-Frame-Options":            "DENY",
		"Referrer-Policy":            "strict-origin-when-cross-origin",
		"Cross-Origin-Opener-Policy": "same-origin",
	}
	for header, value := range want {
		if got := rr.Header().Get(header); got != value {
			t.Errorf("%s = %q, want %q", header, got, value)
		}
	}
	if rr.Header().Get("Content-Security-Policy") == "" {
		t.Error("CSP must be present")
	}
}

// HSTS over plain HTTP is ignored by browsers, and sending it from a LAN
// deployment that later gains a hostname is how you pin yourself into an
// outage. It is withheld until the deployment claims production.
func TestHSTSOnlyInProduction(t *testing.T) {
	if got := serveWithHeaders(t, false).Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("development should not send HSTS, got %q", got)
	}
	if got := serveWithHeaders(t, true).Header().Get("Strict-Transport-Security"); got == "" {
		t.Error("production should send HSTS")
	}
}

// The CSP is weakened by 'unsafe-inline'/'unsafe-eval' out of necessity, so
// the directives that still carry weight are worth pinning explicitly.
func TestCSPKeepsTheDirectivesThatStillBite(t *testing.T) {
	csp := serveWithHeaders(t, true).Header().Get("Content-Security-Policy")

	for _, directive := range []string{
		"frame-ancestors 'none'",
		"object-src 'none'",
		"base-uri 'self'",
		"form-action 'self'",
		"default-src 'self'",
	} {
		if !strings.Contains(csp, directive) {
			t.Errorf("CSP missing %q:\n%s", directive, csp)
		}
	}
}

// The front-end loads Tailwind, Alpine, and MapLibre from CDNs. A CSP that
// omits any of them takes the site down, so pin them.
func TestCSPAllowsTheCDNsTheAppActuallyUses(t *testing.T) {
	csp := serveWithHeaders(t, true).Header().Get("Content-Security-Policy")

	for _, origin := range []string{
		"https://cdn.tailwindcss.com",
		"https://cdn.jsdelivr.net",
		"https://unpkg.com",
		"https://fonts.googleapis.com",
		"https://fonts.gstatic.com",
	} {
		if !strings.Contains(csp, origin) {
			t.Errorf("CSP would block %s:\n%s", origin, csp)
		}
	}
}

// Headers have to be written before the handler commits a status, or they
// never reach the wire.
func TestSecurityHeadersSurviveAnEarlyWrite(t *testing.T) {
	h := SecurityHeaders(false)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("nope"))
	}))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/missing", nil))

	if rr.Code != http.StatusNotFound {
		t.Fatalf("status = %d", rr.Code)
	}
	if rr.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("headers must be set even on an error response")
	}
}
