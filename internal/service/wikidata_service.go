package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// WikidataService pulls a small bundle of facts about a city via the public
// SPARQL endpoint. Free and keyless; we send a polite User-Agent and cache
// responses for 24 h.
//
// The single query covers: population, founding year, elevation, timezone,
// area, sister cities, notable people born there, and official website. The
// goal is an "info box" panel, not an encyclopedia.
type WikidataService struct {
	client *http.Client
	mu     sync.RWMutex
	cache  map[string]wikidataCacheEntry
}

type wikidataCacheEntry struct {
	value     *CityFacts
	expiresAt time.Time
}

// CityFacts is the view-model.
type CityFacts struct {
	Name          string   `json:"name"`
	WikidataID    string   `json:"wikidata_id,omitempty"`
	Population    int64    `json:"population,omitempty"`
	Founded       string   `json:"founded,omitempty"`
	ElevationM    float64  `json:"elevation_m,omitempty"`
	AreaKm2       float64  `json:"area_km2,omitempty"`
	Timezone      string   `json:"timezone,omitempty"`
	OfficialSite  string   `json:"official_site,omitempty"`
	SisterCities  []string `json:"sister_cities,omitempty"`
	NotablePeople []string `json:"notable_people,omitempty"`
	WikipediaURL  string   `json:"wikipedia_url,omitempty"`
}

func NewWikidataService() *WikidataService {
	// 90s ceiling: Wikidata's public SPARQL endpoint enforces a ~60s server
	// timeout on heavy queries (e.g. P131* over US states), and DNS/TLS
	// setup eats another few seconds on cold calls. Inner call sites still
	// pass a tighter context deadline when they want one — that wins.
	return &WikidataService{
		client: &http.Client{Timeout: 90 * time.Second},
		cache:  map[string]wikidataCacheEntry{},
	}
}

// CityByName returns best-effort facts about the given city. Returns (nil,nil)
// when no entity matches so callers can treat it as "no data."
func (s *WikidataService) CityByName(ctx context.Context, name string) (*CityFacts, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	key := strings.ToLower(name)
	s.mu.RLock()
	if v, ok := s.cache[key]; ok && time.Now().Before(v.expiresAt) {
		s.mu.RUnlock()
		return v.value, nil
	}
	s.mu.RUnlock()

	qid, err := s.resolveQID(ctx, name)
	if err != nil || qid == "" {
		s.put(key, nil)
		return nil, err
	}
	facts, err := s.fetchFacts(ctx, qid)
	if err != nil {
		return nil, err
	}
	if facts != nil {
		facts.Name = name
		facts.WikidataID = qid
	}
	s.put(key, facts)
	return facts, nil
}

func (s *WikidataService) put(key string, f *CityFacts) {
	s.mu.Lock()
	s.cache[key] = wikidataCacheEntry{value: f, expiresAt: time.Now().Add(24 * time.Hour)}
	s.mu.Unlock()
}

