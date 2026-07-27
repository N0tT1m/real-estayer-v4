package service

import (
	"strings"
	"testing"

	"github.com/realestayer/v4/internal/models"
)

func testCatalog() []models.Destination {
	return []models.Destination{
		{Name: "Lisbon", Country: "Portugal", Region: "Europe", AirportCode: "LIS", AvgDailyBudget: 120,
			Categories: []string{"city", "beach"}, Latitude: 38.7223, Longitude: -9.1393},
		{Name: "Reykjavik", Country: "Iceland", Region: "Europe", AirportCode: "KEF", AvgDailyBudget: 210,
			Categories: []string{"adventure", "nature"}},
		{Name: "Ho Chi Minh City", Country: "Vietnam", Region: "Asia", AirportCode: "SGN", AvgDailyBudget: 60,
			Categories: []string{"city", "food"}},
	}
}

// The whole point of the grounding step: a place the model invents must never
// reach the caller, and the invention must be visible rather than silently
// swallowed.
func TestMatchPicksDropsPlacesNotInCatalog(t *testing.T) {
	picks := []suggestPick{
		{Name: "Lisbon", Why: "surf breaks nearby"},
		{Name: "Hossegor", Why: "world-class beach break"}, // real place, not in catalog
		{Name: "Atlantis", Why: "hallucinated"},
	}

	got := matchPicks(testCatalog(), picks, 5)

	if len(got.Matches) != 1 {
		t.Fatalf("matches = %d, want only the catalog entry: %+v", len(got.Matches), got.Matches)
	}
	if got.Matches[0].Destination.Name != "Lisbon" {
		t.Errorf("matched %q, want Lisbon", got.Matches[0].Destination.Name)
	}
	if len(got.Dropped) != 2 {
		t.Fatalf("dropped = %v, want both off-catalog names reported", got.Dropped)
	}
	if got.CatalogSize != 3 {
		t.Errorf("catalog_size = %d, want 3", got.CatalogSize)
	}
}

// Actionable fields must come from the database row, not from the model — the
// model only supplies prose. A pick carries no coordinates or airport code, so
// if those survive, they came from the catalog.
func TestMatchPicksTakesFactsFromCatalogNotModel(t *testing.T) {
	got := matchPicks(testCatalog(), []suggestPick{{
		Name: "Lisbon", Why: "good waves", Activities: []string{"surf at Carcavelos"}, Timing: "spring",
	}}, 5)

	if len(got.Matches) != 1 {
		t.Fatalf("matches = %d", len(got.Matches))
	}
	m := got.Matches[0]
	if m.Destination.AirportCode != "LIS" || m.Destination.AvgDailyBudget != 120 {
		t.Errorf("facts should come from the catalog row, got %+v", m.Destination)
	}
	if m.Destination.Latitude == 0 || m.Destination.Longitude == 0 {
		t.Error("coordinates should be carried through from the catalog")
	}
	if m.Why != "good waves" || m.Timing != "spring" {
		t.Errorf("prose should come from the model, got why=%q timing=%q", m.Why, m.Timing)
	}
}

// Models drift on casing and spacing; that shouldn't cost a real match.
func TestMatchPicksNameMatchingIsForgiving(t *testing.T) {
	picks := []suggestPick{
		{Name: "  ho chi minh   city "},
		{Name: "REYKJAVIK"},
	}
	got := matchPicks(testCatalog(), picks, 5)

	if len(got.Matches) != 2 {
		t.Fatalf("matches = %d, want both resolved; dropped=%v", len(got.Matches), got.Dropped)
	}
	if got.Matches[0].Destination.Name != "Ho Chi Minh City" {
		t.Errorf("first match = %q", got.Matches[0].Destination.Name)
	}
}

// Observed against a local model: it qualifies every pick with its country
// despite being told to copy the catalog name exactly. Those are real matches
// and must not be dropped as inventions.
func TestMatchPicksResolvesCountryQualifiedNames(t *testing.T) {
	picks := []suggestPick{
		{Name: "Lisbon, Portugal"},
		{Name: "Ho Chi Minh City, Vietnam"},
	}
	got := matchPicks(testCatalog(), picks, 5)

	if len(got.Dropped) != 0 {
		t.Fatalf("dropped = %v, want country-qualified names resolved", got.Dropped)
	}
	if len(got.Matches) != 2 {
		t.Fatalf("matches = %d, want 2", len(got.Matches))
	}
	if got.Matches[0].Destination.Name != "Lisbon" {
		t.Errorf("first match = %q, want Lisbon", got.Matches[0].Destination.Name)
	}
}

