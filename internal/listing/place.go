package listing

import (
	_ "embed"
	"strings"
)

// Scraped listings arrive with a free-text `location` ("Indianapolis, Indiana",
// "Toronto, Canada", "East Austin") but frequently without `region` or
// `country`. The scraper only fills those from the listing page's JSON-LD or
// og:title, both of which are Airbnb markup that often isn't present in the
// enrichment fetch — so 280 of 366 listings had neither field, and the
// /listings "State" filter offered exactly one state because it could only see
// the older documents that did.
//
// ParsePlace recovers what it can from the location string alone. No network,
// no markup dependency: whatever the scraper managed to record for display is
// enough to derive the filter facets from.

// usStates maps a lowercased state name or postal abbreviation to its
// canonical name. Abbreviations are included because Airbnb writes both
// ("Ocean City, Maryland" and "Austin, TX" both occur).
var usStates = map[string]string{
	"alabama": "Alabama", "al": "Alabama",
	"alaska": "Alaska", "ak": "Alaska",
	"arizona": "Arizona", "az": "Arizona",
	"arkansas": "Arkansas", "ar": "Arkansas",
	"california": "California", "ca": "California",
	"colorado": "Colorado", "co": "Colorado",
	"connecticut": "Connecticut", "ct": "Connecticut",
	"delaware": "Delaware", "de": "Delaware",
	"florida": "Florida", "fl": "Florida",
	"georgia": "Georgia", "ga": "Georgia",
	"hawaii": "Hawaii", "hi": "Hawaii",
	"idaho": "Idaho", "id": "Idaho",
	"illinois": "Illinois", "il": "Illinois",
	"indiana": "Indiana", "in": "Indiana",
	"iowa": "Iowa", "ia": "Iowa",
	"kansas": "Kansas", "ks": "Kansas",
	"kentucky": "Kentucky", "ky": "Kentucky",
	"louisiana": "Louisiana", "la": "Louisiana",
	"maine": "Maine", "me": "Maine",
	"maryland": "Maryland", "md": "Maryland",
	"massachusetts": "Massachusetts", "ma": "Massachusetts",
	"michigan": "Michigan", "mi": "Michigan",
	"minnesota": "Minnesota", "mn": "Minnesota",
	"mississippi": "Mississippi", "ms": "Mississippi",
	"missouri": "Missouri", "mo": "Missouri",
	"montana": "Montana", "mt": "Montana",
	"nebraska": "Nebraska", "ne": "Nebraska",
	"nevada": "Nevada", "nv": "Nevada",
	"new hampshire": "New Hampshire", "nh": "New Hampshire",
	"new jersey": "New Jersey", "nj": "New Jersey",
	"new mexico": "New Mexico", "nm": "New Mexico",
	"new york": "New York", "ny": "New York",
	"north carolina": "North Carolina", "nc": "North Carolina",
	"north dakota": "North Dakota", "nd": "North Dakota",
	"ohio": "Ohio", "oh": "Ohio",
	"oklahoma": "Oklahoma", "ok": "Oklahoma",
	"oregon": "Oregon", "or": "Oregon",
	"pennsylvania": "Pennsylvania", "pa": "Pennsylvania",
	"rhode island": "Rhode Island", "ri": "Rhode Island",
	"south carolina": "South Carolina", "sc": "South Carolina",
	"south dakota": "South Dakota", "sd": "South Dakota",
	"tennessee": "Tennessee", "tn": "Tennessee",
	"texas": "Texas", "tx": "Texas",
	"utah": "Utah", "ut": "Utah",
	"vermont": "Vermont", "vt": "Vermont",
	"virginia": "Virginia", "va": "Virginia",
	"washington": "Washington", "wa": "Washington",
	"west virginia": "West Virginia", "wv": "West Virginia",
	"wisconsin": "Wisconsin", "wi": "Wisconsin",
	"wyoming": "Wyoming", "wy": "Wyoming",
	"district of columbia": "District of Columbia", "dc": "District of Columbia",
	"puerto rico": "Puerto Rico", "pr": "Puerto Rico",
}