// resolveQID uses the wbsearchentities API to pick the best-matching city
// QID. We restrict to English matches; sometimes a village beats a capital
// but for travel queries the top hit is usually the right one.
func (s *WikidataService) resolveQID(ctx context.Context, name string) (string, error) {
	q := url.Values{}
	q.Set("action", "wbsearchentities")
	q.Set("format", "json")
	q.Set("language", "en")
	q.Set("type", "item")
	q.Set("limit", "3")
	q.Set("search", name)
	u := "https://www.wikidata.org/w/api.php?" + q.Encode()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("User-Agent", "Real-Estayer/1.0 (https://real-estayer.app)")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("wikidata search: status %d", resp.StatusCode)
	}
	var raw struct {
		Search []struct {
			ID          string `json:"id"`
			Description string `json:"description"`
		} `json:"search"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return "", err
	}
	// Prefer a result whose description mentions "city", "capital", or
	// "municipality" — otherwise take the first hit.
	for _, r := range raw.Search {
		d := strings.ToLower(r.Description)
		if strings.Contains(d, "city") || strings.Contains(d, "capital") || strings.Contains(d, "metropolis") || strings.Contains(d, "municipality") {
			return r.ID, nil
		}
	}
	if len(raw.Search) > 0 {
		return raw.Search[0].ID, nil
	}
	return "", nil
}

// fetchFacts runs a single SPARQL query that joins on the resolved QID. We
// request ?format=json via the `/sparql` endpoint.
func (s *WikidataService) fetchFacts(ctx context.Context, qid string) (*CityFacts, error) {
	query := fmt.Sprintf(`
SELECT ?pop ?inception ?elev ?area ?tz ?website ?article
       (GROUP_CONCAT(DISTINCT ?sisterLabel; SEPARATOR="|") AS ?sisters)
       (GROUP_CONCAT(DISTINCT ?personLabel; SEPARATOR="|") AS ?people)
WHERE {
  BIND(wd:%s AS ?item)
  OPTIONAL { ?item wdt:P1082 ?pop. }
  OPTIONAL { ?item wdt:P571 ?inception. }
  OPTIONAL { ?item wdt:P2044 ?elev. }
  OPTIONAL { ?item wdt:P2046 ?area. }
  OPTIONAL { ?item wdt:P421 ?tzItem. ?tzItem rdfs:label ?tz FILTER(LANG(?tz)="en"). }
  OPTIONAL { ?item wdt:P856 ?website. }
  OPTIONAL { ?item wdt:P190 ?sister. ?sister rdfs:label ?sisterLabel FILTER(LANG(?sisterLabel)="en"). }
  OPTIONAL {
    ?person wdt:P19 ?item; wikibase:sitelinks ?sl.
    ?person rdfs:label ?personLabel FILTER(LANG(?personLabel)="en").
    FILTER(?sl > 30)
  }
  OPTIONAL {
    ?article schema:about ?item; schema:isPartOf <https://en.wikipedia.org/>.
  }
}
GROUP BY ?pop ?inception ?elev ?area ?tz ?website ?article
LIMIT 1
`, qid)

	u := "https://query.wikidata.org/sparql?query=" + url.QueryEscape(query) + "&format=json"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("Accept", "application/sparql-results+json")
	req.Header.Set("User-Agent", "Real-Estayer/1.0 (https://real-estayer.app)")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wikidata sparql: status %d", resp.StatusCode)
	}
	var raw struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
				Type  string `json:"type"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	if len(raw.Results.Bindings) == 0 {
		return nil, nil
	}
	b := raw.Results.Bindings[0]
	out := &CityFacts{}
	if v, ok := b["pop"]; ok {
		if n, err := strconv.ParseInt(v.Value, 10, 64); err == nil {
			out.Population = n
		}
	}
	if v, ok := b["inception"]; ok {
		out.Founded = trimISODate(v.Value)
	}
	if v, ok := b["elev"]; ok {
		if f, err := strconv.ParseFloat(v.Value, 64); err == nil {
			out.ElevationM = f
		}
	}
	if v, ok := b["area"]; ok {
		if f, err := strconv.ParseFloat(v.Value, 64); err == nil {
			out.AreaKm2 = f
		}
	}
	if v, ok := b["tz"]; ok {
		out.Timezone = v.Value
	}
	if v, ok := b["website"]; ok {
		out.OfficialSite = v.Value
	}
	if v, ok := b["article"]; ok {
		out.WikipediaURL = v.Value
	}
	if v, ok := b["sisters"]; ok {
		out.SisterCities = splitPipe(v.Value, 10)
	}
	if v, ok := b["people"]; ok {
		out.NotablePeople = splitPipe(v.Value, 10)
	}
	return out, nil
}

func trimISODate(s string) string {
	// Wikidata returns ISO-8601 like "1147-01-01T00:00:00Z"; strip the time.
	if i := strings.Index(s, "T"); i > 0 {
		return s[:i]
	}
	return s
}

// --- discovery (used by admin to find top destinations in a region) ---

// ResolvedEntity is the public result of looking up a Wikidata entity by
// free-text name. Admins type "Michigan" or "Bavaria" and we map that to a
// single QID plus the human-readable label/description so the UI can
// confirm the disambiguation went the right way.
type ResolvedEntity struct {
	QID         string `json:"qid"`
	Label       string `json:"label"`
	Description string `json:"description"`
}

