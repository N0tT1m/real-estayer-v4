package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// OverpassService queries OpenStreetMap for restaurants, cafes, sights, and
// other POIs around a coordinate. Keyless and free; we're polite about it
// (low timeout, lightweight queries, cache responses in-process for 5 min).
type OverpassService struct {
	client   *http.Client
	endpoint string
	cache    map[string]overpassCacheEntry
}

type overpassCacheEntry struct {
	value     []Place
	expiresAt time.Time
}

// Place is a small view-model usable by both the destination and "around me"
// pages. It's intentionally a subset of the full OSM properties.
type Place struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Category string  `json:"category"`
	Subtype  string  `json:"subtype,omitempty"`
	Lat      float64 `json:"lat"`
	Lng      float64 `json:"lng"`
	Address  string  `json:"address,omitempty"`
	Website  string  `json:"website,omitempty"`
	Phone    string  `json:"phone,omitempty"`
	Cuisine  string  `json:"cuisine,omitempty"`
	Source   string  `json:"source"` // "osm"
}

func NewOverpassService() *OverpassService {
	return &OverpassService{
		client:   &http.Client{Timeout: 15 * time.Second},
		endpoint: "https://overpass-api.de/api/interpreter",
		cache:    map[string]overpassCacheEntry{},
	}
}

const (
	// defaultRadiusM applies when a caller passes nothing sensible.
	defaultRadiusM = 1500
	// maxRadiusM is the ceiling. Beyond this the public Overpass instance
	// starts answering 504 for the broader categories (beach, hiking).
	maxRadiusM = 10000
)

// ErrUnsupportedCategory means the caller named a category buildOverpassQuery
// doesn't know — a client mistake, worth a 400.
var ErrUnsupportedCategory = errors.New("overpass: unsupported category")

// ErrUpstreamBusy means the public Overpass instance rate-limited us or timed
// the query out. Nothing is wrong with the request; it is worth retrying.
var ErrUpstreamBusy = errors.New("overpass: upstream busy")

