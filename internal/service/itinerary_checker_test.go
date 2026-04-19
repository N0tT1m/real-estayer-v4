package service

import (
	"testing"
	"time"

	"github.com/realestayer/v4/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func item(typ models.TripItemType, title string, start, end time.Time) models.TripItem {
	it := models.TripItem{
		ID:        primitive.NewObjectID(),
		Type:      typ,
		Title:     title,
		StartTime: &start,
	}
	if !end.IsZero() {
		it.EndTime = &end
	}
	return it
}

func TestConflictChecker_Overlap(t *testing.T) {
	base := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	trip := &models.Trip{
		StartDate: base.AddDate(0, 0, -1), EndDate: base.AddDate(0, 0, 7),
		Items: []models.TripItem{
			item(models.TripItemTypeActivity, "Museum", base, base.Add(2*time.Hour)),
			item(models.TripItemTypeActivity, "Cooking class", base.Add(1*time.Hour), base.Add(3*time.Hour)),
		},
	}
	ws := NewConflictChecker().Check(trip)
	if len(ws) == 0 || !contains(ws, "overlap") {
		t.Fatalf("expected overlap warning; got %+v", ws)
	}
}

func TestConflictChecker_TightConnection(t *testing.T) {
	base := time.Date(2025, 6, 1, 8, 0, 0, 0, time.UTC)
	trip := &models.Trip{
		StartDate: base, EndDate: base.AddDate(0, 0, 1),
		Items: []models.TripItem{
			item(models.TripItemTypeFlight, "UA1", base, base.Add(2*time.Hour)),
			// Only 30 minutes between arrival and next departure.
			item(models.TripItemTypeFlight, "UA2", base.Add(2*time.Hour+30*time.Minute), base.Add(5*time.Hour)),
		},
	}
	ws := NewConflictChecker().Check(trip)
	if !contains(ws, "tight_connection") {
		t.Errorf("expected tight_connection warning; got %+v", ws)
	}
}

func TestConflictChecker_CheckInBeforeArrival(t *testing.T) {
	base := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	trip := &models.Trip{
		StartDate: base, EndDate: base.AddDate(0, 0, 2),
		Items: []models.TripItem{
			// Flight lands at 17:00
			item(models.TripItemTypeFlight, "UA1", base.Add(13*time.Hour), base.Add(17*time.Hour)),
			// But the listing check-in is at 15:00 — impossible.
			item(models.TripItemTypeListing, "Loft", base.Add(15*time.Hour), base.Add(16*time.Hour)),
		},
	}
	ws := NewConflictChecker().Check(trip)
	if !contains(ws, "checkin_before_arrival") {
		t.Errorf("expected checkin_before_arrival; got %+v", ws)
	}
	// Severity should be error (blocks the day).
	found := false
	for _, w := range ws {
		if w.Code == "checkin_before_arrival" && w.Severity == SeverityError {
			found = true
		}
	}
	if !found {
		t.Errorf("checkin warning must be error-severity")
	}
}

func TestConflictChecker_OutsideTripDates(t *testing.T) {
	start := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	trip := &models.Trip{
		StartDate: start, EndDate: start.AddDate(0, 0, 3),
		Items: []models.TripItem{
			// Scheduled a week before the trip starts.
			item(models.TripItemTypeActivity, "Pre-trip dinner", start.AddDate(0, 0, -7), time.Time{}),
		},
	}
	ws := NewConflictChecker().Check(trip)
	if !contains(ws, "outside_trip_dates") {
		t.Errorf("expected outside_trip_dates; got %+v", ws)
	}
}

func TestConflictChecker_NoFlightOnly(t *testing.T) {
	start := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	trip := &models.Trip{
		StartDate: start, EndDate: start.AddDate(0, 0, 3),
		Destinations: []models.TripDestination{{Name: "Lisbon"}},
		Items: []models.TripItem{
			item(models.TripItemTypeHotel, "Hotel", start.Add(15*time.Hour), time.Time{}),
		},
	}
	ws := NewConflictChecker().Check(trip)
	if !contains(ws, "no_flight") {
		t.Errorf("expected no_flight info; got %+v", ws)
	}
}

func TestConflictChecker_NilTripSafe(t *testing.T) {
	if got := NewConflictChecker().Check(nil); got != nil {
		t.Errorf("nil trip should return nil, got %+v", got)
	}
}

func TestConflictChecker_SeverityOrdering(t *testing.T) {
	base := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	trip := &models.Trip{
		StartDate: base, EndDate: base.AddDate(0, 0, 3),
		Items: []models.TripItem{
			item(models.TripItemTypeFlight, "UA1", base, base.Add(2*time.Hour)),
			item(models.TripItemTypeHotel, "Hotel", base.Add(time.Hour), base.Add(time.Hour*5)), // causes check-in before arrival
			item(models.TripItemTypeActivity, "Pre-dinner", base.AddDate(0, 0, -10), time.Time{}),
		},
	}
	ws := NewConflictChecker().Check(trip)
	if len(ws) < 2 {
		t.Fatalf("expected multiple warnings, got %+v", ws)
	}
	if ws[0].Severity != SeverityError && ws[0].Severity != SeverityWarn {
		t.Errorf("first warning should be the highest severity, got %s", ws[0].Severity)
	}
	if ws[len(ws)-1].Severity != SeverityInfo {
		t.Errorf("last warning should be info-level, got %s", ws[len(ws)-1].Severity)
	}
}

func contains(ws []Warning, code string) bool {
	for _, w := range ws {
		if w.Code == code {
			return true
		}
	}
	return false
}
