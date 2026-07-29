package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"testing"
	"time"
)

// The four-step key derivation is the most error-prone part of SigV4 and the
// easiest to get subtly wrong (order of region/service, the "AWS4" prefix, the
// literal "aws4_request" terminator). AWS publishes a worked example for it;
// this pins us to that published value.
//
// Source: AWS SigV4 documentation, "Deriving the signing key" worked example.
func TestSigV4SigningKeyMatchesAWSPublishedVector(t *testing.T) {
	const (
		secret    = "wJalrXUtnFEMI/K7MDENG+bPxRfiCYEXAMPLEKEY"
		dateStamp = "20120215"
		region    = "us-east-1"
		service   = "iam"
		want      = "f4780e2d9f65fa895f9c67b32ce1baf0b0d8a43505a000a1a9e090d414db404d"
	)

	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))

	if got := hex.EncodeToString(kSigning); got != want {
		t.Errorf("signing key = %s, want %s", got, want)
	}
}

// The canonical request is a byte-exact construction; a stray newline or an
// unsorted header silently produces a 403 that reads like a credentials
// problem. Build it independently here and compare.
func TestSigV4CanonicalRequestShape(t *testing.T) {
	body := []byte("hello")
	req, err := http.NewRequest(http.MethodPut, "https://bucket.example.com/users/abc/photo-1.jpg", strings.NewReader("hello"))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	at := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
	if err := signAWSv4At(req, body, "AKIDEXAMPLE", "secret", "us-east-1", "s3", at); err != nil {
		t.Fatalf("sign: %v", err)
	}

	bodyHash := sha256Hex(body)
	if got := req.Header.Get("x-amz-content-sha256"); got != bodyHash {
		t.Errorf("x-amz-content-sha256 = %s, want %s", got, bodyHash)
	}
	if got := req.Header.Get("x-amz-date"); got != "20240301T120000Z" {
		t.Errorf("x-amz-date = %s, want 20240301T120000Z", got)
	}

	canonical := strings.Join([]string{
		"PUT",
		"/users/abc/photo-1.jpg",
		"",
		"host:bucket.example.com",
		"x-amz-content-sha256:" + bodyHash,
		"x-amz-date:20240301T120000Z",
		"",
		"host;x-amz-content-sha256;x-amz-date",
		bodyHash,
	}, "\n")

	scope := "20240301/us-east-1/s3/aws4_request"
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		"20240301T120000Z",
		scope,
		sha256Hex([]byte(canonical)),
	}, "\n")

	kDate := hmacSHA256([]byte("AWS4secret"), []byte("20240301"))
	kRegion := hmacSHA256(kDate, []byte("us-east-1"))
	kService := hmacSHA256(kRegion, []byte("s3"))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	wantSig := hex.EncodeToString(hmacSHA256(kSigning, []byte(stringToSign)))

	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=AKIDEXAMPLE/"+scope+", SignedHeaders=host;x-amz-content-sha256;x-amz-date, Signature=") {
		t.Fatalf("unexpected Authorization shape: %s", auth)
	}
	if !strings.HasSuffix(auth, wantSig) {
		t.Errorf("signature mismatch\n got: %s\nwant suffix: %s", auth, wantSig)
	}
}

// Signing must be a pure function of its inputs — same inputs, same output.
func TestSigV4IsDeterministic(t *testing.T) {
	at := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
	sign := func() string {
		req, _ := http.NewRequest(http.MethodPut, "https://b.example.com/k", nil)
		if err := signAWSv4At(req, []byte("x"), "AK", "SK", "auto", "s3", at); err != nil {
			t.Fatalf("sign: %v", err)
		}
		return req.Header.Get("Authorization")
	}
	if a, b := sign(), sign(); a != b {
		t.Errorf("signature not deterministic:\n%s\n%s", a, b)
	}
}

// Any change to a signed input must change the signature, or the signature is
// not actually covering that input.
func TestSigV4CoversItsInputs(t *testing.T) {
	at := time.Date(2024, 3, 1, 12, 0, 0, 0, time.UTC)
	sign := func(url, secret string, body []byte, when time.Time) string {
		req, _ := http.NewRequest(http.MethodPut, url, nil)
		if err := signAWSv4At(req, body, "AK", secret, "auto", "s3", when); err != nil {
			t.Fatalf("sign: %v", err)
		}
		return req.Header.Get("Authorization")
	}

	base := sign("https://b.example.com/k", "SK", []byte("x"), at)

	cases := map[string]string{
		"different key":    sign("https://b.example.com/other", "SK", []byte("x"), at),
		"different body":   sign("https://b.example.com/k", "SK", []byte("y"), at),
		"different secret": sign("https://b.example.com/k", "SK2", []byte("x"), at),
		"different host":   sign("https://c.example.com/k", "SK", []byte("x"), at),
		"different time":   sign("https://b.example.com/k", "SK", []byte("x"), at.Add(time.Hour)),
	}
	for name, got := range cases {
		if got == base {
			t.Errorf("%s produced an identical signature; that input is not covered", name)
		}
	}
}

// AWS query escaping: unreserved passes, everything else is percent-encoded
// per byte — including "/" as %2F, which is where this differs from path
// encoding.
func TestAWSEscape(t *testing.T) {
	cases := []struct{ in, want string }{
		{"abcXYZ019", "abcXYZ019"},
		{"-_.~", "-_.~"},
		{"a b", "a%20b"},
		{"a/b", "a%2Fb"},
		{"a+b", "a%2Bb"},
		{"a=b&c", "a%3Db%26c"},
		{"café", "caf%C3%A9"}, // multi-byte UTF-8, encoded per byte
		{"", ""},
	}
	for _, tc := range cases {
		if got := awsEscape(tc.in); got != tc.want {
			t.Errorf("awsEscape(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Canonical query strings must be sorted by key, and by value within a key.
func TestCanonicalQueryStringSorts(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "https://h/x?b=2&a=1&c=3&a=0", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if got, want := canonicalQueryString(req), "a=0&a=1&b=2&c=3"; got != want {
		t.Errorf("canonicalQueryString = %q, want %q", got, want)
	}
}

// sha256Hex/hmacSHA256 underpin everything above; pin them to known values.
func TestSigV4Primitives(t *testing.T) {
	// echo -n "" | sha256sum
	if got, want := sha256Hex(nil), "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"; got != want {
		t.Errorf("sha256Hex(nil) = %s, want %s", got, want)
	}
	mac := hmac.New(sha256.New, []byte("k"))
	mac.Write([]byte("d"))
	if got, want := hex.EncodeToString(hmacSHA256([]byte("k"), []byte("d"))), hex.EncodeToString(mac.Sum(nil)); got != want {
		t.Errorf("hmacSHA256 disagrees with crypto/hmac: %s vs %s", got, want)
	}
}
