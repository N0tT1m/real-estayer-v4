package service

import (
	"strings"
	"testing"
)

func TestBuildOverpassQueryCoversKnownCategories(t *testing.T) {
	cases := map[string]string{
		"food":      "restaurant",
		"coffee":    "cafe",
		"bar":       "bar",
		"nightclub": "nightclub",
		"sight":     "attraction",
		"museum":    "museum",
		"park":      "park",
	}
	for cat, mustInclude := range cases {
		q := buildOverpassQuery(cat, 10, 20, 1000)
		if q == "" {
			t.Errorf("%s: empty query", cat)
			continue
		}
		if !strings.Contains(q, mustInclude) {
			t.Errorf("%s: query missing %q:\n%s", cat, mustInclude, q)
		}
		if !strings.Contains(q, "around:1000,") {
			t.Errorf("%s: radius not interpolated", cat)
		}
	}
}

// The activity categories are the half that answers "where can I go do X".
// Each must produce a query carrying its OSM tag.
func TestBuildOverpassQueryCoversActivityCategories(t *testing.T) {
	cases := map[string]string{
		"climbing": `["sport"="climbing"]`,
		"surf":     `["sport"="surfing"]`,
		"dive":     `["sport"="scuba_diving"]`,
		"hiking":   `["route"="hiking"]`,
		"cycling":  `["route"="bicycle"]`,
		"beach":    `["natural"="beach"]`,
		"swimming": `["sport"="swimming"]`,
		"golf":     `["leisure"="golf_course"]`,
		"ski":      `["landuse"="winter_sports"]`,
		"spa":      `["leisure"="spa"]`,
		"wildlife": `["leisure"="nature_reserve"]`,
	}
	for cat, mustInclude := range cases {
		q := buildOverpassQuery(cat, 10, 20, 2500)
		if q == "" {
			t.Errorf("%s: empty query", cat)
			continue
		}
		if !strings.Contains(q, mustInclude) {
			t.Errorf("%s: query missing %q:\n%s", cat, mustInclude, q)
		}
		if !strings.Contains(q, "around:2500,") {
			t.Errorf("%s: radius not interpolated", cat)
		}
		// A trail or cycle route is a relation, never a node, so every
		// category has to query all three element types to find anything.
		for _, elem := range []string{"node", "way", "relation"} {
			if !strings.Contains(q, elem) {
				t.Errorf("%s: query should cover %s elements:\n%s", cat, elem, q)
			}
		}
	}
}

func TestBuildOverpassQueryIsCaseInsensitive(t *testing.T) {
	if buildOverpassQuery("Climbing", 0, 0, 1000) == "" {
		t.Error("category matching should not depend on case")
	}
}

func TestBuildOverpassQueryShop(t *testing.T) {
	q := buildOverpassQuery("shop", 0, 0, 1000)
	if !strings.Contains(q, `["shop"]`) {
		t.Errorf("shop category should use ['shop'] filter: %s", q)
	}
}

func TestBuildOverpassQueryUnsupportedReturnsEmpty(t *testing.T) {
	if buildOverpassQuery("spacerocket", 0, 0, 500) != "" {
		t.Errorf("unknown category should return empty string")
	}
}
