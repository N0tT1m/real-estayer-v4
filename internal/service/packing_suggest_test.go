package service

import (
	"strings"
	"testing"
	"time"

	"github.com/realestayer/v4/internal/models"
)

func tripOfDays(n int, month time.Month) *models.Trip {
	start := time.Date(2025, month, 1, 0, 0, 0, 0, time.UTC)
	return &models.Trip{
		StartDate: start,
		EndDate:   start.AddDate(0, 0, n-1),
	}
}

func TestSuggestPackingBaseItems(t *testing.T) {
	pl := SuggestPackingList(tripOfDays(3, time.June))
	names := itemNames(pl)
	for _, needed := range []string{"Phone + charger", "Underwear", "T-shirts / tops", "Toothbrush + paste"} {
		if !containsStr(names, needed) {
			t.Errorf("packing list missing %q. Got %v", needed, names)
		}
	}
}

func TestSuggestPackingScalesWithNights(t *testing.T) {
	short := SuggestPackingList(tripOfDays(2, time.June))
	long := SuggestPackingList(tripOfDays(8, time.June))
	shortUnderwear := findQty(short, "Underwear")
	longUnderwear := findQty(long, "Underwear")
	if longUnderwear <= shortUnderwear {
		t.Errorf("long trip should pack more underwear than short (%d vs %d)", longUnderwear, shortUnderwear)
	}
	if findQty(long, "T-shirts / tops") > 8 {
		t.Errorf("t-shirt count should clamp to 8; got %d", findQty(long, "T-shirts / tops"))
	}
}

func TestSuggestPackingFlightAddsPassport(t *testing.T) {
	trip := tripOfDays(3, time.June)
	trip.Items = []models.TripItem{{Type: models.TripItemTypeFlight, Title: "SFO→NRT"}}
	pl := SuggestPackingList(trip)
	names := itemNames(pl)
	for _, needed := range []string{"Passport", "Universal travel adapter"} {
		if !containsStr(names, needed) {
			t.Errorf("flight trip should include %q. Got %v", needed, names)
		}
	}
}

func TestSuggestPackingBeachDestinationAddsSwim(t *testing.T) {
	trip := tripOfDays(3, time.July)
	trip.Destinations = []models.TripDestination{{Name: "Miami Beach"}}
	pl := SuggestPackingList(trip)
	names := itemNames(pl)
	if !containsStr(names, "Swimwear") || !containsStr(names, "Sunscreen") {
		t.Errorf("beach destination should suggest swimwear/sunscreen. Got %v", names)
	}
}

func TestSuggestPackingColdDestinationAddsWarmLayers(t *testing.T) {
	trip := tripOfDays(4, time.January)
	trip.Destinations = []models.TripDestination{{Name: "Iceland"}}
	pl := SuggestPackingList(trip)
	names := itemNames(pl)
	if !containsStr(names, "Warm jacket") {
		t.Errorf("cold destination should suggest warm jacket. Got %v", names)
	}
}

func TestSuggestPackingMarksAutoSuggested(t *testing.T) {
	pl := SuggestPackingList(tripOfDays(3, time.June))
	for _, p := range pl {
		if !p.AutoSuggested {
			t.Errorf("all suggested items should have AutoSuggested=true; %s didn't", p.Name)
			break
		}
	}
}

func TestSuggestChecklistDependsOnItems(t *testing.T) {
	trip := tripOfDays(3, time.June)
	plain := SuggestChecklist(trip)
	for _, c := range plain {
		if strings.Contains(c.Title, "Check-in online") {
			t.Errorf("non-flight trip shouldn't suggest check-in: %+v", plain)
		}
	}
	trip.Items = []models.TripItem{{Type: models.TripItemTypeFlight, Title: "x"}}
	withFlight := SuggestChecklist(trip)
	found := false
	for _, c := range withFlight {
		if strings.Contains(c.Title, "Check-in online") {
			found = true
		}
	}
	if !found {
		t.Errorf("flight trip should suggest online check-in")
	}
}

func TestDaysBetween(t *testing.T) {
	if daysBetween(tripOfDays(1, time.January)) != 1 {
		t.Errorf("single-day trip should count as 1 day")
	}
	if daysBetween(tripOfDays(5, time.January)) != 5 {
		t.Errorf("5-day trip should count as 5 days")
	}
}

// ---- helpers ----

func itemNames(items []models.PackingItem) []string {
	out := make([]string, 0, len(items))
	for _, i := range items {
		out = append(out, i.Name)
	}
	return out
}
func findQty(items []models.PackingItem, name string) int {
	for _, i := range items {
		if i.Name == name {
			return i.Quantity
		}
	}
	return 0
}
func containsStr(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
