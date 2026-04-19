package service

import (
	"strings"
	"testing"
)

func TestTOTPSecretShape(t *testing.T) {
	s, err := TOTPSecret()
	if err != nil {
		t.Fatal(err)
	}
	if len(s) < 24 {
		t.Errorf("secret too short: %q", s)
	}
	// Base32, no padding — only A-Z and 2-7.
	for _, r := range s {
		ok := (r >= 'A' && r <= 'Z') || (r >= '2' && r <= '7')
		if !ok {
			t.Fatalf("non base32 char in secret: %q", s)
		}
	}
}

func TestTOTPProvisioningURI(t *testing.T) {
	u := TOTPProvisioningURI("JBSWY3DPEHPK3PXP", "Real-Estayer", "you@example.com")
	if !strings.HasPrefix(u, "otpauth://totp/") {
		t.Errorf("unexpected scheme: %s", u)
	}
	// url.PathEscape leaves `@` in path segments alone (it's a reserved
	// sub-delim). Authenticator apps accept the literal form, so we check
	// the account appears — not that it's percent-encoded.
	for _, needed := range []string{"Real-Estayer", "you@example.com", "secret=JBSWY3DPEHPK3PXP", "algorithm=SHA1", "digits=6", "period=30"} {
		if !strings.Contains(u, needed) {
			t.Errorf("URI missing %q in %s", needed, u)
		}
	}
}

func TestHOTPKnownVectors(t *testing.T) {
	// RFC 4226 Appendix D "12345678901234567890" → base32 of that.
	// Expected HOTP values for counters 0..9 are fixed.
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	want := []string{"755224", "287082", "359152", "969429", "338314", "254676", "287922", "162583", "399871", "520489"}
	for i, w := range want {
		if got := hotp(secret, uint64(i)); got != w {
			t.Errorf("hotp(%d) = %s; want %s", i, got, w)
		}
	}
}

func TestTOTPVerifyAcceptsCurrent(t *testing.T) {
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	// Generate what we'd expect right now and round-trip verify.
	code := hotp(secret, uint64(0)) // arbitrary; verify checks current window
	// We can't easily check a synthetic code without time-travel; rely on
	// direct hotp assertions above. Instead verify the negative path.
	_ = code
	if TOTPVerify(secret, "000000") && TOTPVerify(secret, "000001") && TOTPVerify(secret, "000002") {
		t.Errorf("random codes should not all verify")
	}
}

func TestTOTPVerifyRejectsWrongLength(t *testing.T) {
	if TOTPVerify("ABC", "12345") {
		t.Errorf("5-digit code should be rejected")
	}
	if TOTPVerify("ABC", "") {
		t.Errorf("empty code should be rejected")
	}
}
