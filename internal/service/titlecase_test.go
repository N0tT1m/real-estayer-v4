package service

import (
	"strings"
	"testing"
)

// titleWords replaced the deprecated strings.Title. This pins the replacement
// to the original's exact output so the swap can be shown to be behaviour
// preserving rather than merely plausible.
func TestTitleWordsMatchesDeprecatedStringsTitle(t *testing.T) {
	cases := []string{
		"",
		"a",
		"united states",
		"UNITED STATES",
		"united states of america",
		"hertz",
		"HERTZ",
		"delayed",
		"on time",
		"cancelled",
		"bosnia and herzegovina",
		"saint kitts and nevis",
		"cote d'ivoire",
		"guinea-bissau",
		"timor-leste",
		"JFK LAX",
		"  leading space",
		"trailing space  ",
		"multiple   spaces",
		"mixed CaSe input",
		"123 numeric start",
		"under_score",
		"påskeøen",
		"ÅLAND ISLANDS",
	}

	for _, in := range cases {
		//nolint:staticcheck // deliberately comparing against the deprecated
		// function this code replaced; that is the point of the test.
		want := strings.Title(in)
		if got := titleWords(in); got != want {
			t.Errorf("titleWords(%q) = %q, strings.Title gave %q", in, got, want)
		}
	}
}

func TestTitleWordsKeepsWordTails(t *testing.T) {
	// Guards the documented difference from x/text cases.Title, which would
	// return "Jfk Lax" here and quietly change rendered flight subjects.
	if got := titleWords("JFK LAX"); got != "JFK LAX" {
		t.Errorf("titleWords(%q) = %q, want unchanged tails", "JFK LAX", got)
	}
}

func TestFormatCountryNameNormalisesAllCaps(t *testing.T) {
	if got := formatCountryName("UNITED STATES"); got != "United States" {
		t.Errorf("formatCountryName(UNITED STATES) = %q", got)
	}
	// Mixed-case names are passed through untouched.
	if got := formatCountryName("United Kingdom"); got != "United Kingdom" {
		t.Errorf("formatCountryName(United Kingdom) = %q", got)
	}
}
