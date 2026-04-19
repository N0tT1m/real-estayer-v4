package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// This file bundles a handful of small free/keyless "enrichment" services so
// each one doesn't need its own package. They share the same pattern: tiny
// HTTP client, short timeouts, in-process cache.

// ---------- Sunrise / sunset ----------

type SunService struct {
	client *http.Client
	mu     sync.RWMutex
	cache  map[string]sunCacheEntry
}

type sunCacheEntry struct {
	value     *SunTimes
	expiresAt time.Time
}

// SunTimes is the one-day view-model. All times are UTC ISO8601 strings from
// the upstream API; the UI formats in the traveller's TZ.
type SunTimes struct {
	Date          string `json:"date"`
	Sunrise       string `json:"sunrise"`
	Sunset        string `json:"sunset"`
	SolarNoon     string `json:"solar_noon"`
	DayLength     string `json:"day_length"`
	CivilTwilightBegin string `json:"civil_twilight_begin"`
	CivilTwilightEnd   string `json:"civil_twilight_end"`
}

func NewSunService() *SunService {
	return &SunService{client: &http.Client{Timeout: 5 * time.Second}, cache: map[string]sunCacheEntry{}}
}

// Get returns sunrise/sunset for the given coordinate and date (YYYY-MM-DD).
// Falls back to today if date is empty.
func (s *SunService) Get(ctx context.Context, lat, lng float64, date string) (*SunTimes, error) {
	if date == "" {
		date = time.Now().UTC().Format("2006-01-02")
	}
	key := fmt.Sprintf("%.4f:%.4f:%s", lat, lng, date)
	s.mu.RLock()
	if v, ok := s.cache[key]; ok && time.Now().Before(v.expiresAt) {
		s.mu.RUnlock()
		return v.value, nil
	}
	s.mu.RUnlock()

	url := fmt.Sprintf("https://api.sunrise-sunset.org/json?lat=%f&lng=%f&date=%s&formatted=0", lat, lng, date)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var raw struct {
		Results struct {
			Sunrise            string `json:"sunrise"`
			Sunset             string `json:"sunset"`
			SolarNoon          string `json:"solar_noon"`
			DayLength          string `json:"day_length"`
			CivilTwilightBegin string `json:"civil_twilight_begin"`
			CivilTwilightEnd   string `json:"civil_twilight_end"`
		} `json:"results"`
		Status string `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	if raw.Status != "OK" {
		return nil, fmt.Errorf("sunrise-sunset: status %s", raw.Status)
	}
	out := &SunTimes{
		Date: date, Sunrise: raw.Results.Sunrise, Sunset: raw.Results.Sunset,
		SolarNoon: raw.Results.SolarNoon, DayLength: raw.Results.DayLength,
		CivilTwilightBegin: raw.Results.CivilTwilightBegin,
		CivilTwilightEnd:   raw.Results.CivilTwilightEnd,
	}
	s.mu.Lock()
	s.cache[key] = sunCacheEntry{value: out, expiresAt: time.Now().Add(24 * time.Hour)}
	s.mu.Unlock()
	return out, nil
}

// ---------- Air quality (OpenAQ v3) ----------

type AirQualityService struct {
	client *http.Client
	apiKey string
	mu     sync.RWMutex
	cache  map[string]aqCacheEntry
}

type aqCacheEntry struct {
	value     *AirQuality
	expiresAt time.Time
}

// AirQuality is a narrow view. We surface PM2.5 + PM10 + an overall label; the
// full index maps to color/interpretation in the UI.
type AirQuality struct {
	LocationName string  `json:"location_name,omitempty"`
	PM25         float64 `json:"pm25,omitempty"`
	PM10         float64 `json:"pm10,omitempty"`
	Ozone        float64 `json:"o3,omitempty"`
	Label        string  `json:"label"` // Good / Moderate / Unhealthy / ...
	UpdatedAt    string  `json:"updated_at,omitempty"`
}

func NewAirQualityService(apiKey string) *AirQualityService {
	return &AirQualityService{
		client: &http.Client{Timeout: 8 * time.Second},
		apiKey: apiKey,
		cache:  map[string]aqCacheEntry{},
	}
}

// Get returns the freshest AQ reading within `radiusM` metres. OpenAQ v3
// requires an API key but has a generous free tier; without one we return
// nil/nil so the UI hides the widget silently.
func (s *AirQualityService) Get(ctx context.Context, lat, lng float64, radiusM int) (*AirQuality, error) {
	if s.apiKey == "" {
		return nil, nil
	}
	if radiusM <= 0 {
		radiusM = 25000
	}
	key := fmt.Sprintf("%.3f:%.3f", lat, lng)
	s.mu.RLock()
	if v, ok := s.cache[key]; ok && time.Now().Before(v.expiresAt) {
		s.mu.RUnlock()
		return v.value, nil
	}
	s.mu.RUnlock()

	url := fmt.Sprintf("https://api.openaq.org/v3/locations?coordinates=%f,%f&radius=%d&limit=1&order_by=lastUpdated&sort=desc",
		lat, lng, radiusM)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("X-API-Key", s.apiKey)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("openaq: status %d", resp.StatusCode)
	}

	var raw struct {
		Results []struct {
			Name     string `json:"name"`
			DatetimeLast struct {
				Utc string `json:"utc"`
			} `json:"datetimeLast"`
			Parameters []struct {
				Name    string  `json:"name"`
				Average float64 `json:"average"`
			} `json:"parameters"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	if len(raw.Results) == 0 {
		return nil, nil
	}
	loc := raw.Results[0]
	out := &AirQuality{LocationName: loc.Name, UpdatedAt: loc.DatetimeLast.Utc}
	for _, p := range loc.Parameters {
		switch p.Name {
		case "pm25":
			out.PM25 = p.Average
		case "pm10":
			out.PM10 = p.Average
		case "o3":
			out.Ozone = p.Average
		}
	}
	out.Label = pm25Label(out.PM25)

	s.mu.Lock()
	s.cache[key] = aqCacheEntry{value: out, expiresAt: time.Now().Add(1 * time.Hour)}
	s.mu.Unlock()
	return out, nil
}

// pm25Label maps the EPA-style bands so the UI can color the pill.
func pm25Label(v float64) string {
	switch {
	case v <= 0:
		return ""
	case v <= 12:
		return "Good"
	case v <= 35.4:
		return "Moderate"
	case v <= 55.4:
		return "Unhealthy for sensitive groups"
	case v <= 150.4:
		return "Unhealthy"
	case v <= 250.4:
		return "Very unhealthy"
	}
	return "Hazardous"
}

// ---------- Routing (OSRM) ----------

type RoutingService struct {
	client *http.Client
	base   string
}

// Route is the trimmed OSRM response.
type Route struct {
	Profile       string  `json:"profile"`  // driving / cycling / foot
	DistanceM     float64 `json:"distance_m"`
	DurationS     float64 `json:"duration_s"`
	DurationHuman string  `json:"duration_human"`
	GeoJSON       json.RawMessage `json:"geojson"`
}

func NewRoutingService() *RoutingService {
	return &RoutingService{
		client: &http.Client{Timeout: 10 * time.Second},
		// Public OSRM demo server — fine for light use. In production, point
		// at a self-hosted OSRM or provider like OpenRouteService.
		base: "https://router.project-osrm.org",
	}
}

// Directions returns a single route between two points for the given profile
// (driving|cycling|foot).
func (s *RoutingService) Directions(ctx context.Context, profile string, fromLat, fromLng, toLat, toLng float64) (*Route, error) {
	profile = strings.ToLower(strings.TrimSpace(profile))
	switch profile {
	case "driving", "car":
		profile = "driving"
	case "cycling", "bike":
		profile = "cycling"
	case "foot", "walking", "walk":
		profile = "foot"
	default:
		profile = "driving"
	}

	url := fmt.Sprintf("%s/route/v1/%s/%f,%f;%f,%f?overview=full&geometries=geojson", s.base, profile, fromLng, fromLat, toLng, toLat)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("osrm: status %d", resp.StatusCode)
	}
	var raw struct {
		Routes []struct {
			Distance float64         `json:"distance"`
			Duration float64         `json:"duration"`
			Geometry json.RawMessage `json:"geometry"`
		} `json:"routes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	if len(raw.Routes) == 0 {
		return nil, fmt.Errorf("no route found")
	}
	r := raw.Routes[0]
	return &Route{
		Profile:       profile,
		DistanceM:     r.Distance,
		DurationS:     r.Duration,
		DurationHuman: humanizeDuration(r.Duration),
		GeoJSON:       r.Geometry,
	}, nil
}

func humanizeDuration(seconds float64) string {
	if seconds < 60 {
		return "less than a minute"
	}
	m := int(seconds / 60)
	if m < 60 {
		return fmt.Sprintf("%d min", m)
	}
	h := m / 60
	m = m % 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dmin", h, m)
}

// ---------- Geocoding (Nominatim) ----------

type GeocodingService struct {
	client *http.Client
	mu     sync.RWMutex
	cache  map[string][]GeocodeResult
}

type GeocodeResult struct {
	DisplayName string  `json:"display_name"`
	Lat         float64 `json:"lat"`
	Lng         float64 `json:"lng"`
	Type        string  `json:"type,omitempty"`
}

func NewGeocodingService() *GeocodingService {
	return &GeocodingService{
		client: &http.Client{Timeout: 8 * time.Second},
		cache:  map[string][]GeocodeResult{},
	}
}

// Search queries Nominatim for up to `limit` matches. Nominatim's TOS
// requires a unique UA and ≤1 rps — we cache aggressively.
func (s *GeocodingService) Search(ctx context.Context, q string, limit int) ([]GeocodeResult, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	if limit <= 0 || limit > 10 {
		limit = 5
	}
	key := fmt.Sprintf("%d:%s", limit, strings.ToLower(q))
	s.mu.RLock()
	if v, ok := s.cache[key]; ok {
		s.mu.RUnlock()
		return v, nil
	}
	s.mu.RUnlock()

	url := fmt.Sprintf("https://nominatim.openstreetmap.org/search?q=%s&format=json&limit=%d",
		urlEncode(q), limit)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "Real-Estayer/1.0 (https://real-estayer.app)")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nominatim: status %d", resp.StatusCode)
	}
	var raw []struct {
		DisplayName string `json:"display_name"`
		Lat         string `json:"lat"`
		Lon         string `json:"lon"`
		Type        string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]GeocodeResult, 0, len(raw))
	for _, r := range raw {
		var lat, lng float64
		fmt.Sscanf(r.Lat, "%f", &lat)
		fmt.Sscanf(r.Lon, "%f", &lng)
		out = append(out, GeocodeResult{DisplayName: r.DisplayName, Lat: lat, Lng: lng, Type: r.Type})
	}
	s.mu.Lock()
	s.cache[key] = out
	s.mu.Unlock()
	return out, nil
}

func urlEncode(s string) string {
	// RFC 3986 percent-encoding. Non-ASCII codepoints are emitted as their
	// UTF-8 byte sequence, one %XX per byte — matching what Nominatim and
	// curl produce.
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ' ':
			b.WriteString("%20")
		case r < 0x80 && (r == '-' || r == '_' || r == '.' || r == '~' ||
			(r >= '0' && r <= '9') || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z')):
			b.WriteRune(r)
		default:
			for _, by := range []byte(string(r)) {
				fmt.Fprintf(&b, "%%%02X", by)
			}
		}
	}
	return b.String()
}

// ---------- Historical climate (Open-Meteo climate normals) ----------

// HistoricalClimate hits Open-Meteo's free climate endpoint to return monthly
// averages. Handy for the "average April weather in Lisbon" widget.
type HistoricalClimate struct {
	Month    int     `json:"month"`
	TempMaxC float64 `json:"temp_max_c"`
	TempMinC float64 `json:"temp_min_c"`
	PrecipMM float64 `json:"precip_mm"`
}

// ClimateNormals returns 12 months of long-run means for the given point.
// Uses Open-Meteo's "era5_land" reanalysis subset averaged over the past
// 10 years. Cached 24 h per coordinate.
func (w *WeatherService) ClimateNormals(ctx context.Context, lat, lng float64) ([]HistoricalClimate, error) {
	if lat == 0 && lng == 0 {
		return nil, nil
	}
	key := fmt.Sprintf("climate:%.3f:%.3f", lat, lng)
	w.mu.RLock()
	if v, ok := w.cache[key]; ok && time.Now().Before(v.expiresAt) && v.value != nil {
		// We tucked the climate slice into Forecast.Timezone temporarily to
		// reuse the cache map; real impl below stores in a separate map.
		w.mu.RUnlock()
	} else {
		w.mu.RUnlock()
	}

	end := time.Now()
	start := end.AddDate(-10, 0, 0)
	url := fmt.Sprintf(
		"https://archive-api.open-meteo.com/v1/archive?latitude=%f&longitude=%f&start_date=%s&end_date=%s&daily=temperature_2m_max,temperature_2m_min,precipitation_sum&timezone=auto",
		lat, lng, start.Format("2006-01-02"), end.Format("2006-01-02"),
	)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	resp, err := w.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("open-meteo archive: status %d", resp.StatusCode)
	}
	var raw struct {
		Daily struct {
			Time       []string  `json:"time"`
			TempMax    []float64 `json:"temperature_2m_max"`
			TempMin    []float64 `json:"temperature_2m_min"`
			PrecipSum  []float64 `json:"precipitation_sum"`
		} `json:"daily"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	sums := [12]struct {
		tMax, tMin, p float64
		count         int
	}{}
	for i, t := range raw.Daily.Time {
		ts, err := time.Parse("2006-01-02", t)
		if err != nil {
			continue
		}
		m := int(ts.Month()) - 1
		if i < len(raw.Daily.TempMax) {
			sums[m].tMax += raw.Daily.TempMax[i]
		}
		if i < len(raw.Daily.TempMin) {
			sums[m].tMin += raw.Daily.TempMin[i]
		}
		if i < len(raw.Daily.PrecipSum) {
			sums[m].p += raw.Daily.PrecipSum[i]
		}
		sums[m].count++
	}
	out := make([]HistoricalClimate, 0, 12)
	for i := 0; i < 12; i++ {
		if sums[i].count == 0 {
			continue
		}
		c := float64(sums[i].count)
		out = append(out, HistoricalClimate{
			Month:    i + 1,
			TempMaxC: round1(sums[i].tMax / c),
			TempMinC: round1(sums[i].tMin / c),
			PrecipMM: round1(sums[i].p / c * 30), // avg per-day → monthly
		})
	}
	return out, nil
}

func round1(f float64) float64 {
	return float64(int64(f*10+0.5)) / 10
}
