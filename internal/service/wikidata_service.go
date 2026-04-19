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
	return &WikidataService{
		client: &http.Client{Timeout: 15 * time.Second},
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
