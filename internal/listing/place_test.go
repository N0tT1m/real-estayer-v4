package listing

import "testing"

func TestParsePlace(t *testing.T) {
	cases := []struct {
		in                    string
		city, region, country string
	}{
		// The shapes actually present in the 280 unfilled documents.
		{"Indianapolis, Indiana", "Indianapolis", "Indiana", "United States"},
		{"Ocean City, Maryland", "Ocean City", "Maryland", "United States"},
		{"Ann Arbor, Michigan", "Ann Arbor", "Michigan", "United States"},
		{"Toronto, Canada", "Toronto", "", "Canada"},
		{"Toronto, Ontario, Canada", "Toronto", "Ontario", "Canada"},
		{"Austin, TX", "Austin", "Texas", "United States"},
		{"Vancouver, BC", "Vancouver", "British Columbia", "Canada"},
		// "CA" is California, not Canada — countries are matched before
		// states, so an alias here would misfile every Californian listing.
		{"San Diego, CA", "San Diego", "California", "United States"},
		{"Los Angeles, California", "Los Angeles", "California", "United States"},

		// Bare neighbourhoods. These must NOT be assigned a state: guessing
		// would file listings under a filter they don't belong to, which is
		// worse than leaving the facet empty.
		{"East Austin", "East Austin", "", ""},
		{"Zilker", "Zilker", "", ""},
		{"Travis Heights", "Travis Heights", "", ""},

		// Unknown trailing segment: keep the city, record nothing we'd have
		// to invent.
		{"Paris, France", "Paris", "", ""},
		{"Somewhere, Atlantis", "Somewhere", "", ""},

		{"", "", "", ""},
		{"   ", "", "", ""},
		{" Denver , Colorado ", "Denver", "Colorado", "United States"},
	}
	for _, tc := range cases {
		city, region, country := ParsePlace(tc.in)
		if city != tc.city || region != tc.region || country != tc.country {
			t.Errorf("ParsePlace(%q) = (%q,%q,%q), want (%q,%q,%q)",
				tc.in, city, region, country, tc.city, tc.region, tc.country)
		}
	}
}

// Legacy documents hold country "USA" and region "michigan"; the filter's
// variant list exists only to paper over that. Normalising lets new writes be
// canonical without breaking the old ones.
func TestNormalizers(t *testing.T) {
	for in, want := range map[string]string{
		"USA": "United States", "usa": "United States",
		"United States": "United States", "Canada": "Canada",
		"": "", "Freedonia": "",
	} {
		if got := NormalizeCountry(in); got != want {
			t.Errorf("NormalizeCountry(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{
		"michigan": "Michigan", "TX": "Texas", "ontario": "Ontario",
		"": "", "nowhere": "",
	} {
		if got := NormalizeRegion(in); got != want {
			t.Errorf("NormalizeRegion(%q) = %q, want %q", in, got, want)
		}
	}
}
