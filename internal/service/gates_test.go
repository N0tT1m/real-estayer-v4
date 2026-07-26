package service

// Each external integration that depends on a key has the same contract:
// Configured() reports whether it's usable, and the caller receives a sentinel
// error when it isn't. These tests lock that contract in so a misconfigured
// prod never silently turns an integration into a request that 401s on the
// upstream provider.

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEventsConfiguredGate(t *testing.T) {
	if NewEventsService("").Configured() {
		t.Errorf("empty key → not configured")
	}
	if !NewEventsService("anything").Configured() {
		t.Errorf("non-empty key → configured")
	}
}

func TestAIItineraryConfiguredAndErrSentinel(t *testing.T) {
	s := NewAIItineraryService("", "", "")
	if s.Configured() {
		t.Errorf("empty key → not configured")
	}
	_, err := s.Generate(context.Background(), ItineraryRequest{Destination: "Lisbon", StartDate: time.Now(), EndDate: time.Now().Add(24 * time.Hour)})
	if !errors.Is(err, ErrAIItineraryNotConfigured) {
		t.Errorf("expected ErrAIItineraryNotConfigured, got %v", err)
	}
}

func TestAIItineraryValidation(t *testing.T) {
	s := NewAIItineraryService("key", "claude-test", "")
	if _, err := s.Generate(context.Background(), ItineraryRequest{}); err == nil {
		t.Errorf("expected error for missing destination")
	}
	if _, err := s.Generate(context.Background(), ItineraryRequest{Destination: "X",
		StartDate: time.Date(2025, 1, 2, 0, 0, 0, 0, time.UTC),
		EndDate:   time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)}); err == nil {
		t.Errorf("expected error for reversed dates")
	}
}

func TestFlightStatusConfiguredAndErrSentinel(t *testing.T) {
	s := NewFlightStatusService("")
	if s.Configured() {
		t.Errorf("empty key → not configured")
	}
	_, err := s.Lookup(context.Background(), "BA283", "")
	if !errors.Is(err, ErrFlightStatusNotConfigured) {
		t.Errorf("expected ErrFlightStatusNotConfigured, got %v", err)
	}
}

func TestFlightStatusEmptyNumberRejected(t *testing.T) {
	s := NewFlightStatusService("key")
	_, err := s.Lookup(context.Background(), "", "")
	if err == nil {
		t.Errorf("expected error for empty flight number")
	}
}

func TestUnsplashConfiguredGate(t *testing.T) {
	if NewUnsplashService("").Configured() {
		t.Errorf("empty → not configured")
	}
	photo, err := NewUnsplashService("").ForQuery(context.Background(), "Lisbon")
	if err != nil || photo != nil {
		t.Errorf("unconfigured Unsplash should return (nil, nil), got %v / %v", photo, err)
	}
}

func TestAirQualityEmptyKeyReturnsNil(t *testing.T) {
	s := NewAirQualityService("")
	aq, err := s.Get(context.Background(), 38.7, -9.1, 0)
	if err != nil {
		t.Errorf("unconfigured AQ should not error: %v", err)
	}
	if aq != nil {
		t.Errorf("unconfigured AQ should return nil, got %+v", aq)
	}
}

func TestNatureEBirdGate(t *testing.T) {
	s := NewNatureService("")
	_, err := s.EBirdRecent(context.Background(), 38.7, -9.1, 0, 0)
	if !errors.Is(err, ErrEBirdNotConfigured) {
		t.Errorf("expected ErrEBirdNotConfigured, got %v", err)
	}
}
