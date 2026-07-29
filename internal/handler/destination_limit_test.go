package handler

import (
	"strconv"
	"testing"
)

// boundedLimit is what stops an unauthenticated caller turning `?limit=` into
// "return the whole collection". FeaturedAPI shipped without this bound while
// SearchAPI had it, so the cases below pin both ends of the range.
func TestBoundedLimit(t *testing.T) {
	const def, max = 6, 100

	cases := []struct {
		name string
		raw  string
		want int
	}{
		{"absent falls back", "", def},
		{"in range is honoured", "24", 24},
		{"exactly max is honoured", strconv.Itoa(max), max},
		{"one over max falls back", strconv.Itoa(max + 1), def},
		{"collection dump falls back", "999999", def},
		{"zero falls back", "0", def},
		{"negative falls back", "-5", def},
		{"non-numeric falls back", "all", def},
		{"empty-ish junk falls back", "  ", def},
		{"overflow falls back", "99999999999999999999", def},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := boundedLimit(tc.raw, def, max); got != tc.want {
				t.Errorf("boundedLimit(%q, %d, %d) = %d, want %d",
					tc.raw, def, max, got, tc.want)
			}
		})
	}
}

// The cap has to actually be enforceable — a default above the ceiling would
// mean the fallback path itself exceeds the bound.
func TestBoundedLimitDefaultsWithinCap(t *testing.T) {
	for _, def := range []int{6, 24} {
		if def > maxPageLimit {
			t.Errorf("default %d exceeds maxPageLimit %d", def, maxPageLimit)
		}
	}
}
