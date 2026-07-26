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

		// Global coverage: the scraper targets every region, not just North
		// America, so any recognised country must resolve.
		{"Paris, France", "Paris", "", "France"},
		{"Barcelona, Spain", "Barcelona", "", "Spain"},
		{"Kyoto, Japan", "Kyoto", "", "Japan"},
		{"Cartagena, Colombia", "Cartagena", "", "Colombia"},
		{"Cape Town, South Africa", "Cape Town", "", "South Africa"},
		{"Amsterdam, Holland", "Amsterdam", "", "Netherlands"},
		{"Istanbul, Türkiye", "Istanbul", "", "Turkey"},
		{"Edinburgh, Scotland", "Edinburgh", "", "United Kingdom"},

		// Subdivisions outside US/CA are kept as written: we cannot enumerate
		// every country's regions, and Airbnb's own string beats discarding it.
		{"Barcelona, Catalonia, Spain", "Barcelona", "Catalonia", "Spain"},
		{"Kyoto, Kansai, Japan", "Kyoto", "Kansai", "Japan"},

		// Genuinely unknown trailing segment still records nothing.
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
