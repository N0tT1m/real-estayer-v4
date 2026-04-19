package service

import (
	"testing"
	"time"
)

func TestParseRFC3339Lax(t *testing.T) {
	// Proper RFC3339
	a := parseRFC3339Lax("2025-06-01T10:15:00Z")
	if a.IsZero() || a.Year() != 2025 {
		t.Errorf("proper RFC3339 failed: %v", a)
	}
	// AviationStack sometimes strips the zone.
	b := parseRFC3339Lax("2025-06-01T10:15:00")
	if b.IsZero() {
		t.Errorf("TZ-less form should parse")
	}
	// Garbage → zero.
	if !parseRFC3339Lax("not a date").IsZero() {
		t.Errorf("garbage should return zero time")
	}
	if !parseRFC3339Lax("").IsZero() {
		t.Errorf("empty → zero")
	}
	_ = time.Now() // silence import
}
