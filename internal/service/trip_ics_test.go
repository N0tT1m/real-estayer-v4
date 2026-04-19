package service

import (
	"strings"
	"testing"
	"time"

	"github.com/realestayer/v4/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestRenderTripICS(t *testing.T) {
	start := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	flightStart := start.Add(10 * time.Hour)
	flightEnd := flightStart.Add(3 * time.Hour)
	trip := &models.Trip{
		ID:        primitive.NewObjectID(),
		Name:      "Tokyo, Japan",
		Description: "Cherry blossom, ramen, temples.",
		StartDate: start,
		EndDate:   start.Add(5 * 24 * time.Hour),
		Items: []models.TripItem{
			{
				ID:        primitive.NewObjectID(),
				Type:      models.TripItemTypeFlight,
				Title:     "SFO → NRT",
				Provider:  "amadeus",
				StartTime: &flightStart,
				EndTime:   &flightEnd,
				Notes:     "Check in 24h ahead.",
			},
			{
				ID:    primitive.NewObjectID(),
				Type:  models.TripItemTypeActivity,
				Title: "Meiji Shrine walk",
				// No times → becomes all-day on start date.
			},
		},
	}

	ics := RenderTripICS(trip)
	checks := []string{
		"BEGIN:VCALENDAR",
		"PRODID:-//Real-Estayer//Trip//EN",
		"SUMMARY:Trip: Tokyo\\, Japan", // escaped comma
		"SUMMARY:Flight: SFO → NRT",
		"SUMMARY:Activity: Meiji Shrine walk",
		"DTSTART:20250601T100000Z",
		"DTEND:20250601T130000Z",
		"DTSTART;VALUE=DATE:20250601",
		"END:VEVENT",
		"END:VCALENDAR",
	}
	for _, c := range checks {
		if !strings.Contains(ics, c) {
			t.Errorf("ICS missing %q", c)
		}
	}
}

func TestICSEscape(t *testing.T) {
	got := icsEscape(`a, b; c\nd\n`)
	want := `a\, b\; c\\nd\\n`
	if got != want {
		t.Errorf("icsEscape(...) = %q; want %q", got, want)
	}
}
