package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const testSecret = "test-secret-for-csrf-middleware-only"

func newProtectedMux(secret string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ok", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	return CSRF(secret, false)(mux)
}

func TestCSRFGETIssuesCookieAndPasses(t *testing.T) {
	h := newProtectedMux(testSecret)
	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET should pass; got %d", rr.Code)
	}
	setCookie := rr.Header().Get("Set-Cookie")
	if !strings.Contains(setCookie, csrfCookieName+"=") {
		t.Fatalf("expected csrf cookie in response, got %q", setCookie)
	}
}

func TestCSRFRejectsPOSTWithoutToken(t *testing.T) {
	h := newProtectedMux(testSecret)
	req := httptest.NewRequest(http.MethodPost, "/ok", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusForbidden {
		t.Fatalf("POST without token should be 403; got %d", rr.Code)
	}
}

func TestCSRFAcceptsPOSTWithValidHeader(t *testing.T) {
	h := newProtectedMux(testSecret)

	// Step 1: GET to obtain a token cookie.
	getReq := httptest.NewRequest(http.MethodGet, "/ok", nil)
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, getReq)
	cookie := extractCookie(t, getRR.Result().Cookies(), csrfCookieName)

	// Step 2: POST with that token in both cookie and header.
	postReq := httptest.NewRequest(http.MethodPost, "/ok", nil)
	postReq.AddCookie(cookie)
	postReq.Header.Set(csrfHeaderName, cookie.Value)
	postRR := httptest.NewRecorder()
	h.ServeHTTP(postRR, postReq)

	if postRR.Code != http.StatusOK {
		t.Fatalf("POST with matching token should succeed; got %d", postRR.Code)
	}
}

func TestCSRFRejectsMismatchedToken(t *testing.T) {
	h := newProtectedMux(testSecret)

	getReq := httptest.NewRequest(http.MethodGet, "/ok", nil)
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, getReq)
	cookie := extractCookie(t, getRR.Result().Cookies(), csrfCookieName)

	postReq := httptest.NewRequest(http.MethodPost, "/ok", nil)
	postReq.AddCookie(cookie)
	postReq.Header.Set(csrfHeaderName, "forged-token-value")
	postRR := httptest.NewRecorder()
	h.ServeHTTP(postRR, postReq)

	if postRR.Code != http.StatusForbidden {
		t.Fatalf("POST with mismatched token should be 403; got %d", postRR.Code)
	}
}

func TestCSRFRejectsTokenFromDifferentSecret(t *testing.T) {
	// A token signed under secret A must not validate under secret B.
	signerA := newProtectedMux("secret-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	verifierB := newProtectedMux("secret-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")

	getReq := httptest.NewRequest(http.MethodGet, "/ok", nil)
	getRR := httptest.NewRecorder()
	signerA.ServeHTTP(getRR, getReq)
	cookie := extractCookie(t, getRR.Result().Cookies(), csrfCookieName)

	postReq := httptest.NewRequest(http.MethodPost, "/ok", nil)
	postReq.AddCookie(cookie)
	postReq.Header.Set(csrfHeaderName, cookie.Value)
	postRR := httptest.NewRecorder()
	verifierB.ServeHTTP(postRR, postReq)

	// Under secret B the cookie fails HMAC verification, so the middleware
	// mints a fresh token — the submitted one no longer matches the reissued
	// cookie and the request is rejected.
	if postRR.Code != http.StatusForbidden {
		t.Fatalf("token from a different secret should be rejected; got %d", postRR.Code)
	}
}

func TestCSRFFormFieldAccepted(t *testing.T) {
	h := newProtectedMux(testSecret)

	getReq := httptest.NewRequest(http.MethodGet, "/ok", nil)
	getRR := httptest.NewRecorder()
	h.ServeHTTP(getRR, getReq)
	cookie := extractCookie(t, getRR.Result().Cookies(), csrfCookieName)

	body := strings.NewReader(csrfFormField + "=" + cookie.Value)
	postReq := httptest.NewRequest(http.MethodPost, "/ok", body)
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postReq.AddCookie(cookie)
	postRR := httptest.NewRecorder()
	h.ServeHTTP(postRR, postReq)

	if postRR.Code != http.StatusOK {
		t.Fatalf("POST with form-field token should succeed; got %d body=%q", postRR.Code, postRR.Body.String())
	}
}

func extractCookie(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	for _, c := range cookies {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("cookie %q not found", name)
	return nil
}
