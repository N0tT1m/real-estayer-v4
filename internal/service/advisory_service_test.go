package service

import (
	"context"
	"testing"
)

func TestAdvisoryUnknownCountryReturnsDefault(t *testing.T) {
	s := NewAdvisoryService()
	a := s.ForCountry(context.Background(), "ZZ")
	if a == nil || a.Level != 1 || a.Source != "curated" {
		t.Fatalf("default advisory = %+v", a)
	}
	if a.UpdatedAt == "" {
		t.Errorf("UpdatedAt should be stamped")
	}
}

func TestAdvisoryKnownCountryReturnsCurated(t *testing.T) {
	s := NewAdvisoryService()
	cases := map[string]int{
		"JP": 1, "FR": 2, "MX": 3, "CN": 3,
	}
	for code, expectedLevel := range cases {
		a := s.ForCountry(context.Background(), code)
		if a.Level != expectedLevel {
			t.Errorf("%s: level = %d; want %d", code, a.Level, expectedLevel)
		}
		if a.LevelLabel == "" || a.Summary == "" {
			t.Errorf("%s: missing fields %+v", code, a)
		}
	}
}

func TestAdvisoryCaseInsensitiveCode(t *testing.T) {
	s := NewAdvisoryService()
	a := s.ForCountry(context.Background(), " fr ")
	if a.Level != 2 {
		t.Fatalf("expected France level 2 with messy input, got %d", a.Level)
	}
}
