package service

import (
	"context"
	"encoding/json"
	"fmt"
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

// Nearby returns POIs within `radiusM` metres of (lat,lng) for the given
// category. Supported categories: food, coffee, sight, park, shop, museum,
// bar, nightclub.
func (s *OverpassService) Nearby(ctx context.Context, category string, lat, lng float64, radiusM int) ([]Place, error) {
	if radiusM <= 0 || radiusM > 10000 {
		radiusM = 1500
	}
	q := buildOverpassQuery(category, lat, lng, radiusM)
	if q == "" {
		return nil, fmt.Errorf("overpass: unsupported category %q", category)
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
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("overpass: status %d", resp.StatusCode)
	}

	var raw struct {
		Elements []struct {
			Type   string             `json:"type"`
			ID     int64              `json:"id"`
			Lat    float64            `json:"lat"`
			Lon    float64            `json:"lon"`
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
			ID:      fmt.Sprintf("%s/%d", e.Type, e.ID),
			Name:    name,
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
