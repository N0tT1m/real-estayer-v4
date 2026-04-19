package service

import (
	"math"
	"testing"
	"time"

	"github.com/realestayer/v4/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestHaversineKm(t *testing.T) {
	// SFO → NRT is ~8,270 km.
	got := haversineKm(37.6188, -122.3750, 35.7719, 140.3928)
	if math.Abs(got-8270) > 80 {
		t.Fatalf("SFO→NRT haversine = %.0f; want ~8270", got)
	}
}

func TestEstimateTripFlightByDistance(t *testing.T) {
	trip := &models.Trip{
		StartDate: time.Now(), EndDate: time.Now().Add(3 * 24 * time.Hour),
		Items: []models.TripItem{
			{ID: primitive.NewObjectID(), Type: models.TripItemTypeFlight, Title: "SFO→NRT",
				Details: map[string]any{"distance_km": 8270.0}},
		},
	}
	est := NewCarbonService().EstimateTrip(trip)
	if est.TotalKg < 1500 {
		t.Fatalf("long-haul flight should be >= 1500 kg; got %.1f", est.TotalKg)
	}
	if est.Equivalents.TreesForOneYear == 0 {
		t.Errorf("expected non-zero tree equivalent")
	}
}

func TestEstimateTripHotelUsesNights(t *testing.T) {
	trip := &models.Trip{
		StartDate: time.Now(), EndDate: time.Now().Add(3 * 24 * time.Hour), // 4 nights
		Items: []models.TripItem{
			{ID: primitive.NewObjectID(), Type: models.TripItemTypeHotel, Title: "Hotel X"},
		},
	}
	est := NewCarbonService().EstimateTrip(trip)
	// 4 nights × 12 kg = 48 kg.
	if math.Abs(est.TotalKg-48) > 0.2 {
		t.Fatalf("hotel emissions = %.1f; want ~48", est.TotalKg)
	}
}

func TestFlightEmissionsTiers(t *testing.T) {
	short := flightEmissionsKg(500)
	long := flightEmissionsKg(5000)
	if short > long || short <= 0 || long <= 0 {
		t.Fatalf("short=%v long=%v", short, long)
	}
	// Long-haul should be *lower* per-km but total higher.
	if long/5000 >= short/500 {
		t.Fatalf("long-haul per-km should be lower than short-haul")
	}
}