// ResolveEntity maps a free-text region/place name to the best Wikidata
// QID match. Unlike resolveQID (which biases toward cities), this one
// prefers descriptions that look like regions — states, provinces,
// countries — because discovery is typically over an administrative area.
func (s *WikidataService) ResolveEntity(ctx context.Context, name string) (*ResolvedEntity, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	q := url.Values{}
	q.Set("action", "wbsearchentities")
	q.Set("format", "json")
	q.Set("language", "en")
	q.Set("type", "item")
	q.Set("limit", "10")
	q.Set("search", name)
	u := "https://www.wikidata.org/w/api.php?" + q.Encode()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("User-Agent", "Real-Estayer/1.0 (https://real-estayer.app)")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wikidata search: status %d", resp.StatusCode)
	}

	var raw struct {
		Search []struct {
			ID          string `json:"id"`
			Label       string `json:"label"`
			Description string `json:"description"`
		} `json:"search"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	if len(raw.Search) == 0 {
		return nil, nil
	}

	// Prefer administrative-area-ish descriptions (state, province, country,
	// region). If none match, fall back to the first hit.
	regionHints := []string{"state of", "province of", "country in", "region of", "county in", "autonomous", "state in", "department of", "prefecture"}
	for _, r := range raw.Search {
		d := strings.ToLower(r.Description)
		for _, h := range regionHints {
			if strings.Contains(d, h) {
				return &ResolvedEntity{QID: r.ID, Label: r.Label, Description: r.Description}, nil
			}
		}
	}
	top := raw.Search[0]
	return &ResolvedEntity{QID: top.ID, Label: top.Label, Description: top.Description}, nil
}

// DiscoveredPlace is a raw SPARQL row — one candidate destination within a
// region. Pageview ranking and Wikipedia enrichment happen downstream in
// DestinationDiscoveryService.
type DiscoveredPlace struct {
	QID            string   `json:"qid"`
	Name           string   `json:"name"`
	Description    string   `json:"description"` // short Wikidata gloss, not Wikipedia extract
	CountryCode    string   `json:"country_code"`
	Latitude       float64  `json:"latitude"`
	Longitude      float64  `json:"longitude"`
	ArticleTitle   string   `json:"article_title"` // en.wikipedia.org/wiki/<this>
	WikipediaURL   string   `json:"wikipedia_url"`
	SitelinkCount  int      `json:"sitelink_count"`
	Types          []string `json:"types"` // e.g. ["city","tourist attraction"]
}

// DiscoverTourismPlaces returns tourism-relevant places located (transitively)
// within the given region QID, pre-filtered by Wikipedia sitelink count as a
// rough popularity floor. Returns up to ~200 candidates; the caller ranks
// them with pageviews and trims.
//
// We union across common tourism-relevant Wikidata classes:
//   - Q486972  human settlement (city/town/village)
//   - Q570116  tourist attraction
//   - Q33506   museum
//   - Q46169   national park
//   - Q22698   park
//   - Q9259    World Heritage Site
//   - Q23397   lake
//   - Q46831   mountain range
//   - Q4022    river
//   - Q40080   beach
func (s *WikidataService) DiscoverTourismPlaces(ctx context.Context, regionQID string) ([]DiscoveredPlace, error) {
	query := fmt.Sprintf(`
SELECT DISTINCT ?place ?placeLabel ?desc ?countryCode ?coord ?sitelinks ?article
WHERE {
  ?place wdt:P131* wd:%s ;
         wikibase:sitelinks ?sitelinks .
  FILTER(?sitelinks >= 3)

  VALUES ?type {
    wd:Q486972 wd:Q570116 wd:Q33506 wd:Q46169
    wd:Q22698  wd:Q9259   wd:Q23397 wd:Q46831
    wd:Q4022   wd:Q40080
  }
  ?place wdt:P31/wdt:P279* ?type .

  ?article schema:about ?place ; schema:isPartOf <https://en.wikipedia.org/> .

  OPTIONAL {
    ?place wdt:P17 ?country .
    ?country wdt:P297 ?countryCode .
  }
  OPTIONAL { ?place wdt:P625 ?coord . }
  OPTIONAL { ?place schema:description ?desc . FILTER(LANG(?desc) = "en") }

  SERVICE wikibase:label { bd:serviceParam wikibase:language "en" . }
}
ORDER BY DESC(?sitelinks)
LIMIT 200
`, regionQID)

	// POST the query so it doesn't ride in the URL — SPARQL queries over large
	// regions can exceed practical GET length limits, and some proxies cap
	// query strings below what Wikidata will accept.
	form := url.Values{}
	form.Set("query", query)
	form.Set("format", "json")
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, "https://query.wikidata.org/sparql", strings.NewReader(form.Encode()))
	req.Header.Set("Accept", "application/sparql-results+json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Real-Estayer/1.0 (https://real-estayer.app)")

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wikidata discover: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wikidata discover: status %d", resp.StatusCode)
	}

	var raw struct {
		Results struct {
			Bindings []map[string]struct {
				Value string `json:"value"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	out := make([]DiscoveredPlace, 0, len(raw.Results.Bindings))
	seen := map[string]struct{}{}
	for _, b := range raw.Results.Bindings {
		p := DiscoveredPlace{}

		if v, ok := b["place"]; ok {
			p.QID = lastPathSegment(v.Value)
		}
		if v, ok := b["placeLabel"]; ok {
			p.Name = v.Value
		}
		// Skip rows that use the raw QID as the label — happens when no English
		// label is attached, and we don't want "Q12345" showing up in the UI.
		if p.Name == "" || strings.HasPrefix(p.Name, "Q") && isAllDigitsAfter(p.Name, 1) {
			continue
		}
		if _, dup := seen[p.QID]; dup {
			continue
		}
		seen[p.QID] = struct{}{}

		if v, ok := b["desc"]; ok {
			p.Description = v.Value
		}
		if v, ok := b["countryCode"]; ok {
			p.CountryCode = v.Value
		}
		if v, ok := b["coord"]; ok {
			if lat, lng, ok := parseWKTPoint(v.Value); ok {
				p.Latitude = lat
				p.Longitude = lng
			}
		}
		if v, ok := b["sitelinks"]; ok {
			if n, err := strconv.Atoi(v.Value); err == nil {
				p.SitelinkCount = n
			}
		}
		if v, ok := b["article"]; ok {
			p.WikipediaURL = v.Value
			p.ArticleTitle = decodeArticleTitle(v.Value)
		}

		out = append(out, p)
	}
	return out, nil
}

