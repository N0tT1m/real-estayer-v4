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

// The catalog is a preference, not a whitelist. matchPicks resolves what it
// can locally and hands everything else to discovery rather than declaring it
// invented — the earlier behaviour, which dropped a correct "Hossegor" for
// surfing, is what made the finder look like it only knew the seeded cities.
func TestMatchPicksDefersOffCatalogNamesToDiscovery(t *testing.T) {
	picks := []suggestPick{
		{Name: "Lisbon", Why: "surf breaks nearby"},
		{Name: "Hossegor", Why: "world-class beach break"}, // real place, not in catalog
		{Name: "Atlantis", Why: "hallucinated"},
	}

	got, ungrounded := matchPicks(testCatalog(), picks, 5)

	if len(ungrounded) != 2 {
		t.Fatalf("ungrounded = %+v, want both off-catalog names deferred", ungrounded)
	}
	if ungrounded[0].pick.Name != "Hossegor" || ungrounded[1].pick.Name != "Atlantis" {
		t.Errorf("ungrounded names = %q, %q", ungrounded[0].pick.Name, ungrounded[1].pick.Name)
	}
	// Nothing is dropped here — only discovery can decide that.
	if len(got.Dropped) != 0 {
		t.Errorf("dropped = %v, want the verdict left to discovery", got.Dropped)
	}
	if got.Matches[0].Destination.Name != "Lisbon" {
		t.Errorf("first match = %q, want Lisbon", got.Matches[0].Destination.Name)
	}
	if got.CatalogSize != 3 {
		t.Errorf("catalog_size = %d, want 3", got.CatalogSize)
	}
}

// The deferred picks hold their place in the model's ranking. Discovery runs
// concurrently, so without reserved slots the results would come back ordered
// by whichever Wikidata lookup returned first.
func TestMatchPicksReservesSlotsInModelOrder(t *testing.T) {
	picks := []suggestPick{
		{Name: "Hossegor"},
		{Name: "Lisbon"},
		{Name: "Chamonix"},
	}

	got, ungrounded := matchPicks(testCatalog(), picks, 5)

	if len(got.Matches) != 3 {
		t.Fatalf("matches = %d, want a slot per pick", len(got.Matches))
	}
	if got.Matches[1].Destination.Name != "Lisbon" {
		t.Errorf("catalog match landed at the wrong index: %+v", got.Matches)
	}
	if len(ungrounded) != 2 || ungrounded[0].slot != 0 || ungrounded[1].slot != 2 {
		t.Errorf("slots = %+v, want the off-catalog picks to hold indexes 0 and 2", ungrounded)
	}
}

