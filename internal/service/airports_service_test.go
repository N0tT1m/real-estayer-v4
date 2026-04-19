package service

import (
	"context"
	"math"
	"testing"
)

func TestAirportLookupByIATA(t *testing.T) {
	s := NewAirportService()
	got, err := s.Lookup(context.Background(), "sfo")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.City != "San Francisco" {
		t.Fatalf("SFO → %+v", got)
	}
}

func TestAirportDistanceSFONRT(t *testing.T) {
	s := NewAirportService()
	km, err := s.DistanceBetween(context.Background(), "SFO", "NRT")
	if err != nil {
		t.Fatal(err)
	}
	// Real distance ~8,270 km.
	if math.Abs(km-8270) > 100 {
		t.Fatalf("SFO→NRT = %.0f; want ~8270", km)
	}
}

func TestAirportSearch(t *testing.T) {
	s := NewAirportService()
	hits := s.Search(context.Background(), "tokyo", 5)
	found := false
	for _, a := range hits {
		if a.IATA == "HND" || a.IATA == "NRT" {
			found = true
		}
	}
	if !found {
		t.Fatalf("search tokyo did not return Haneda/Narita: %+v", hits)
	}
}

func TestAirportLookupUnknown(t *testing.T) {
	s := NewAirportService()
	a, err := s.Lookup(context.Background(), "ZZZ")
	if err != nil {
		t.Fatal(err)
	}
	if a != nil {
		t.Fatalf("expected nil for unknown IATA, got %+v", a)
	}
}