// parseWKTPoint parses a WKT "Point(lng lat)" string (Wikidata's P625 format).
func parseWKTPoint(wkt string) (lat, lng float64, ok bool) {
	i := strings.Index(wkt, "(")
	j := strings.Index(wkt, ")")
	if i < 0 || j < 0 || j <= i {
		return 0, 0, false
	}
	parts := strings.Fields(wkt[i+1 : j])
	if len(parts) != 2 {
		return 0, 0, false
	}
	lng, err := strconv.ParseFloat(parts[0], 64)
	if err != nil {
		return 0, 0, false
	}
	lat, err = strconv.ParseFloat(parts[1], 64)
	if err != nil {
		return 0, 0, false
	}
	return lat, lng, true
}

// decodeArticleTitle extracts the URL-decoded title from a Wikipedia URL
// like https://en.wikipedia.org/wiki/Detroit -> "Detroit".
func decodeArticleTitle(wikiURL string) string {
	const prefix = "/wiki/"
	i := strings.Index(wikiURL, prefix)
	if i < 0 {
		return ""
	}
	raw := wikiURL[i+len(prefix):]
	decoded, err := url.PathUnescape(raw)
	if err != nil {
		return strings.ReplaceAll(raw, "_", " ")
	}
	return strings.ReplaceAll(decoded, "_", " ")
}

// lastPathSegment returns everything after the final "/" — used to pull the
// QID out of a Wikidata entity URL like http://www.wikidata.org/entity/Q12345.
func lastPathSegment(s string) string {
	if i := strings.LastIndex(s, "/"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func isAllDigitsAfter(s string, start int) bool {
	if start >= len(s) {
		return false
	}
	for i := start; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func splitPipe(s string, max int) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, "|")
	seen := map[string]struct{}{}
	out := make([]string, 0, max)
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
		if len(out) >= max {
			break
		}
	}
	return out
}
