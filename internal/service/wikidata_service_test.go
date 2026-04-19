package service

import (
	"reflect"
	"testing"
)

func TestTrimISODate(t *testing.T) {
	cases := map[string]string{
		"1147-01-01T00:00:00Z": "1147-01-01",
		"2020-05-01T12:34:56":  "2020-05-01",
		"2020-05-01":           "2020-05-01",
		"":                     "",
	}
	for in, want := range cases {
		if got := trimISODate(in); got != want {
			t.Errorf("trimISODate(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestSplitPipeDedupesAndLimits(t *testing.T) {
	got := splitPipe("Alice|Bob|alice|  |Charlie|Dave|Eve|Bob", 3)
	want := []string{"Alice", "Bob", "alice"}
	// Dedup is case-sensitive ("alice" != "Alice") — verify that contract.
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitPipe = %v; want %v", got, want)
	}
}

func TestSplitPipeEmpty(t *testing.T) {
	if splitPipe("", 5) != nil {
		t.Errorf("empty input should return nil")
	}
	// Whitespace/empty segments yield an empty (not necessarily nil) slice —
	// both are fine for the UI which only looks at len().
	if got := splitPipe("||  ||", 5); len(got) != 0 {
		t.Errorf("whitespace/empty segments → zero-length; got %v", got)
	}
}
