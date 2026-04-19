package service

import (
	"strings"
	"testing"
)

func TestPM25Label(t *testing.T) {
	cases := []struct {
		v    float64
		want string
	}{
		{0, ""},
		{-1, ""},
		{10, "Good"},
		{12, "Good"},
		{30, "Moderate"},
		{50, "Unhealthy for sensitive groups"},
		{100, "Unhealthy"},
		{200, "Very unhealthy"},
		{300, "Hazardous"},
	}
	for _, c := range cases {
		if got := pm25Label(c.v); got != c.want {
			t.Errorf("pm25Label(%v) = %q; want %q", c.v, got, c.want)
		}
	}
}

func TestHumanizeDuration(t *testing.T) {
	cases := []struct {
		seconds float64
		want    string
	}{
		{0, "less than a minute"},
		{59, "less than a minute"},
		{60, "1 min"},
		{90, "1 min"}, // truncates toward zero
		{1800, "30 min"},
		{3600, "1h"},
		{5400, "1h 30min"},
		{7200, "2h"},
	}
	for _, c := range cases {
		if got := humanizeDuration(c.seconds); got != c.want {
			t.Errorf("humanize(%v) = %q; want %q", c.seconds, got, c.want)
		}
	}
}

func TestURLEncodeHandlesSpacesAndSymbols(t *testing.T) {
	got := urlEncode("São Paulo & beach")
	for _, expected := range []string{"%20", "%26", "S%C3%A3o"} {
		if !strings.Contains(got, expected) {
			t.Errorf("urlEncode output missing %q: got %q", expected, got)
		}
	}
	// Reserved chars in unreserved set should pass through untouched.
	if urlEncode("safe-word.1_0~") != "safe-word.1_0~" {
		t.Errorf("unreserved chars should pass through: %q", urlEncode("safe-word.1_0~"))
	}
}

func TestRound1(t *testing.T) {
	cases := map[float64]float64{
		1.24:  1.2,
		1.25:  1.3, // rounds half up
		1.99:  2.0,
		-1.25: -1.2, // consistent banker-less behaviour for negatives
		0:     0,
	}
	for in, want := range cases {
		if got := round1(in); got != want {
			t.Errorf("round1(%v) = %v; want %v", in, got, want)
		}
	}
}

func TestRound2TripExpense(t *testing.T) {
	if round2(1.234) != 1.23 {
		t.Errorf("round2(1.234) = %v", round2(1.234))
	}
	if round2(1.235) != 1.24 {
		t.Errorf("round2(1.235) = %v", round2(1.235))
	}
}

func TestSafeIndexers(t *testing.T) {
	strs := []string{"a", "b"}
	if safeIndexStr(strs, 0) != "a" || safeIndexStr(strs, 2) != "" {
		t.Errorf("safeIndexStr bounds check failed")
	}
	fs := []float64{1.5}
	if safeIndexF(fs, 0) != 1.5 || safeIndexF(fs, 5) != 0 {
		t.Errorf("safeIndexF bounds check failed")
	}
	is := []int{42}
	if safeIndexI(is, 0) != 42 || safeIndexI(is, 10) != 0 {
		t.Errorf("safeIndexI bounds check failed")
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if firstNonEmpty("", "  ", "x") != "x" {
		t.Errorf("firstNonEmpty should skip empty and whitespace-only")
	}
	if firstNonEmpty("", "") != "" {
		t.Errorf("all empty → empty")
	}
	if firstNonEmpty(" a ", "b") != "a" {
		t.Errorf("should trim and return first non-empty")
	}
}

func TestJoinNonEmpty(t *testing.T) {
	got := joinNonEmpty(", ", "a", "", "  ", "b")
	if got != "a, b" {
		t.Errorf("joinNonEmpty = %q; want %q", got, "a, b")
	}
	if joinNonEmpty(" / ") != "" {
		t.Errorf("no args → empty")
	}
}