// The forgiving retry must not let a different city be captured by a shorter
// name that happens to be its prefix.
func TestMatchPicksPrefixDoesNotCaptureDifferentCity(t *testing.T) {
	catalog := []models.Destination{
		{Name: "Porto", Country: "Portugal"},
		{Name: "Porto Alegre", Country: "Brazil"},
	}

	got := matchPicks(catalog, []suggestPick{{Name: "Porto Alegre, Brazil"}}, 5)
	if len(got.Matches) != 1 {
		t.Fatalf("matches = %d, dropped = %v", len(got.Matches), got.Dropped)
	}
	if got.Matches[0].Destination.Name != "Porto Alegre" {
		t.Errorf("matched %q, want Porto Alegre — the exact name must win", got.Matches[0].Destination.Name)
	}
}

func TestMatchPicksDeduplicatesAndRespectsLimit(t *testing.T) {
	picks := []suggestPick{
		{Name: "Lisbon"}, {Name: "lisbon"}, {Name: "Reykjavik"}, {Name: "Ho Chi Minh City"},
	}

	got := matchPicks(testCatalog(), picks, 2)
	if len(got.Matches) != 2 {
		t.Fatalf("matches = %d, want the limit honoured", len(got.Matches))
	}
	if got.Matches[0].Destination.Name == got.Matches[1].Destination.Name {
		t.Error("a repeated pick should not occupy two slots")
	}
}

func TestMatchPicksIgnoresBlankNames(t *testing.T) {
	got := matchPicks(testCatalog(), []suggestPick{{Name: "   "}, {Name: ""}}, 5)
	if len(got.Matches) != 0 {
		t.Errorf("matches = %+v, want none", got.Matches)
	}
	if len(got.Dropped) != 0 {
		t.Errorf("dropped = %v, want blanks ignored rather than reported", got.Dropped)
	}
}

// The catalog is what constrains the model, so it has to actually reach the
// prompt — with the fields needed to tell similar places apart.
func TestBuildSuggestPromptCarriesCatalogAndConstraints(t *testing.T) {
	p := buildSuggestPrompt(testCatalog(), []string{"surfing", "hiking"},
		ActivitySuggestRequest{Month: 9, MaxBudget: 150}, 3)

	for _, want := range []string{"Lisbon", "Portugal", "Europe", "Reykjavik", "surfing, hiking", "September"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt missing %q:\n%s", want, p)
		}
	}
	if !strings.Contains(p, "at most 3") {
		t.Errorf("prompt should carry the limit:\n%s", p)
	}
	if !strings.Contains(p, "$150") {
		t.Errorf("prompt should carry the budget ceiling:\n%s", p)
	}
}

func TestBuildSuggestPromptOmitsUnsetConstraints(t *testing.T) {
	p := buildSuggestPrompt(testCatalog(), []string{"surfing"}, ActivitySuggestRequest{}, 5)
	if strings.Contains(p, "Travelling in:") {
		t.Errorf("no month set, so none should appear:\n%s", p)
	}
	if strings.Contains(p, "Budget ceiling") {
		t.Errorf("no budget set, so none should appear:\n%s", p)
	}
}

func TestSuggestUnconfiguredFailsFast(t *testing.T) {
	s := NewDestinationSuggestService(NewAIItineraryService("", "", ""), nil)
	if _, err := s.Suggest(t.Context(), ActivitySuggestRequest{Activities: []string{"surf"}}); err != ErrAIItineraryNotConfigured {
		t.Errorf("err = %v, want ErrAIItineraryNotConfigured", err)
	}
}

func TestNormalizeName(t *testing.T) {
	cases := map[string]string{
		"  Ho Chi Minh   City ": "ho chi minh city",
		"LISBON":                "lisbon",
		"Reykjavik":             "reykjavik",
	}
	for in, want := range cases {
		if got := normalizeName(in); got != want {
			t.Errorf("normalizeName(%q) = %q, want %q", in, got, want)
		}
	}
	// Distinct cities must not collapse into one another.
	if normalizeName("Porto") == normalizeName("Porto Alegre") {
		t.Error("normalisation is too aggressive: distinct cities collided")
	}
}
