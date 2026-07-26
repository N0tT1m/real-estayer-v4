package listing

import (
	"sort"
	"strings"
)

// Airbnb's amenity list is not a controlled vocabulary. A single scrape of 104
// listings produced 142 distinct strings, including 23 ways to say "there is
// shampoo" (`Kirkland Shampoo`, `Panteen Conditioner`, `TraderJoe's Tea Tree
// Conditioner`, ...), nine spellings of `Clothing Storage`, and booking badges
// like `Superhost` filed as if they were amenities.
//
// Canonicalise reduces that to a small, stable facet set worth exposing as
// filters. The raw list is deliberately preserved on the listing — see
// models.Listing.Features — because the previous normalisation rewrote
// features in place and destroyed the source strings, which is how a
// mislabelling bug became unrecoverable without a re-scrape.

// Badge is a marketing or booking label, not a property of the place.
// Keeping these out of the amenity facets stops "Superhost" appearing as
// something you can filter a house by.
var badgeSet = map[string]string{
	"superhost":              "Superhost",
	"guest favorite":         "Guest Favorite",
	"luxe":                   "Luxe",
	"free cancellation":      "Free Cancellation",
	"extended stay discount": "Extended Stay Discount",
	"pay $0 today":           "Pay $0 Today",
	"rare find":              "Rare Find",
	"top rated":              "Top Rated",
	"highly rated":           "Highly Rated",
}

// noiseSubstrings mark entries that describe inventory rather than a feature
// anyone would search on. A baking sheet is not a reason to book a house.
var noiseSubstrings = []string{
	"baking sheet", "dishes and silverware", "wine glasses", "hangers",
	"bed linens", "extra pillows", "cleaning products", "essentials",
	"drying rack", "clothing storage", "room-darkening", "hot water",
	"outlet covers", "table corner guards", "window guards", "portable fans",
	"laundromat nearby", "luggage dropoff",
	"babysitter recommendations", "books and reading material",
	"cleaning available during stay", "noise decibel monitors",
	"beach essentials", "breakfast bar", "dining table",
}

// canonicalRules maps a substring to the facet it implies. Order matters:
// the first match wins, so narrower rules must precede broader ones (a
// "plunge pool" is a hot tub, not a pool; a "hair dryer" is not a clothes
// dryer). This mirrors the Rust scraper's normaliser, which had exactly those
// two shadowing bugs.
var canonicalRules = []struct {
	needles []string
	exclude []string
	facet   string
}{
	// Toiletries first: they carry brand names that would otherwise trip
	// other rules, and there are 23 variants of them.
	{needles: []string{"shampoo", "conditioner", "body soap", "body wash", "shower gel", "soap"}, facet: "Toiletries"},

	{needles: []string{"hot tub", "hottub", "jacuzzi", "whirlpool", "jetted tub", "plunge pool", "sauna"}, facet: "Hot Tub"},
	{needles: []string{"pool", "swimming"}, facet: "Pool"},
	{needles: []string{"waterfront", "lakefront", "beachfront", "oceanfront", "riverfront"}, facet: "Waterfront"},
	{needles: []string{"beach access", "private beach", "ski-in", "ski-out"}, facet: "Beach & Slope Access"},
	{needles: []string{" view", "skyline"}, facet: "Scenic View"},

	{needles: []string{"air conditioning", "aircon", "window ac", "ac -", "split type", "ceiling fan"}, facet: "Air Conditioning"},
	{needles: []string{"heating", "heater", "fireplace", "fire pit"}, facet: "Heating"},

	{needles: []string{"wifi", "wi-fi", "ethernet", "internet"}, facet: "WiFi"},
	{needles: []string{"workspace", "desk", "office"}, facet: "Workspace"},

	{needles: []string{"dishwasher"}, facet: "Dishwasher"},
	{needles: []string{"washer", "washing machine", "laundry"}, exclude: []string{"dishwasher"}, facet: "Washer"},
	{needles: []string{"hair dryer", "hairdryer", "blow dryer"}, facet: "Hair Dryer"},
	{needles: []string{"dryer"}, exclude: []string{"hair dryer", "hairdryer", "blow dryer"}, facet: "Dryer"},

	{needles: []string{"kitchen", "stove", "oven", "microwave", "refrigerator", "freezer", "toaster",
		"blender", "bread maker", "rice maker", "kettle", "coffee", "breakfast"}, facet: "Kitchen"},
	{needles: []string{"bbq", "grill", "barbecue", "smoker"}, facet: "BBQ Grill"},

	{needles: []string{"parking", "garage", "carport", "driveway"}, facet: "Parking"},
	{needles: []string{"ev charger", "ev charging", "charging station", "tesla"}, facet: "EV Charger"},

	{needles: []string{"crib", "high chair", "children", "children’s", "baby", "changing table", "playroom", "playground"}, facet: "Family Friendly"},
	{needles: []string{"pets allowed", "pet friendly", "dogs allowed", "cats allowed"}, facet: "Pets Allowed"},
	{needles: []string{"smoking allowed"}, facet: "Smoking Allowed"},

	{needles: []string{"tv", "television", "netflix", "projector", "home theater"}, facet: "TV"},
	{needles: []string{"sound system", "record player", "piano", "bluetooth"}, facet: "Sound System"},
	{needles: []string{"game console", "game room", "board games", "bowling", "mini golf", "life size games", "theme room", "arcade"}, facet: "Games"},

	{needles: []string{"gym", "fitness", "exercise", "treadmill", "weights"}, facet: "Gym"},
	{needles: []string{"bikes", "kayak", "canoe", "hammock", "sun lounger"}, facet: "Outdoor Gear"},
	{needles: []string{"patio", "balcony", "deck", "outdoor space", "outdoor furniture", "outdoor dining",
		"outdoor shower", "yard", "garden", "terrace"}, facet: "Outdoor Space"},

	{needles: []string{"elevator", "lift", "wheelchair", "accessible", "step-free", "single level"}, facet: "Accessible"},
	{needles: []string{"self check-in", "keyless", "smart lock", "lockbox", "keypad", "private entrance"}, facet: "Self Check-in"},
	{needles: []string{"security", "alarm", "fire extinguisher", "first aid", "safe", "doorman", "gated"}, facet: "Safety"},
	{needles: []string{"bathtub"}, facet: "Bathtub"},
	{needles: []string{"resort access", "free resort"}, facet: "Resort Access"},
	{needles: []string{"long term", "monthly"}, facet: "Long Term Stays"},
	{needles: []string{"iron"}, exclude: []string{"ironing board"}, facet: "Iron"},
}

