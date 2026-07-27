package service

import (
	"reflect"
	"testing"
)

func TestTrimISODate(t *testing.T) {
	cases := map[string]string{
		"1147-01-01T00:00:00Z": "1147-01-01",
		"2020-05-01T12:34:56":  "2020-05-01",
		"2020-05-01":           "2020-05-01",
		"":                     "",
	}
	for in, want := range cases {
		if got := trimISODate(in); got != want {
			t.Errorf("trimISODate(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestSplitPipeDedupesAndLimits(t *testing.T) {
	got := splitPipe("Alice|Bob|alice|  |Charlie|Dave|Eve|Bob", 3)
	want := []string{"Alice", "Bob", "alice"}
	// Dedup is case-sensitive ("alice" != "Alice") — verify that contract.
	if !reflect.DeepEqual(got, want) {
		t.Errorf("splitPipe = %v; want %v", got, want)
	}
}

func TestSplitPipeEmpty(t *testing.T) {
	if splitPipe("", 5) != nil {
		t.Errorf("empty input should return nil")
	}
	// Whitespace/empty segments yield an empty (not necessarily nil) slice —
	// both are fine for the UI which only looks at len().
	if got := splitPipe("||  ||", 5); len(got) != 0 {
		t.Errorf("whitespace/empty segments → zero-length; got %v", got)
	}
}

// row builds a SPARQL result row for the place-disambiguation tests.
func row(qid, label, countryCode, coord, sitelinks, article string) sparqlRow {
	r := sparqlRow{
		"place":     {Value: "http://www.wikidata.org/entity/" + qid},
		"sitelinks": {Value: sitelinks},
	}
	if label != "" {
		r["placeLabel"] = sparqlValue{Value: label}
	}
	if countryCode != "" {
		r["countryCode"] = sparqlValue{Value: countryCode}
	}
	if coord != "" {
		r["coord"] = sparqlValue{Value: coord}
	}
	if article != "" {
		r["article"] = sparqlValue{Value: "https://en.wikipedia.org/wiki/" + article}
	}
	return r
}

// The bug this exists to prevent: "Banff" for a skiing request resolved to the
// Aberdeenshire fishing town, and got stored as a ski destination in the United
// Kingdom. Both are real places with the same name, so only the popularity
// signal separates them.
func TestPickBestPlacePrefersTheFamousNamesake(t *testing.T) {
	rows := []sparqlRow{
		row("Q793036", "Banff", "GB", "Point(-2.52 57.66)", "12", "Banff,_Aberdeenshire"),
		row("Q795391", "Banff", "CA", "Point(-115.57 51.17)", "68", "Banff,_Alberta"),
	}

	got := pickBestPlace("Banff", rows)

	if got == nil {
		t.Fatal("got nil, want the Alberta town")
	}
	if got.CountryCode != "CA" {
		t.Errorf("resolved to %s (%s), want the Canadian Banff", got.CountryCode, got.QID)
	}
	if got.Latitude < 50 {
		t.Errorf("latitude = %v, want the Alberta coordinates", got.Latitude)
	}
}

// Rank order in the result set must not decide the outcome — the previous
// resolver took the first plausible hit, which is exactly how it went wrong.
func TestPickBestPlaceIgnoresRowOrder(t *testing.T) {
	obscure := row("Q1", "Springfield", "US", "Point(-89.65 39.79)", "9", "Springfield,_Illinois")
	famous := row("Q2", "Springfield", "US", "Point(-93.29 37.20)", "44", "Springfield,_Missouri")

	first := pickBestPlace("Springfield", []sparqlRow{obscure, famous})
	second := pickBestPlace("Springfield", []sparqlRow{famous, obscure})

	if first == nil || second == nil {
		t.Fatal("both orderings should resolve")
	}
	if first.QID != second.QID {
		t.Errorf("order changed the answer: %s vs %s", first.QID, second.QID)
	}
	if first.QID != "Q2" {
		t.Errorf("resolved to %s, want the better-documented Q2", first.QID)
	}
}

// A name shared with a person is the other half of the Churchill problem: the
// search endpoint returns Winston Churchill for "Churchill". Entities without
// coordinates are not places, and requiring them removes that whole class.
func TestPickBestPlaceRejectsNonPlaces(t *testing.T) {
	rows := []sparqlRow{
		// A person: hugely documented, but no coordinates.
		row("Q8016", "Winston Churchill", "GB", "", "230", "Winston_Churchill"),
		row("Q1075988", "Churchill", "CA", "Point(-94.16 58.76)", "31", "Churchill,_Manitoba"),
	}

	got := pickBestPlace("Churchill", rows)

	if got == nil {
		t.Fatal("got nil, want the Manitoba town")
	}
	if got.QID != "Q1075988" {
		t.Errorf("resolved to %s, want the town rather than the person", got.QID)
	}
}

// Description and hero image are read from Wikipedia downstream, so a place
// without an article renders as a blank card. Better to report it unverified.
func TestPickBestPlaceRequiresAnEnglishArticle(t *testing.T) {
	rows := []sparqlRow{
		row("Q1", "Nowhere", "US", "Point(1 1)", "400", ""),
	}
	if got := pickBestPlace("Nowhere", rows); got != nil {
		t.Errorf("got %+v, want nil when there is no article to enrich from", got)
	}
}

func TestPickBestPlaceReturnsNilWhenNothingQualifies(t *testing.T) {
	if got := pickBestPlace("Atlantis", nil); got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}

// Wikidata's canonical label is what gets stored, so /explore/<name> links
// resolve to the same row on the way back.
func TestPickBestPlaceAdoptsCanonicalLabel(t *testing.T) {
	rows := []sparqlRow{
		row("Q1234", "Banff National Park", "CA", "Point(-115.55 51.17)", "60", "Banff_National_Park"),
	}

	got := pickBestPlace("banff national park", rows)

	if got == nil {
		t.Fatal("got nil")
	}
	if got.Name != "Banff National Park" {
		t.Errorf("name = %q, want the canonical Wikidata label", got.Name)
	}
	if got.ArticleTitle != "Banff National Park" {
		t.Errorf("article title = %q", got.ArticleTitle)
	}
}

// Without an English label Wikidata echoes the QID; storing "Q12345" as a
// destination name would be worse than keeping what the caller asked for.
func TestPickBestPlaceKeepsCallerNameWhenLabelIsAQID(t *testing.T) {
	rows := []sparqlRow{
		row("Q999", "Q999", "IS", "Point(-21.9 64.1)", "7", "Somewhere"),
	}

	got := pickBestPlace("Somewhere", rows)

	if got == nil {
		t.Fatal("got nil")
	}
	if got.Name != "Somewhere" {
		t.Errorf("name = %q, want the caller's name rather than the QID", got.Name)
	}
}
