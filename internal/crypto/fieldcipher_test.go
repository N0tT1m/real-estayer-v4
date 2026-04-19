package crypto

import (
	"encoding/hex"
	"strings"
	"testing"
)

func testCipher(t *testing.T) *FieldCipher {
	t.Helper()
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	c, err := NewFieldCipher(hex.EncodeToString(key))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestFieldCipherRoundTrip(t *testing.T) {
	c := testCipher(t)
	for _, plain := range []string{"", "A1234567", "passport123!", strings.Repeat("x", 300)} {
		ct, err := c.Encrypt(plain)
		if err != nil {
			t.Fatalf("encrypt: %v", err)
		}
		if plain != "" && ct == plain {
			t.Fatalf("ciphertext equal to plaintext: %s", ct)
		}
		got, err := c.Decrypt(ct)
		if err != nil {
			t.Fatalf("decrypt: %v", err)
		}
		if got != plain {
			t.Errorf("round-trip mismatch: %q → %q → %q", plain, ct, got)
		}
	}
}

func TestFieldCipherEncryptIdempotent(t *testing.T) {
	c := testCipher(t)
	ct, err := c.Encrypt("hello")
	if err != nil {
		t.Fatal(err)
	}
	// Re-encrypting an already-tagged string should be a no-op.
	ct2, _ := c.Encrypt(ct)
	if ct != ct2 {
		t.Errorf("double-encrypt should be no-op: %s vs %s", ct, ct2)
	}
}

func TestFieldCipherDisabledPassthrough(t *testing.T) {
	c, _ := NewFieldCipher("")
	if c.Enabled() {
		t.Fatal("expected disabled")
	}
	enc, _ := c.Encrypt("hello")
	if enc != "hello" {
		t.Errorf("disabled cipher should pass plaintext: got %q", enc)
	}
	dec, _ := c.Decrypt("hello")
	if dec != "hello" {
		t.Errorf("plain inputs should pass through decrypt untouched")
	}
}

func TestFieldCipherDisabledRejectsCiphertext(t *testing.T) {
	enabled := testCipher(t)
	ct, _ := enabled.Encrypt("secret")
	disabled, _ := NewFieldCipher("")
	if _, err := disabled.Decrypt(ct); err == nil {
		t.Errorf("disabled cipher should refuse to decrypt real ciphertext")
	}
}

func TestFieldCipherRejectsShortKey(t *testing.T) {
	if _, err := NewFieldCipher("short"); err == nil {
		t.Errorf("short key should error")
	}
}

func TestFieldCipherAcceptsHexOrRaw(t *testing.T) {
	raw := strings.Repeat("k", 32)
	if _, err := NewFieldCipher(raw); err != nil {
		t.Errorf("32-byte raw key should work: %v", err)
	}
	hexKey := strings.Repeat("ab", 32) // 64 chars
	if _, err := NewFieldCipher(hexKey); err != nil {
		t.Errorf("64-char hex key should work: %v", err)
	}
}
