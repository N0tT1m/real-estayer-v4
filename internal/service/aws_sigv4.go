package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// signAWSv4 signs an http.Request with the AWS Signature Version 4 algorithm
// that S3, R2, MinIO, Wasabi and friends all accept. Body is expected in full
// (we SHA-256 it); streaming signed uploads aren't supported here but 10 MB
// photos fit comfortably in memory.
//
// Strictly the minimal shape needed for PUT requests against
// `s3.{region}.amazonaws.com/{bucket}/{key}` or an S3-compatible endpoint.
func signAWSv4(req *http.Request, body []byte, accessKey, secretKey, region, service string) error {
	return signAWSv4At(req, body, accessKey, secretKey, region, service, time.Now())
}

// signAWSv4At is signAWSv4 with the signing instant supplied by the caller.
//
// Split out purely so the signature is testable: a signature is a pure
// function of its inputs, but reading the clock inside made every output
// unreproducible, which is why this had no tests. Signing code is exactly
// where a silent, untested mistake costs you — every upload fails with an
// opaque 403, or worse, works until a request happens to contain a character
// the escaping got wrong.
func signAWSv4At(req *http.Request, body []byte, accessKey, secretKey, region, service string, at time.Time) error {
	now := at.UTC()
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")

	hostHeader := req.URL.Host
	req.Header.Set("Host", hostHeader)
	bodyHash := sha256Hex(body)
	req.Header.Set("x-amz-content-sha256", bodyHash)
	req.Header.Set("x-amz-date", amzDate)

	canonURI := req.URL.EscapedPath()
	if canonURI == "" {
		canonURI = "/"
	}
	canonQuery := canonicalQueryString(req)

	// Canonical headers — lowercase names, sorted, values trimmed.
	hdrs := []string{"host", "x-amz-content-sha256", "x-amz-date"}
	sort.Strings(hdrs)
	var canonHeaders strings.Builder
	for _, h := range hdrs {
		fmt.Fprintf(&canonHeaders, "%s:%s\n", h, strings.TrimSpace(req.Header.Get(h)))
	}
	signedHeaders := strings.Join(hdrs, ";")

	canonicalReq := strings.Join([]string{
		req.Method,
		canonURI,
		canonQuery,
		canonHeaders.String(),
		signedHeaders,
		bodyHash,
	}, "\n")

	scope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, region, service)
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		sha256Hex([]byte(canonicalReq)),
	}, "\n")

	kDate := hmacSHA256([]byte("AWS4"+secretKey), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	kSigning := hmacSHA256(kService, []byte("aws4_request"))
	signature := hex.EncodeToString(hmacSHA256(kSigning, []byte(stringToSign)))

	auth := fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		accessKey, scope, signedHeaders, signature)
	req.Header.Set("Authorization", auth)
	return nil
}

func canonicalQueryString(req *http.Request) string {
	q := req.URL.Query()
	keys := make([]string, 0, len(q))
	for k := range q {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for i, k := range keys {
		vs := q[k]
		sort.Strings(vs)
		for j, v := range vs {
			if i+j > 0 {
				b.WriteByte('&')
			}
			b.WriteString(awsEscape(k))
			b.WriteByte('=')
			b.WriteString(awsEscape(v))
		}
	}
	return b.String()
}

// awsEscape implements AWS's URI escaping for *query* components: the
// unreserved set (A-Z a-z 0-9 - _ . ~) passes through, everything else is
// percent-encoded byte by byte, including "/" as %2F and a space as %20.
//
// Note this is deliberately not the rule for the canonical *path*, where "/"
// must stay literal — that half comes from url.URL.EscapedPath() above rather
// than from this function. An earlier comment here claimed slashes were left
// alone, which described the path rule while the code implemented the query
// rule; the code was right.
func awsEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ' ':
			b.WriteString("%20")
		case r < 0x80 && (r == '-' || r == '_' || r == '.' || r == '~' ||
			(r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')):
			b.WriteRune(r)
		default:
			for _, by := range []byte(string(r)) {
				fmt.Fprintf(&b, "%%%02X", by)
			}
		}
	}
	return b.String()
}

func sha256Hex(b []byte) string {
	h := sha256.New()
	h.Write(b)
	return hex.EncodeToString(h.Sum(nil))
}

func hmacSHA256(key, data []byte) []byte {
	m := hmac.New(sha256.New, key)
	m.Write(data)
	return m.Sum(nil)
}