// Nearby returns POIs within `radiusM` metres of (lat,lng) for the given
// category.
//
// Places: food, coffee, sight, park, shop, museum, bar, nightclub.
// Activities: climbing, surf, dive, hiking, cycling, beach, swimming, golf,
// ski, spa, wildlife.
//
// The full list lives in buildOverpassQuery; anything else is an error rather
// than an empty result, so a typo in a caller is loud.
func (s *OverpassService) Nearby(ctx context.Context, category string, lat, lng float64, radiusM int) ([]Place, error) {
	// An out-of-range radius used to fall all the way back to 1500 m, so
	// asking for 12 km silently searched 1.5 km and looked like "nothing here".
	// Clamp to the ceiling instead, and say so.
	switch {
	case radiusM <= 0:
		radiusM = defaultRadiusM
	case radiusM > maxRadiusM:
		slog.Debug("overpass: radius clamped to ceiling",
			"requested_m", radiusM, "used_m", maxRadiusM, "category", category)
		radiusM = maxRadiusM
	}
	q := buildOverpassQuery(category, lat, lng, radiusM)
	if q == "" {
		return nil, fmt.Errorf("%w: %q", ErrUnsupportedCategory, category)
	}
	key := fmt.Sprintf("%s|%.4f|%.4f|%d", category, lat, lng, radiusM)
	if v, ok := s.cache[key]; ok && time.Now().Before(v.expiresAt) {
		return v.value, nil
	}

	form := url.Values{}
	form.Set("data", q)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Real-Estayer/1.0 (https://real-estayer.app)")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		// Distinguish "we asked wrong" from "the public instance is busy".
		// Overpass answers 429 when rate-limiting and 504 when a query is too
		// heavy; both were reaching the browser as HTTP 400, which blamed the
		// user for an upstream condition they can only wait out.
		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode == http.StatusGatewayTimeout {
			return nil, fmt.Errorf("%w: status %d", ErrUpstreamBusy, resp.StatusCode)
		}
		return nil, fmt.Errorf("overpass: status %d", resp.StatusCode)
	}

	var raw struct {
		Elements []struct {
			Type   string  `json:"type"`
			ID     int64   `json:"id"`
			Lat    float64 `json:"lat"`
			Lon    float64 `json:"lon"`
			Center *struct {
				Lat float64 `json:"lat"`
				Lon float64 `json:"lon"`
			} `json:"center"`
			Tags map[string]string `json:"tags"`
		} `json:"elements"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	out := make([]Place, 0, len(raw.Elements))
	for _, e := range raw.Elements {
		name := e.Tags["name"]
		if name == "" {
			continue
		}
		p := Place{
			ID:       fmt.Sprintf("%s/%d", e.Type, e.ID),
			Name:     name,
			Category: category,
			Subtype:  firstNonEmpty(e.Tags["amenity"], e.Tags["tourism"], e.Tags["shop"], e.Tags["leisure"]),
			Address:  joinNonEmpty(", ", e.Tags["addr:housenumber"]+" "+e.Tags["addr:street"], e.Tags["addr:city"]),
			Website:  firstNonEmpty(e.Tags["website"], e.Tags["contact:website"]),
			Phone:    firstNonEmpty(e.Tags["phone"], e.Tags["contact:phone"]),
			Cuisine:  e.Tags["cuisine"],
			Source:   "osm",
		}
		if e.Lat != 0 || e.Lon != 0 {
			p.Lat, p.Lng = e.Lat, e.Lon
		} else if e.Center != nil {
			p.Lat, p.Lng = e.Center.Lat, e.Center.Lon
		} else {
			continue
		}
		out = append(out, p)
	}

	s.cache[key] = overpassCacheEntry{value: out, expiresAt: time.Now().Add(5 * time.Minute)}
	return out, nil
}

// buildOverpassQuery builds the Overpass QL string. We ask for nodes + ways
// + relations (with `out center`) so things like parks render.
func buildOverpassQuery(category string, lat, lng float64, radius int) string {
	// (node/way/relation ... filter ...)
	var filters []string
	switch strings.ToLower(category) {
	case "food":
		filters = []string{`["amenity"~"restaurant|fast_food"]`}
	case "coffee":
		filters = []string{`["amenity"="cafe"]`}
	case "bar":
		filters = []string{`["amenity"~"bar|pub"]`}
	case "nightclub":
		filters = []string{`["amenity"="nightclub"]`}
	case "sight":
		filters = []string{`["tourism"~"attraction|viewpoint|artwork|museum|gallery"]`}
	case "museum":
		filters = []string{`["tourism"="museum"]`}
	case "park":
		filters = []string{`["leisure"~"park|garden|nature_reserve"]`}
	case "shop":
		filters = []string{`["shop"]`}

	// Activity categories. These answer "where can I actually do X here",
	// which the categories above can't — they only cover eating, drinking,
	// and sightseeing. OSM tags a sport two ways: sport=* on a venue, and
	// route=* on a linear feature (a trail is a relation, not a node), so
	// the ones with routes list both filters.
	case "climbing":
		filters = []string{`["sport"="climbing"]`, `["climbing"]`}
	case "surf":
		filters = []string{`["sport"="surfing"]`}
	case "dive":
		filters = []string{`["sport"="scuba_diving"]`, `["shop"="scuba_diving"]`}
	case "hiking":
		filters = []string{`["route"="hiking"]`, `["information"="guidepost"]`}
	case "cycling":
		filters = []string{`["route"="bicycle"]`, `["shop"="bicycle"]`}
	case "beach":
		filters = []string{`["natural"="beach"]`, `["leisure"="beach_resort"]`}
	case "swimming":
		filters = []string{`["sport"="swimming"]`, `["leisure"="swimming_pool"]`}
	case "golf":
		filters = []string{`["leisure"="golf_course"]`}
	case "ski":
		filters = []string{`["landuse"="winter_sports"]`, `["sport"="skiing"]`}
	case "spa":
		filters = []string{`["leisure"="spa"]`, `["amenity"="spa"]`}
	case "wildlife":
		filters = []string{`["leisure"="nature_reserve"]`, `["tourism"="zoo"]`}

	default:
		return ""
	}
	var body strings.Builder
	body.WriteString(`[out:json][timeout:12];(`)
	for _, f := range filters {
		fmt.Fprintf(&body, `node%s(around:%d,%f,%f);`, f, radius, lat, lng)
		fmt.Fprintf(&body, `way%s(around:%d,%f,%f);`, f, radius, lat, lng)
		fmt.Fprintf(&body, `relation%s(around:%d,%f,%f);`, f, radius, lat, lng)
	}
	body.WriteString(`);out center 50;`)
	return body.String()
}

func firstNonEmpty(xs ...string) string {
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			return strings.TrimSpace(x)
		}
	}
	return ""
}

func joinNonEmpty(sep string, xs ...string) string {
	parts := make([]string, 0, len(xs))
	for _, x := range xs {
		if strings.TrimSpace(x) != "" {
			parts = append(parts, strings.TrimSpace(x))
		}
	}
	return strings.Join(parts, sep)
}
