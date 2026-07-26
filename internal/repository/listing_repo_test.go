package repository

import (
	"reflect"
	"testing"

	"github.com/realestayer/v4/internal/models"
	"go.mongodb.org/mongo-driver/bson"
)

func TestNormalizeListingPaging(t *testing.T) {
	tests := []struct {
		name          string
		page, limit   int
		wantPage      int
		wantLimit     int
		wantUnlimited bool
		wantSkip      int
	}{
		{"defaults", 0, 0, 1, 20, true, 0},
		{"negative page clamps to first", -5, 10, 1, 10, false, 0},
		{"second page skips one page", 2, 10, 2, 10, false, 10},
		{"tenth page", 10, 25, 10, 25, false, 225},
		{"limit -1 means unlimited", 1, -1, 1, 20, true, 0},
		{"explicit limit is kept", 3, 50, 3, 50, false, 100},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := models.ListingSearchParams{Page: tc.page, Limit: tc.limit}
			unlimited, skip := normalizeListingPaging(&p)
			if p.Page != tc.wantPage {
				t.Errorf("Page = %d, want %d", p.Page, tc.wantPage)
			}
			if p.Limit != tc.wantLimit {
				t.Errorf("Limit = %d, want %d", p.Limit, tc.wantLimit)
			}
			if unlimited != tc.wantUnlimited {
				t.Errorf("unlimited = %v, want %v", unlimited, tc.wantUnlimited)
			}
			if skip != tc.wantSkip {
				t.Errorf("skip = %d, want %d", skip, tc.wantSkip)
			}
		})
	}
}

func TestBuildListingFilterEmptyParamsMatchesEverything(t *testing.T) {
	got := buildListingFilter(models.ListingSearchParams{})
	if len(got) != 0 {
		t.Errorf("empty params produced filter %v, want an empty document", got)
	}
}

func TestBuildListingFilterTextSearchSpansThreeFields(t *testing.T) {
	got := buildListingFilter(models.ListingSearchParams{Query: "cabin"})
	or, ok := got["$or"].([]bson.M)
	if !ok {
		t.Fatalf("$or = %T, want []bson.M", got["$or"])
	}
	if len(or) != 3 {
		t.Fatalf("$or has %d clauses, want 3", len(or))
	}
	fields := map[string]bool{}
	for _, clause := range or {
		for k, v := range clause {
			fields[k] = true
			m, ok := v.(bson.M)
			if !ok {
				t.Fatalf("clause %s = %T", k, v)
			}
			if m["$regex"] != "cabin" {
				t.Errorf("%s regex = %v, want cabin", k, m["$regex"])
			}
			if m["$options"] != "i" {
				t.Errorf("%s should be case-insensitive, got options %v", k, m["$options"])
			}
		}
	}
	for _, want := range []string{"title", "description", "location"} {
		if !fields[want] {
			t.Errorf("text search does not cover %q", want)
		}
	}
}

// The city filter is anchored and comma-terminated so "Detroit" doesn't match
// "Detroit Lakes". Regex metacharacters in user input must be escaped.
func TestBuildListingFilterCityIsAnchoredAndEscaped(t *testing.T) {
	got := buildListingFilter(models.ListingSearchParams{City: "St. Louis"})
	m, ok := got["location"].(bson.M)
	if !ok {
		t.Fatalf("location = %T, want bson.M", got["location"])
	}
	rx, _ := m["$regex"].(string)
	if rx != `^St\. Louis\s*,` {
		t.Errorf("city regex = %q, want the dot escaped and the value anchored", rx)
	}
}

func TestBuildListingFilterCityRegexIsInjectionSafe(t *testing.T) {
	// A hostile city value must not be able to inject alternation.
	got := buildListingFilter(models.ListingSearchParams{City: "a|b.*"})
	m := got["location"].(bson.M)
	rx := m["$regex"].(string)
	if rx != `^a\|b\.\*\s*,` {
		t.Errorf("regex = %q, want metacharacters escaped", rx)
	}
}

// City is applied after Location and shares the same key, so it wins. Pinning
// this documents the precedence rather than leaving it to chance.
func TestBuildListingFilterCityOverridesLocation(t *testing.T) {
	got := buildListingFilter(models.ListingSearchParams{Location: "Michigan", City: "Detroit"})
	m := got["location"].(bson.M)
	rx := m["$regex"].(string)
	if rx != `^Detroit\s*,` {
		t.Errorf("location = %q, want the City filter to take precedence", rx)
	}
}

