package listing

import "testing"

// Room counts arrive two ways: as prose in the description ("this 2-bedroom,
// 2-bath condo") and as stray entries in the scraped amenity list
// ("2 bedrooms"). Both have to be recovered, and the structured entries are
// the more trustworthy of the two.

func TestFactsFromTextProse(t *testing.T) {
	f := FactsFromText("Three minutes from downtown, this 2-bedroom, 2-bath condo sleeps 6.")
	if f.Bedrooms != 2 {
		t.Errorf("Bedrooms = %d, want 2", f.Bedrooms)
	}
	if f.Bathrooms != 2 {
		t.Errorf("Bathrooms = %v, want 2", f.Bathrooms)
	}
	if f.Sleeps != 6 {
		t.Errorf("Sleeps = %d, want 6", f.Sleeps)
	}
}

func TestFactsFromTextHalfBaths(t *testing.T) {
	if got := FactsFromText("3 bedrooms, 2.5 baths").Bathrooms; got != 2.5 {
		t.Errorf("Bathrooms = %v, want 2.5", got)
	}
	if got := FactsFromText("One bedroom with a half bath").Bathrooms; got != 0.5 {
		t.Errorf("half bath = %v, want 0.5", got)
	}
}

func TestFactsFromTextGuestPhrasings(t *testing.T) {
	for _, s := range []string{"sleeps 8", "accommodates 8", "up to 8 guests", "8 guests"} {
		if got := FactsFromText(s).Sleeps; got != 8 {
			t.Errorf("%q -> Sleeps %d, want 8", s, got)
		}
	}
}

func TestFactsFromTextIgnoresAbsurdValues(t *testing.T) {
	// Guards against picking a year or a price out of the prose.
	f := FactsFromText("built in 1920 bedrooms were different then")
	if f.Bedrooms != 0 {
		t.Errorf("Bedrooms = %d, want 0 for an implausible count", f.Bedrooms)
	}
}

func TestFactsFromTextEmpty(t *testing.T) {
	if !FactsFromText("").Empty() {
		t.Error("empty text should yield no facts")
	}
	if !FactsFromText("a lovely place by the sea").Empty() {
		t.Error("prose with no counts should yield no facts")
	}
}

// A bare "1 bed" amenity entry must not be misread as "sleeps 1".
func TestFactsFromFeaturesDoesNotInferSleeps(t *testing.T) {
	f := FactsFromFeatures([]string{"1 bed", "2 bedrooms", "WiFi"})
	if f.Beds != 1 {
		t.Errorf("Beds = %d, want 1", f.Beds)
	}
	if f.Bedrooms != 2 {
		t.Errorf("Bedrooms = %d, want 2", f.Bedrooms)
	}
	if f.Sleeps != 0 {
		t.Errorf("Sleeps = %d; a bed count is not a guest count", f.Sleeps)
	}
}

func TestFactsFromFeaturesHandlesQualifiedBeds(t *testing.T) {
	if got := FactsFromFeatures([]string{"1 double bed"}).Beds; got != 1 {
		t.Errorf("Beds = %d, want 1", got)
	}
}

// The structured amenity entries win over prose when they disagree.
func TestExtractFactsPrefersStructuredEntries(t *testing.T) {
	f := ExtractFacts(
		[]string{"3 bedrooms"},
		"a cosy 1-bedroom apartment", // stale or wrong prose
		"",
	)
	if f.Bedrooms != 3 {
		t.Errorf("Bedrooms = %d, want the structured value 3", f.Bedrooms)
	}
}

func TestExtractFactsFillsGapsFromProse(t *testing.T) {
	f := ExtractFacts([]string{"2 bedrooms"}, "2-bath condo that sleeps 5", "")
	if f.Bedrooms != 2 || f.Bathrooms != 2 || f.Sleeps != 5 {
		t.Errorf("facts = %+v, want prose to fill what the features lacked", f)
	}
}

func TestFactsCompleteAndMerge(t *testing.T) {
	partial := Facts{Bedrooms: 2}
	if partial.Complete() {
		t.Error("partial facts should not report Complete")
	}
	if partial.Empty() {
		t.Error("partial facts are not Empty")
	}
	full := partial.merge(Facts{Bedrooms: 9, Bathrooms: 1, Beds: 3, Sleeps: 4})
	if full.Bedrooms != 2 {
		t.Errorf("merge overwrote an existing value: %d", full.Bedrooms)
	}
	if !full.Complete() {
		t.Errorf("merge should have filled the gaps: %+v", full)
	}
}
