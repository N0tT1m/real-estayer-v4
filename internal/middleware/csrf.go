package middleware

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type csrfKey struct{}

const (
	csrfCookieName = "csrf_token"
	csrfHeaderName = "X-CSRF-Token"
	csrfFormField  = "csrf_token"
)

// CSRF implements the double-submit cookie pattern. Each response sets a random
// token in a readable cookie; state-changing requests must echo it either in the
// X-CSRF-Token header or a csrf_token form field. The token is bound to a server
// secret via HMAC so attackers can't forge one by setting the cookie via a
// sub-domain.
func CSRF(secret string, secure bool) func(http.Handler) http.Handler {
	key := []byte(secret)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie(csrfCookieName)
			token := ""
			if err == nil {
				token = cookie.Value
			}
			if token == "" || !verifyCSRF(token, key) {
				token = issueCSRF(key)
				// #nosec G124 -- HttpOnly must be false here: the double-submit
				// pattern requires JS to read this cookie and echo it back in the
				// X-CSRF-Token header. Secure/SameSite are set below.
				http.SetCookie(w, &http.Cookie{
					Name:     csrfCookieName,
					Value:    token,
					Path:     "/",
					Expires:  time.Now().Add(12 * time.Hour),
					HttpOnly: false,
					Secure:   secure,
					SameSite: http.SameSiteLaxMode,
				})
			}

			ctx := context.WithValue(r.Context(), csrfKey{}, token)
			r = r.WithContext(ctx)

			if isSafeMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}

			submitted := r.Header.Get(csrfHeaderName)
			if submitted == "" {
				if ct := r.Header.Get("Content-Type"); strings.HasPrefix(ct, "application/x-www-form-urlencoded") || strings.HasPrefix(ct, "multipart/form-data") {
					_ = r.ParseForm()
					submitted = r.FormValue(csrfFormField)
				}
			}
			if submitted == "" || !hmac.Equal([]byte(submitted), []byte(token)) {
				if strings.HasPrefix(r.URL.Path, "/api/") {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusForbidden)
					_ = json.NewEncoder(w).Encode(map[string]string{"error": "invalid or missing CSRF token"})
				} else {
					http.Error(w, "Forbidden: invalid CSRF token", http.StatusForbidden)
				}
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// CSRFToken returns the current request's CSRF token, to be embedded in templates.
func CSRFToken(ctx context.Context) string {
	if v, ok := ctx.Value(csrfKey{}).(string); ok {
		return v
	}
	return ""
}

func issueCSRF(key []byte) string {
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	mac := hmac.New(sha256.New, key)
	mac.Write(nonce)
	sig := mac.Sum(nil)
	return hex.EncodeToString(nonce) + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func verifyCSRF(token string, key []byte) bool {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return false
	}
	nonce, err := hex.DecodeString(parts[0])
	if err != nil || len(nonce) != 16 {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, key)
	mac.Write(nonce)
	return hmac.Equal(sig, mac.Sum(nil))
}

func isSafeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return false
}