func TestBuildListingFilterPriceRange(t *testing.T) {
	tests := []struct {
		name             string
		min, max         float64
		wantKey          bool
		wantGte, wantLte interface{}
	}{
		{"neither bound", 0, 0, false, nil, nil},
		{"min only", 50, 0, true, 50.0, nil},
		{"max only", 0, 200, true, nil, 200.0},
		{"both bounds", 50, 200, true, 50.0, 200.0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := buildListingFilter(models.ListingSearchParams{MinPrice: tc.min, MaxPrice: tc.max})
			raw, present := got["price_numeric"]
			if present != tc.wantKey {
				t.Fatalf("price_numeric present = %v, want %v", present, tc.wantKey)
			}
			if !tc.wantKey {
				return
			}
			m := raw.(bson.M)
			if gte, ok := m["$gte"]; ok != (tc.wantGte != nil) || (ok && gte != tc.wantGte) {
				t.Errorf("$gte = %v (present %v), want %v", gte, ok, tc.wantGte)
			}
			if lte, ok := m["$lte"]; ok != (tc.wantLte != nil) || (ok && lte != tc.wantLte) {
				t.Errorf("$lte = %v (present %v), want %v", lte, ok, tc.wantLte)
			}
		})
	}
}

func TestBuildListingFilterRatingOnlyAppliesWhenPositive(t *testing.T) {
	if got := buildListingFilter(models.ListingSearchParams{MinRating: 0}); got["rating_numeric"] != nil {
		t.Errorf("zero rating should not filter, got %v", got["rating_numeric"])
	}
	got := buildListingFilter(models.ListingSearchParams{MinRating: 4.5})
	m, ok := got["rating_numeric"].(bson.M)
	if !ok || m["$gte"] != 4.5 {
		t.Errorf("rating_numeric = %v, want $gte 4.5", got["rating_numeric"])
	}
}

// Features are conjunctive: a listing must have all of them, not any.
func TestBuildListingFilterFeaturesUseAll(t *testing.T) {
	if got := buildListingFilter(models.ListingSearchParams{Features: nil}); got["features"] != nil {
		t.Errorf("no features should not filter, got %v", got["features"])
	}
	feats := []string{"hot_tub", "pool"}
	got := buildListingFilter(models.ListingSearchParams{Features: feats})
	m, ok := got["features"].(bson.M)
	if !ok {
		t.Fatalf("features = %T", got["features"])
	}
	if !reflect.DeepEqual(m["$all"], feats) {
		t.Errorf("features = %v, want $all %v", m, feats)
	}
}

// Property type is matched exactly, unlike the regex-based location filters.
func TestBuildListingFilterPropertyTypeIsExact(t *testing.T) {
	got := buildListingFilter(models.ListingSearchParams{PropertyType: "Cabin"})
	if got["property_type"] != "Cabin" {
		t.Errorf("property_type = %v, want the literal string Cabin", got["property_type"])
	}
}

func TestBuildListingFilterCombinesIndependentCriteria(t *testing.T) {
	got := buildListingFilter(models.ListingSearchParams{
		Query: "lake", Region: "Midwest", Country: "USA",
		MinPrice: 100, MinRating: 4, Features: []string{"pool"},
		PropertyType: "House",
	})
	for _, key := range []string{"$or", "region", "country", "price_numeric", "rating_numeric", "features", "property_type"} {
		if _, ok := got[key]; !ok {
			t.Errorf("combined filter missing %q", key)
		}
	}
}

func TestBuildListingSort(t *testing.T) {
	tests := map[string]bson.M{
		"price_asc":   {"price_numeric": 1},
		"price_desc":  {"price_numeric": -1},
		"rating_desc": {"rating_numeric": -1},
		"newest":      {"created_at": -1},
		"":            {"created_at": -1},
		"bogus_value": {"created_at": -1}, // unknown keys fall back, never panic
	}
	for in, want := range tests {
		if got := buildListingSort(in); !reflect.DeepEqual(got, want) {
			t.Errorf("buildListingSort(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParsePrice(t *testing.T) {
	tests := map[string]float64{
		"":            0,
		"$1,234":      1234,
		"$99":         99,
		"99":          99,
		"$1,234.56":   1234.56,
		"£450 night":  450,
		"not a price": 0,
	}
	for in, want := range tests {
		if got := parsePrice(in); got != want {
			t.Errorf("parsePrice(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseRating(t *testing.T) {
	tests := map[string]float64{
		"":            0,
		"4.5":         4.5,
		"4.95 (123)":  4.95,
		"5":           5,
		"no rating":   0,
		"New listing": 0,
	}
	for in, want := range tests {
		if got := parseRating(in); got != want {
			t.Errorf("parseRating(%q) = %v, want %v", in, got, want)
		}
	}
}