// Actionable fields must come from the database row, not from the model — the
// model only supplies prose. A pick carries no coordinates or airport code, so
// if those survive, they came from the catalog.
func TestMatchPicksTakesFactsFromCatalogNotModel(t *testing.T) {
	got, _ := matchPicks(testCatalog(), []suggestPick{{
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
	got, _ := matchPicks(testCatalog(), picks, 5)

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
	got, _ := matchPicks(testCatalog(), picks, 5)

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

	got, _ := matchPicks(catalog, []suggestPick{{Name: "Porto Alegre, Brazil"}}, 5)
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

	got, _ := matchPicks(testCatalog(), picks, 2)
	if len(got.Matches) != 2 {
		t.Fatalf("matches = %d, want the limit honoured", len(got.Matches))
	}
	if got.Matches[0].Destination.Name == got.Matches[1].Destination.Name {
		t.Error("a repeated pick should not occupy two slots")
	}
}

func TestMatchPicksIgnoresBlankNames(t *testing.T) {
	got, _ := matchPicks(testCatalog(), []suggestPick{{Name: "   "}, {Name: ""}}, 5)
	if len(got.Matches) != 0 {
		t.Errorf("matches = %+v, want none", got.Matches)
	}
	if len(got.Dropped) != 0 {
		t.Errorf("dropped = %v, want blanks ignored rather than reported", got.Dropped)
	}
}

// A queued place has no destinations row, so the UI must be told not to link
// it. Both flags travel together and the count only moves for queued places —
// a catalog hit reached through discovery is not a new find.
func TestActivityMatchFlagsDistinguishQueuedFromCatalog(t *testing.T) {
	queued := ActivityMatch{Discovered: true, AwaitingReview: true}
	if !queued.AwaitingReview {
		t.Error("a queued place must be marked as awaiting review")
	}

	established := ActivityMatch{Destination: models.Destination{Name: "Lisbon"}}
	if established.Discovered || established.AwaitingReview {
		t.Error("a catalog match must carry neither flag")
	}
}

// Without a discovery service the finder must still answer, using the catalog
// alone — the fail-soft rule every optional integration follows.
func TestResolveUngroundedWithoutDiscoveryDropsAndKeepsCatalogMatches(t *testing.T) {
	s := &DestinationSuggestService{}
	out, ungrounded := matchPicks(testCatalog(), []suggestPick{
		{Name: "Lisbon"}, {Name: "Hossegor"},
	}, 5)

	s.resolveUngrounded(t.Context(), out, ungrounded, ActivitySuggestRequest{}, 5)

	if len(out.Matches) != 1 || out.Matches[0].Destination.Name != "Lisbon" {
		t.Fatalf("matches = %+v, want the catalog match to survive", out.Matches)
	}
	if len(out.Dropped) != 1 || out.Dropped[0] != "Hossegor" {
		t.Errorf("dropped = %v, want the unresolvable name reported", out.Dropped)
	}
	if out.DiscoveredCount != 0 {
		t.Errorf("discovered_count = %d, want 0", out.DiscoveredCount)
	}
}

// Unfilled slots must collapse without disturbing the order of the ones that
// resolved.
func TestCompactMatchesPreservesOrder(t *testing.T) {
	out := &ActivitySuggestions{Matches: []ActivityMatch{
		{Destination: models.Destination{Name: "Chamonix"}},
		{}, // discovery failed here
		{Destination: models.Destination{Name: "Lisbon"}},
	}}

	compactMatches(out, 5)

	if len(out.Matches) != 2 {
		t.Fatalf("matches = %+v, want the empty slot removed", out.Matches)
	}
	if out.Matches[0].Destination.Name != "Chamonix" || out.Matches[1].Destination.Name != "Lisbon" {
		t.Errorf("order not preserved: %+v", out.Matches)
	}
}

func TestCompactMatchesHonoursLimit(t *testing.T) {
	out := &ActivitySuggestions{Matches: []ActivityMatch{
		{Destination: models.Destination{Name: "A"}},
		{Destination: models.Destination{Name: "B"}},
		{Destination: models.Destination{Name: "C"}},
	}}
	compactMatches(out, 2)
	if len(out.Matches) != 2 {
		t.Errorf("matches = %d, want the limit honoured", len(out.Matches))
	}
}

// A discovered row never went through the Mongo query that enforced the
// caller's constraints, so it has to be checked here or the budget ceiling
// becomes advisory.
func TestSuggestionSatisfiesEnforcesCallerConstraints(t *testing.T) {
	zermatt := models.Destination{Name: "Zermatt", Region: "Europe", AvgDailyBudget: 300}

	if suggestionSatisfies(zermatt, ActivitySuggestRequest{MaxBudget: 80}) {
		t.Error("a $300/day place must not survive an $80/day ceiling")
	}
	if !suggestionSatisfies(zermatt, ActivitySuggestRequest{MaxBudget: 300}) {
		t.Error("a place exactly at the ceiling should pass")
	}
	if suggestionSatisfies(zermatt, ActivitySuggestRequest{Region: "Asia"}) {
		t.Error("region mismatch must be rejected")
	}
	if !suggestionSatisfies(zermatt, ActivitySuggestRequest{Region: "europe"}) {
		t.Error("region match should be case-insensitive")
	}
	if !suggestionSatisfies(zermatt, ActivitySuggestRequest{}) {
		t.Error("an unconstrained request should accept anything")
	}
}

// The prompt is what lifts the catalog from a whitelist to a preference. If
// this wording regresses, the model goes back to answering only with seeded
// cities and the feature silently narrows again.
func TestSuggestSystemPromptAllowsOffCatalogPlaces(t *testing.T) {
	if !strings.Contains(suggestSystemPrompt, "not in it, name that place anyway") {
		t.Errorf("prompt must invite off-catalog answers:\n%s", suggestSystemPrompt)
	}
	if strings.Contains(suggestSystemPrompt, "Choose ONLY from the CATALOG") {
		t.Errorf("prompt still confines the model to the catalog:\n%s", suggestSystemPrompt)
	}
	if !strings.Contains(suggestSystemPrompt, "English Wikipedia article") {
		t.Errorf("prompt must ask for verifiable places:\n%s", suggestSystemPrompt)
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
	s := NewDestinationSuggestService(NewAIItineraryService("", "", ""), nil, nil)
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
