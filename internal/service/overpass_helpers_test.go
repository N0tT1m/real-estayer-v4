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
