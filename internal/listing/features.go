// Package listing contains helpers for working with listing data.
package listing

import (
	"strings"
	"unicode"
)

// NormalizeFeature collapses a free-text amenity string into one of the
// canonical categories the app filters on (e.g. "Hot Tub", "Pool"). Anything
// that doesn't match a known category falls through as title-cased input so
// we don't lose information. Badges are kept verbatim because their exact
// wording matters in the UI.
func NormalizeFeature(feature string) string {
	trimmed := strings.TrimSpace(feature)
	if trimmed == "" {
		return ""
	}
	lower := strings.ToLower(trimmed)

	// Hot tub and its many aliases. Note: "spa" is ambiguous — the substring
	// "space" would false-match it, so we require the bare word.
	if containsAny(lower, "hot tub", "hottub", "hot-tub", "jacuzzi", "whirlpool",
		"jetted tub", "jet tub", "soaking tub", "spa tub", "hydrotherapy", "plunge pool") ||
		(strings.Contains(lower, "spa") && !strings.Contains(lower, "space")) {
		return "Hot Tub"
	}
	if containsAny(lower, "pool", "swimming") {
		return "Pool"
	}
	if containsAny(lower, "waterfront", "lakefront", "beachfront", "oceanfront",
		"lake view", "ocean view", "water view", "beach view") {
		return "Waterfront"
	}
	if lower == "a/c" || lower == "ac" || containsAny(lower, "air conditioning", "aircon") {
		return "Air Conditioning"
	}
	if containsAny(lower, "wifi", "wi-fi", "wireless", "internet") {
		return "WiFi"
	}
	if strings.Contains(lower, "kitchen") {
		return "Kitchen"
	}
	// The dishwasher/hair-dryer guards matter: "dishwasher" contains "washer"
	// and "hair dryer" contains "dryer", so without them a dishwasher is
	// labelled laundry and a hair dryer a clothes dryer. Both then fall
	// through to titleCase and keep their own names.
	if containsAny(lower, "washer", "washing machine", "laundry") &&
		!strings.Contains(lower, "dishwasher") {
		return "Washer"
	}
	if strings.Contains(lower, "dryer") &&
		!containsAny(lower, "hair dryer", "hairdryer", "blow dryer") {
		return "Dryer"
	}
	if containsAny(lower, "workspace", "work space", "desk") {
		return "Workspace"
	}
	if containsAny(lower, "parking", "garage") {
		return "Parking"
	}
	if containsAny(lower, "gym", "fitness", "exercise") {
		return "Gym"
	}
	if lower == "heat" || containsAny(lower, "heating", "heater") {
		return "Heating"
	}
	if containsAny(lower, "tv", "television") {
		return "TV"
	}
	if containsAny(lower, "fireplace", "fire pit") {
		return "Fireplace"
	}
	if containsAny(lower, "bbq", "grill", "barbecue") {
		return "BBQ Grill"
	}
	if containsAny(lower, "sauna", "steam room") {
		return "Sauna"
	}
	if containsAny(lower, "elevator", "lift") {
		return "Elevator"
	}
	if containsAny(lower, "balcony", "patio", "deck", "terrace", "garden", "yard") {
		return "Outdoor Space"
	}

	// Badges get preserved verbatim.
	switch lower {
	case "superhost", "guest favorite", "rare find", "top rated", "highly rated":
		return trimmed
	}

	return titleCase(trimmed)
}

// NormalizeFeatures runs each entry through NormalizeFeature and de-duplicates
// while preserving input order. Empty entries are dropped.
func NormalizeFeatures(features []string) []string {
	out := make([]string, 0, len(features))
	seen := make(map[string]struct{}, len(features))
	for _, f := range features {
		n := NormalizeFeature(f)
		if n == "" {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	return out
}

func containsAny(s string, needles ...string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

func titleCase(s string) string {
	parts := strings.Fields(s)
	for i, p := range parts {
		runes := []rune(strings.ToLower(p))
		if len(runes) > 0 {
			runes[0] = unicode.ToUpper(runes[0])
		}
		parts[i] = string(runes)
	}
	return strings.Join(parts, " ")
}
