// Package crypto provides the field-level encryption helpers used for
// sensitive user data (passport numbers, KTN, loyalty numbers).
//
// Stored form is "v1:<base64(nonce||ciphertext)>", which lets us rotate
// algorithms later by bumping the version tag. Plain-text values are
// passed through untouched when no key is configured, so local development
// isn't gated on key management.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// FieldCipher wraps an AES-GCM instance. Zero-value means "no encryption";
// callers should go through NewFieldCipher so key validation happens once.
type FieldCipher struct {
	aead cipher.AEAD
}

// NewFieldCipher builds a cipher from a 32-byte key. Accepts either 32
// raw bytes or the hex encoding of same (handy for env vars). Returns a
// zero-value cipher (with no error) when the key is empty — callers should
// check Enabled() before assuming anything is encrypted.
func NewFieldCipher(hexOrRawKey string) (*FieldCipher, error) {
	if hexOrRawKey == "" {
		return &FieldCipher{}, nil
	}
	key, err := decodeKey(hexOrRawKey)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &FieldCipher{aead: aead}, nil
}

// Enabled reports whether encryption is actually configured.
func (c *FieldCipher) Enabled() bool { return c != nil && c.aead != nil }

// Encrypt returns the v1-tagged ciphertext for plaintext. When the cipher
// isn't configured the plaintext is returned verbatim.
func (c *FieldCipher) Encrypt(plain string) (string, error) {
	if !c.Enabled() || plain == "" || strings.HasPrefix(plain, "v1:") {
		return plain, nil
	}
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := c.aead.Seal(nil, nonce, []byte(plain), nil)
	combined := append(nonce, ct...)
	return "v1:" + base64.RawStdEncoding.EncodeToString(combined), nil
}

// Decrypt reverses Encrypt. Anything that's not a tagged ciphertext comes
// back as-is so upgrades from plain-text are invisible.
func (c *FieldCipher) Decrypt(stored string) (string, error) {
	if stored == "" || !strings.HasPrefix(stored, "v1:") {
		return stored, nil
	}
	if !c.Enabled() {
		return "", errors.New("fieldcipher: encountered ciphertext but cipher is disabled")
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(stored, "v1:"))
	if err != nil {
		return "", fmt.Errorf("fieldcipher: decode: %w", err)
	}
	ns := c.aead.NonceSize()
	if len(raw) < ns+c.aead.Overhead() {
		return "", errors.New("fieldcipher: ciphertext too short")
	}
	nonce, ct := raw[:ns], raw[ns:]
	plain, err := c.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", fmt.Errorf("fieldcipher: open: %w", err)
	}
	return string(plain), nil
}

// MustEncrypt panics on error. Convenient in service code that treats
// encryption as infallible (random-source failures are catastrophic).
func (c *FieldCipher) MustEncrypt(plain string) string {
	out, err := c.Encrypt(plain)
	if err != nil {
		panic(err)
	}
	return out
}

func decodeKey(s string) ([]byte, error) {
	if len(s) == 32 {
		return []byte(s), nil
	}
	// Hex: 64 chars → 32 bytes.
	if len(s) == 64 {
		b, err := hexDecode(s)
		if err == nil {
			return b, nil
		}
	}
	return nil, errors.New("fieldcipher: key must be 32 raw bytes or 64 hex chars")
}

// hexDecode is a minimal hex decoder that doesn't pull encoding/hex's extra
// alloc behaviour; returns an error if any char is non-hex.
func hexDecode(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, errors.New("odd length hex")
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(out); i++ {
		hi, err1 := hexNibble(s[2*i])
		lo, err2 := hexNibble(s[2*i+1])
		if err1 != nil || err2 != nil {
			return nil, errors.New("invalid hex")
		}
		out[i] = hi<<4 | lo
	}
	return out, nil
}

func hexNibble(c byte) (byte, error) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', nil
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, nil
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, nil
	}
	return 0, errors.New("not hex")
}
