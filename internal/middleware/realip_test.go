package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// seenIP runs a request through TrustedProxyIP and reports the client IP the
// rate limiters would end up keying on.
func seenIP(t *testing.T, trusted []string, remoteAddr string, headers map[string]string) string {
	t.Helper()

	var got string
	h := TrustedProxyIP(trusted)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = clientIP(r)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remoteAddr
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	h.ServeHTTP(httptest.NewRecorder(), req)
	return got
}

// The vulnerability chi's RealIP has: a direct client sets X-Forwarded-For and
// gets a fresh rate-limit bucket for every value it invents. With no trusted
// proxies configured the header must be ignored outright.
func TestForwardedHeadersIgnoredWithoutTrustedProxies(t *testing.T) {
	cases := []map[string]string{
		{"X-Forwarded-For": "1.2.3.4"},
		{"X-Real-IP": "1.2.3.4"},
		{"True-Client-IP": "1.2.3.4"},
	}
	for _, headers := range cases {
		if got := seenIP(t, nil, "203.0.113.9:5555", headers); got != "203.0.113.9" {
			t.Errorf("headers %v: client IP = %q, want the real peer 203.0.113.9", headers, got)
		}
	}
}

// Even with proxies configured, a peer outside those ranges is just a client
// and its headers carry no authority.
func TestForwardedHeadersIgnoredFromUntrustedPeer(t *testing.T) {
	got := seenIP(t, []string{"10.0.0.0/8"}, "203.0.113.9:5555",
		map[string]string{"X-Forwarded-For": "1.2.3.4"})
	if got != "203.0.113.9" {
		t.Errorf("client IP = %q, want the real peer — 203.0.113.9 is not a trusted proxy", got)
	}
}

func TestForwardedHeaderHonouredFromTrustedProxy(t *testing.T) {
	got := seenIP(t, []string{"10.0.0.0/8"}, "10.1.2.3:5555",
		map[string]string{"X-Forwarded-For": "198.51.100.7"})
	if got != "198.51.100.7" {
		t.Errorf("client IP = %q, want 198.51.100.7 from the trusted proxy", got)
	}
}

// The chain is walked right-to-left. Reading left-to-right is precisely what
// makes spoofing work, because the client controls the leftmost entry.
func TestXForwardedForWalksRightToLeft(t *testing.T) {
	got := seenIP(t, []string{"10.0.0.0/8"}, "10.1.2.3:5555",
		map[string]string{"X-Forwarded-For": "9.9.9.9, 198.51.100.7, 10.0.0.5"})
	if got != "198.51.100.7" {
		t.Errorf("client IP = %q, want 198.51.100.7 — 9.9.9.9 is client-supplied padding", got)
	}
}

func TestSingleValueHeadersAcceptedFromTrustedProxy(t *testing.T) {
	got := seenIP(t, []string{"10.0.0.0/8"}, "10.1.2.3:5555",
		map[string]string{"X-Real-IP": "198.51.100.7"})
	if got != "198.51.100.7" {
		t.Errorf("client IP = %q, want 198.51.100.7", got)
	}
}

// A bare IP should work as a trusted-proxy entry without demanding a suffix.
func TestBareIPAcceptedAsTrustedProxy(t *testing.T) {
	got := seenIP(t, []string{"10.1.2.3"}, "10.1.2.3:5555",
		map[string]string{"X-Forwarded-For": "198.51.100.7"})
	if got != "198.51.100.7" {
		t.Errorf("client IP = %q, want a bare IP to be treated as /32", got)
	}
}

// Garbage in the header must not blank out the key, or every malformed
// request would share one bucket.
func TestUnparseableForwardedValueFallsBackToPeer(t *testing.T) {
	got := seenIP(t, []string{"10.0.0.0/8"}, "10.1.2.3:5555",
		map[string]string{"X-Forwarded-For": "not-an-ip"})
	if got != "10.1.2.3" {
		t.Errorf("client IP = %q, want the peer when the header is unusable", got)
	}
}
