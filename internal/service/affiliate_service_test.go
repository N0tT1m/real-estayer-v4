package service

import (
	"strings"
	"testing"
	"time"
)

func TestAffiliateForDestinationContainsKeyProviders(t *testing.T) {
	s := NewAffiliateService("test-tag")
	in := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	out := time.Date(2025, 6, 5, 0, 0, 0, 0, time.UTC)
	links := s.ForDestination("Lisbon", &in, &out, "LAX")
	if len(links.Stays) == 0 || len(links.Flights) == 0 {
		t.Fatalf("expected stays and flights quick-links")
	}
	joined := ""
	for _, l := range append(append(links.Stays, links.Flights...), links.Info...) {
		joined += l.URL + "\n"
	}
	for _, expected := range []string{"booking.com", "airbnb.com", "skyscanner.com", "wikivoyage.org"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("expected a link to %s in: %s", expected, joined)
		}
	}
	if !strings.Contains(joined, "2025-06-01") {
		t.Errorf("check-in date not propagated: %s", joined)
	}
}
