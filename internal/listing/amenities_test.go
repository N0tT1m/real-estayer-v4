package listing

import (
	"reflect"
	"sort"
	"testing"
)

func has(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

// The 23 brand-name toiletry strings must collapse to one facet, or the
// filter list is unusable.
func TestToiletryBrandsCollapse(t *testing.T) {
	raw := []string{
		"Kirkland Shampoo", "Panteen Conditioner", "Mrs. Meyer's Body Soap",
		"TraderJoe's Tea Tree Conditioner", "All-in-one Body Soap", "Shower Gel",
		"Aveeno Body Soap", "Luxury Salon Brands Used Shampoo",
		"EO Everyone Shampoo And Body Wash, Cedar Citrus Body Soap",
	}
	am, _ := Canonicalise(raw)
	if len(am) != 1 || am[0] != "Toiletries" {
		t.Errorf("amenities = %v, want exactly [Toiletries]", am)
	}
}

func TestClothingStorageVariantsAreDropped(t *testing.T) {
	raw := []string{
		"Clothing Storage", "Clothing Storage: Closet",
		"Clothing Storage: Walk-in Closet, Closet, Wardrobe, And Dresser",
	}
	am, _ := Canonicalise(raw)
	if len(am) != 0 {
		t.Errorf("amenities = %v, want none — wardrobe layout is not a filter", am)
	}
}

// These are the two bugs that shipped in the Rust normaliser: an earlier
// substring rule swallowing a later category.
func TestNoSubstringShadowing(t *testing.T) {
	am, _ := Canonicalise([]string{"Dishwasher", "Washer", "Hair Dryer", "Dryer"})
	for _, want := range []string{"Dishwasher", "Washer", "Hair Dryer", "Dryer"} {
		if !has(am, want) {
			t.Errorf("%q was shadowed by an earlier rule; got %v", want, am)
		}
	}
}

func TestHotTubBeatsPool(t *testing.T) {
	am, _ := Canonicalise([]string{"Plunge pool"})
	if !has(am, "Hot Tub") || has(am, "Pool") {
		t.Errorf("amenities = %v, want Hot Tub (a plunge pool is not a swimming pool)", am)
	}
}

// Badges are marketing labels; filtering a house by "Superhost" is a category
// error and clutters the facet list.
func TestBadgesAreSeparatedFromAmenities(t *testing.T) {
	am, badges := Canonicalise([]string{"Superhost", "Guest favorite", "Luxe", "WiFi", "Pay $0 today"})
	if has(am, "Superhost") || has(am, "Guest Favorite") || has(am, "Luxe") {
		t.Errorf("badges leaked into amenities: %v", am)
	}
	if !has(am, "WiFi") {
		t.Errorf("real amenity lost: %v", am)
	}
	sort.Strings(badges)
	want := []string{"Guest Favorite", "Luxe", "Pay $0 Today", "Superhost"}
	if !reflect.DeepEqual(badges, want) {
		t.Errorf("badges = %v, want %v", badges, want)
	}
}

// Room counts become structured Facts, so they must not also appear as facets.
func TestRoomCountsAreNotAmenities(t *testing.T) {
	am, _ := Canonicalise([]string{"2 bedrooms", "1 bed", "1 double bed", "WiFi"})
	if len(am) != 1 || am[0] != "WiFi" {
		t.Errorf("amenities = %v, want only [WiFi] — counts belong in Facts", am)
	}
}

func TestInventoryNoiseIsDropped(t *testing.T) {
	noise := []string{
		"Baking Sheet", "Body Soap", "Hangers", "Bed Linens", "Wine Glasses",
		"Extra Pillows And Blankets", "Dishes And Silverware", "Outlet Covers",
	}
	am, _ := Canonicalise(noise)
	// Body Soap is a toiletry, everything else is inventory.
	for _, a := range am {
		if a != "Toiletries" {
			t.Errorf("kept inventory noise as a facet: %q", a)
		}
	}
}

// "Single Level Home" is an accessibility signal, not inventory.
func TestSingleLevelIsAccessibility(t *testing.T) {
	am, _ := Canonicalise([]string{"Single Level Home"})
	if !has(am, "Accessible") {
		t.Errorf("amenities = %v, want Accessible", am)
	}
}

func TestViewVariantsCollapse(t *testing.T) {
	am, _ := Canonicalise([]string{"Mountain View", "River View", "City Skyline View", "Golf Course View"})
	if len(am) != 1 || am[0] != "Scenic View" {
		t.Errorf("amenities = %v, want exactly [Scenic View]", am)
	}
}

func TestSoundSystemVariantsCollapse(t *testing.T) {
	am, _ := Canonicalise([]string{"Sonos Bluetooth Sound System", "Control4 Bluetooth Sound System", "Sound System"})
	if len(am) != 1 || am[0] != "Sound System" {
		t.Errorf("amenities = %v, want exactly [Sound System]", am)
	}
}

// An amenity nobody anticipated should survive rather than vanish, so the
// facet list grows with reality instead of silently truncating it.
func TestUnknownButPlausibleAmenityIsKept(t *testing.T) {
	am, _ := Canonicalise([]string{"Helipad"})
	if !has(am, "Helipad") {
		t.Errorf("amenities = %v, want the unrecognised amenity preserved", am)
	}
}

// Output must be stable: an unstable order produces a spurious diff on every
// re-scrape and makes change detection useless.
func TestOutputIsSortedAndDeduplicated(t *testing.T) {
	am, _ := Canonicalise([]string{"WiFi", "Pool", "WiFi", "wifi", "Free wifi", "Pool"})
	if !sort.StringsAreSorted(am) {
		t.Errorf("amenities not sorted: %v", am)
	}
	seen := map[string]bool{}
	for _, a := range am {
		if seen[a] {
			t.Errorf("duplicate facet %q in %v", a, am)
		}
		seen[a] = true
	}
}

func TestCanonicaliseEmptyInput(t *testing.T) {
	am, badges := Canonicalise(nil)
	if len(am) != 0 || len(badges) != 0 {
		t.Errorf("empty input produced %v / %v", am, badges)
	}
	am, _ = Canonicalise([]string{"", "   "})
	if len(am) != 0 {
		t.Errorf("blank entries produced %v", am)
	}
}