// caProvinces is the Canadian equivalent. Kept separate from usStates so a
// match also tells us which country to record.
var caProvinces = map[string]string{
	"alberta": "Alberta", "ab": "Alberta",
	"british columbia": "British Columbia", "bc": "British Columbia",
	"manitoba": "Manitoba", "mb": "Manitoba",
	"new brunswick": "New Brunswick", "nb": "New Brunswick",
	"newfoundland and labrador": "Newfoundland and Labrador", "nl": "Newfoundland and Labrador",
	"nova scotia": "Nova Scotia", "ns": "Nova Scotia",
	"ontario": "Ontario", "on": "Ontario",
	"prince edward island": "Prince Edward Island", "pe": "Prince Edward Island",
	"quebec": "Quebec", "québec": "Quebec", "qc": "Quebec",
	"saskatchewan": "Saskatchewan", "sk": "Saskatchewan",
	"yukon": "Yukon", "yt": "Yukon",
	"northwest territories": "Northwest Territories", "nt": "Northwest Territories",
	"nunavut": "Nunavut", "nu": "Nunavut",
}

//go:embed countries.txt
var countriesData string

// countryAliases maps every accepted spelling (lowercased) to its canonical
// country name, loaded from the shared table so the Rust scraper and this
// package cannot disagree about what "Türkiye" or "Holland" normalise to.
var countryAliases = func() map[string]string {
	m := make(map[string]string, 512)
	for _, line := range strings.Split(countriesData, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		canon, aliases, _ := strings.Cut(line, "|")
		canon = strings.TrimSpace(canon)
		if canon == "" {
			continue
		}
		m[strings.ToLower(canon)] = canon
		for _, a := range strings.Split(aliases, ",") {
			if a = strings.TrimSpace(a); a != "" {
				m[strings.ToLower(a)] = canon
			}
		}
	}
	return m
}()

// ParsePlace derives city, region and country from a location string.
//
// Any component it cannot determine comes back empty — callers must not treat
// "" as a value. A bare neighbourhood ("East Austin", "Zilker") legitimately
// yields nothing but a city, because the string genuinely does not say which
// state it is in, and guessing would put listings under the wrong filter.
func ParsePlace(location string) (city, region, country string) {
	parts := splitTrim(location)
	if len(parts) == 0 {
		return "", "", ""
	}
	city = parts[0]
	if len(parts) == 1 {
		// Neighbourhood or bare city; nothing further is knowable.
		return city, "", ""
	}

	// Work from the last segment inward: Airbnb orders these coarsest-last
	// ("Ocean City, Maryland", "Toronto, Ontario, Canada").
	last := strings.ToLower(parts[len(parts)-1])

	if c, ok := countryAliases[last]; ok {
		country = c
		// "Toronto, Ontario, Canada" / "Barcelona, Catalonia, Spain" — the
		// middle segment is the subdivision. Canonicalise it where we have a
		// table (US/CA, where Airbnb also abbreviates), otherwise keep it
		// as written: we cannot enumerate every country's subdivisions, and
		// the string Airbnb chose is better than discarding it.
		if len(parts) >= 3 {
			raw := parts[len(parts)-2]
			mid := strings.ToLower(raw)
			switch {
			case country == "United States":
				region = usStates[mid]
			case country == "Canada":
				region = caProvinces[mid]
			default:
				region = raw
			}
		}
		return city, region, country
	}

	if r, ok := usStates[last]; ok {
		return city, r, "United States"
	}
	if r, ok := caProvinces[last]; ok {
		return city, r, "Canada"
	}

	// Trailing segment is neither a known country nor a US/CA subdivision.
	// Leave country unset rather than recording the raw string: the filters
	// match against a fixed variant list, and a stray value would create a
	// dead entry in the dropdown.
	return city, "", ""
}

// NormalizeCountry maps a scraped country spelling to its canonical form,
// returning "" when unrecognised. Existing documents carry "USA"; new ones
// should carry "United States" so the filter's variant list stops being
// load-bearing.
func NormalizeCountry(raw string) string {
	return countryAliases[strings.ToLower(strings.TrimSpace(raw))]
}

// NormalizeRegion title-cases a scraped region to its canonical spelling.
// Legacy documents hold "michigan"; the dropdown should show "Michigan".
func NormalizeRegion(raw string) string {
	k := strings.ToLower(strings.TrimSpace(raw))
	if r, ok := usStates[k]; ok {
		return r
	}
	if r, ok := caProvinces[k]; ok {
		return r
	}
	return ""
}

func splitTrim(s string) []string {
	out := make([]string, 0, 3)
	for _, p := range strings.Split(s, ",") {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
