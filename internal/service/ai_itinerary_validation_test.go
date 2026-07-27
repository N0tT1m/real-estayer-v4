package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

// configuredItineraryService returns a service that passes the Configured()
// check without reaching any network. Every case below is rejected by
// validation before a request is built, so no stub server is needed — and if
// one of them ever stopped short-circuiting, the test would hang or fail rather
// than silently pass.
func configuredItineraryService() *AIItineraryService {
	return NewAIItineraryService("test-key", "", "")
}

// These validation failures used to be bare errors.New, so handlers mapped them
// to 502 Bad Gateway — a blank destination read to the client as though the
// upstream model had broken.
func TestGenerateRejectsBadInputAsInvalidRequest(t *testing.T) {
	start := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		req  ItineraryRequest
	}{
		{
			name: "blank destination",
			req:  ItineraryRequest{StartDate: start, EndDate: start.AddDate(0, 0, 3)},
		},
		{
			name: "end date before start",
			req: ItineraryRequest{
				Destination: "Lisbon",
				StartDate:   start,
				EndDate:     start.AddDate(0, 0, -2),
			},
		},
	}
	s := configuredItineraryService()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Generate(context.Background(), tc.req)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !errors.Is(err, ErrInvalidItineraryRequest) {
				t.Errorf("error = %v; want it to wrap ErrInvalidItineraryRequest", err)
			}
			if errors.Is(err, ErrAIItineraryNotConfigured) {
				t.Errorf("bad input misreported as a configuration problem: %v", err)
			}
		})
	}
}

func TestRefineRejectsBadInputAsInvalidRequest(t *testing.T) {
	cases := []struct {
		name string
		turn RefinementTurn
	}{
		{
			name: "missing prior itinerary",
			turn: RefinementTurn{Feedback: "more museums"},
		},
		{
			name: "blank feedback",
			turn: RefinementTurn{Prior: &Itinerary{}, Feedback: "   "},
		},
	}
	s := configuredItineraryService()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Refine(context.Background(), tc.turn)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !errors.Is(err, ErrInvalidItineraryRequest) {
				t.Errorf("error = %v; want it to wrap ErrInvalidItineraryRequest", err)
			}
		})
	}
}

// An unconfigured service must still report the configuration problem rather
// than the input problem — that ordering is what lets the UI hide the feature
// instead of showing a validation message.
func TestUnconfiguredServiceReportsConfigurationFirst(t *testing.T) {
	s := NewAIItineraryService("", "", "")
	_, err := s.Generate(context.Background(), ItineraryRequest{})
	if !errors.Is(err, ErrAIItineraryNotConfigured) {
		t.Errorf("error = %v; want ErrAIItineraryNotConfigured", err)
	}
	if errors.Is(err, ErrInvalidItineraryRequest) {
		t.Errorf("configuration failure misreported as bad input: %v", err)
	}
}