// Canonicalise splits a raw scraped feature list into filterable amenity
// facets and booking badges, discarding inventory noise and the room-count
// entries that ExtractFacts already turned into structured fields.
//
// Both results are sorted and de-duplicated so the output is stable — an
// unstable order would produce spurious diffs on every re-scrape.
func Canonicalise(features []string) (amenities, badges []string) {
	amenitySet := map[string]struct{}{}
	badgeSet2 := map[string]struct{}{}

	for _, raw := range features {
		s := strings.ToLower(strings.TrimSpace(raw))
		if s == "" {
			continue
		}
		if b, ok := badgeSet[s]; ok {
			badgeSet2[b] = struct{}{}
			continue
		}
		// Room counts are structured elsewhere; drop them from the facets.
		if isRoomCount(s) {
			continue
		}
		if facet, ok := matchFacet(s); ok {
			amenitySet[facet] = struct{}{}
			continue
		}
		// Unmatched and noisy: drop. Unmatched and plausible: keep the
		// original so a genuinely new amenity is not silently lost.
		if isNoise(s) {
			continue
		}
		amenitySet[titleCase(strings.TrimSpace(raw))] = struct{}{}
	}

	return sortedKeys(amenitySet), sortedKeys(badgeSet2)
}

// matchFacet applies canonicalRules in order, honouring per-rule exclusions.
func matchFacet(lower string) (string, bool) {
	for _, rule := range canonicalRules {
		excluded := false
		for _, ex := range rule.exclude {
			if strings.Contains(lower, ex) {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		for _, n := range rule.needles {
			if strings.Contains(lower, n) {
				return rule.facet, true
			}
		}
	}
	return "", false
}

func isNoise(lower string) bool {
	for _, n := range noiseSubstrings {
		if strings.Contains(lower, n) {
			return true
		}
	}
	return false
}

// isRoomCount matches entries like "2 bedrooms", "1 bed", "1 double bed".
func isRoomCount(lower string) bool {
	f := FactsFromText(lower)
	if f.Bedrooms > 0 || f.Beds > 0 || f.Bathrooms > 0 {
		// Only treat it as a pure count if that is essentially all it says.
		return len(strings.Fields(lower)) <= 4
	}
	return false
}

func sortedKeys(m map[string]struct{}) []string {
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
