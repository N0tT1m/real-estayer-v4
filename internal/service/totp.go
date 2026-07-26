// Package service — totp.go
//
// Dependency-free TOTP (RFC 6238 / HOTP RFC 4226) implementation. Keeps the
// app self-contained; no need for github.com/pquerna/otp or similar for
// the limited feature set we need: enrol → QR → verify.
package service

import (
	"crypto/hmac"
	"crypto/rand"
	// #nosec G505 -- RFC 6238 mandates HMAC-SHA1 for TOTP interoperability.
	"crypto/sha1"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTPSecret creates a new random 160-bit secret, encoded as base32 (no
// padding — Google Authenticator + 1Password accept this form).
func TOTPSecret() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(b), nil
}

// TOTPProvisioningURI builds the otpauth:// URL used by authenticator apps.
// The issuer shows as the app name in the UI; account is typically the user
// email. Both get URL-escaped.
func TOTPProvisioningURI(secret, issuer, account string) string {
	label := issuer + ":" + account
	v := url.Values{}
	v.Set("secret", secret)
	v.Set("issuer", issuer)
	v.Set("algorithm", "SHA1")
	v.Set("digits", "6")
	v.Set("period", "30")
	return "otpauth://totp/" + url.PathEscape(label) + "?" + v.Encode()
}

// TOTPVerify checks a user-entered 6-digit code. Accepts the current window
// plus the two neighbours so small clock drift doesn't lock users out.
func TOTPVerify(secret, code string) bool {
	code = strings.TrimSpace(code)
	if len(code) != 6 {
		return false
	}
	now := time.Now().Unix() / 30
	for _, delta := range []int64{-1, 0, 1} {
		// #nosec G115 -- now is Unix()/30; negative only for a pre-1970 clock.
		if hotp(secret, uint64(now+delta)) == code {
			return true
		}
	}
	return false
}

// hotp computes a 6-digit HOTP per RFC 4226 over a counter. Exposed as
// lowercase because only tests reach into it — it's not API surface.
func hotp(secret string, counter uint64) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.ToUpper(secret))
	if err != nil {
		return ""
	}
	buf := make([]byte, 8)
	binary.BigEndian.PutUint64(buf, counter)
	mac := hmac.New(sha1.New, key)
	mac.Write(buf)
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	truncated := binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", truncated%1_000_000)
}
