package service

import (
	"context"
	"testing"
)

func TestVisaKnownRoutes(t *testing.T) {
	s := NewVisaService()
	cases := []struct {
		from, to string
		kind     Kind
	}{
		{"US", "JP", KindVisaFree},
		{"US", "AU", KindETA},
		{"US", "CN", KindVisaRequired},
		{"US", "AE", KindVisaOnArrival},
		{"GB", "US", KindETA},
		{"IN", "US", KindVisaRequired},
	}
	for _, c := range cases {
		got := s.Check(context.Background(), c.from, c.to)
		if got.Kind != c.kind {
			t.Errorf("%s→%s: got %s; want %s", c.from, c.to, got.Kind, c.kind)
		}
		if got.UpdatedAt == "" {
			t.Errorf("%s→%s: UpdatedAt should be stamped", c.from, c.to)
		}
	}
}

func TestVisaSameCountryFree(t *testing.T) {
	s := NewVisaService()
	r := s.Check(context.Background(), "JP", "jp")
	if r.Kind != KindVisaFree {
		t.Errorf("same-country should be visa-free; got %s", r.Kind)
	}
}

func TestVisaUnknownReturnsNotAvailable(t *testing.T) {
	s := NewVisaService()
	r := s.Check(context.Background(), "XX", "ZZ")
	if r.Kind != KindNotAvailable {
		t.Errorf("unknown route should be not_available, got %s", r.Kind)
	}
	// Must not pretend it's visa-free — that'd be a safety bug.
	if r.Label == "" {
		t.Errorf("should include helpful label")
	}
}

func TestVisaEmptyInputGivesHint(t *testing.T) {
	s := NewVisaService()
	r := s.Check(context.Background(), "", "FR")
	if r.Kind != KindNotAvailable {
		t.Errorf("empty citizenship → not_available, got %s", r.Kind)
	}
}
