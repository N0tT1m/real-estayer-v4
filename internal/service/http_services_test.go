package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestWeatherForecastDecodesAndCaches spins up a stub Open-Meteo and verifies
// the parser maps the JSON correctly + caches within the TTL.
func TestWeatherForecastDecodesAndCaches(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if !strings.Contains(r.URL.Path, "/forecast") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"latitude": 38.72, "longitude": -9.14, "timezone": "Europe/Lisbon",
			"daily": map[string]interface{}{
				"time":                          []string{"2025-06-01", "2025-06-02"},
				"temperature_2m_max":            []float64{25.0, 27.5},
				"temperature_2m_min":            []float64{15.5, 16.0},
				"precipitation_probability_max": []int{10, 40},
				"precipitation_sum":             []float64{0.0, 2.5},
				"wind_speed_10m_max":            []float64{8, 10},
				"weather_code":                  []int{0, 61},
			},
		})
	}))
	defer srv.Close()

	s := NewWeatherService()
	s.client.Transport = rewriteTransport(srv.URL)

	f, err := s.Get(context.Background(), 38.72, -9.14, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Daily) != 2 {
		t.Fatalf("expected 2 daily entries, got %d", len(f.Daily))
	}
	if f.Daily[0].WeatherEmoji != "☀️" {
		t.Errorf("code 0 should be clear; got %q", f.Daily[0].WeatherEmoji)
	}
	if f.Daily[1].WeatherLabel != "Rain" {
		t.Errorf("code 61 should be Rain; got %q", f.Daily[1].WeatherLabel)
	}

	// Second call within TTL should hit the cache.
	if _, err := s.Get(context.Background(), 38.72, -9.14, 2); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Errorf("expected 1 upstream call (cache hit 2nd time), got %d", hits)
	}
}

func TestWeatherZeroCoordsReturnsNil(t *testing.T) {
	s := NewWeatherService()
	f, err := s.Get(context.Background(), 0, 0, 7)
	if err != nil || f != nil {
		t.Errorf("(0,0) should short-circuit to nil/nil, got %v / %v", f, err)
	}
}

// TestCurrencyConvertUsesCachedRates hits a stub ECB feed and checks math.
func TestCurrencyConvertUsesCachedRates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?>
<gesmes:Envelope xmlns:gesmes="x">
  <Cube>
    <Cube time="2025-01-01">
      <Cube currency="USD" rate="1.1"/>
      <Cube currency="GBP" rate="0.85"/>
      <Cube currency="JPY" rate="170.0"/>
    </Cube>
  </Cube>
</gesmes:Envelope>`))
	}))
	defer srv.Close()

	s := NewCurrencyService()
	s.client.Transport = rewriteTransport(srv.URL)

	ctx := context.Background()
	// EUR→USD: 100 EUR × 1.1 = 110 USD
	got, err := s.Convert(ctx, 100, "EUR", "USD")
	if err != nil {
		t.Fatal(err)
	}
	if got < 109.9 || got > 110.1 {
		t.Errorf("100 EUR→USD ≈ %.2f; want ~110", got)
	}
	// USD→GBP: 100 USD / 1.1 × 0.85 ≈ 77.27
	got, err = s.Convert(ctx, 100, "USD", "GBP")
	if err != nil {
		t.Fatal(err)
	}
	if got < 77 || got > 77.5 {
		t.Errorf("100 USD→GBP ≈ %.2f; want ~77.27", got)
	}
	// Same currency short-circuits.
	same, _ := s.Convert(ctx, 50, "EUR", "EUR")
	if same != 50 {
		t.Errorf("same-currency convert should be identity, got %v", same)
	}
}

func TestCurrencyUnknownCurrencyErrors(t *testing.T) {
	s := NewCurrencyService()
	// Seed the cache directly so ensureRates is a no-op.
	s.rates = map[string]float64{"EUR": 1, "USD": 1.1}
	s.expires = time.Now().Add(1 * time.Hour)
	_, err := s.Convert(context.Background(), 10, "EUR", "XXX")
	if err == nil {
		t.Errorf("expected error for unknown currency")
	}
}

// TestSunServiceDecodes stubs sunrise-sunset.org.
func TestSunServiceDecodes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
            "results": {
                "sunrise": "2025-06-01T05:45:00+00:00",
                "sunset":  "2025-06-01T20:10:00+00:00",
                "solar_noon": "2025-06-01T12:57:30+00:00",
                "day_length": "14:25:00",
                "civil_twilight_begin": "2025-06-01T05:10:00+00:00",
                "civil_twilight_end":   "2025-06-01T20:45:00+00:00"
            },
            "status": "OK"
        }`))
	}))
	defer srv.Close()

	s := NewSunService()
	s.client.Transport = rewriteTransport(srv.URL)

	out, err := s.Get(context.Background(), 38.7, -9.1, "2025-06-01")
	if err != nil {
		t.Fatal(err)
	}
	if out == nil || !strings.HasPrefix(out.Sunrise, "2025-06-01") {
		t.Fatalf("unexpected sun times: %+v", out)
	}
	if out.DayLength != "14:25:00" {
		t.Errorf("day length not propagated")
	}
}

func TestSunServiceErrorStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"status": "INVALID_REQUEST"}`))
	}))
	defer srv.Close()

	s := NewSunService()
	s.client.Transport = rewriteTransport(srv.URL)
	_, err := s.Get(context.Background(), 38.7, -9.1, "")
	if err == nil {
		t.Errorf("expected error when upstream reports a non-OK status")
	}
}

// TestOverpassNearby checks we tolerate both node + way(with center) shapes.
func TestOverpassNearby(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
            "elements": [
                {"type":"node","id":1,"lat":38.71,"lon":-9.14,"tags":{"name":"Nook","amenity":"cafe"}},
                {"type":"way","id":2,"center":{"lat":38.70,"lon":-9.13},"tags":{"name":"Park","leisure":"park"}},
                {"type":"node","id":3,"tags":{"name":"No coords","amenity":"cafe"}}
            ]
        }`))
	}))
	defer srv.Close()

	s := NewOverpassService()
	s.client.Transport = rewriteTransport(srv.URL)
	s.endpoint = srv.URL + "/interpreter" // rewriteTransport ignores host; endpoint path is only used to POST

	places, err := s.Nearby(context.Background(), "food", 38.71, -9.14, 1500)
	if err != nil {
		t.Fatal(err)
	}
	// Node (1) and way (2) both have coords → 2 results; node 3 has no
	// coords and should be dropped.
	if len(places) != 2 {
		t.Fatalf("expected 2 places, got %d: %+v", len(places), places)
	}
	if places[0].Name == "" || places[1].Name == "" {
		t.Errorf("names should propagate")
	}
}

// TestOverpassUnsupportedCategory short-circuits before hitting the network.
func TestOverpassUnsupportedCategory(t *testing.T) {
	s := NewOverpassService()
	_, err := s.Nearby(context.Background(), "not-a-thing", 0, 0, 100)
	if err == nil {
		t.Errorf("expected error for unsupported category")
	}
}

// TestRoutingServiceReturnsRoute stubs OSRM.
func TestRoutingServiceReturnsRoute(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{
            "routes": [
                {"distance": 5400, "duration": 720,
                 "geometry": {"type":"LineString","coordinates":[[0,0],[1,1]]}}
            ]
        }`))
	}))
	defer srv.Close()

	s := NewRoutingService()
	s.base = srv.URL
	s.client.Transport = rewriteTransport(srv.URL)

	r, err := s.Directions(context.Background(), "driving", 0, 0, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if r.Profile != "driving" {
		t.Errorf("profile echo = %q", r.Profile)
	}
	if r.DurationHuman != "12 min" {
		t.Errorf("duration humanise = %q; want 12 min", r.DurationHuman)
	}
	if len(r.GeoJSON) == 0 {
		t.Errorf("GeoJSON should be non-empty")
	}
}

func TestRoutingServiceProfileNormalisation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/cycling/") {
			t.Errorf("expected /cycling/ in path, got %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"routes":[{"distance":100,"duration":30,"geometry":{}}]}`))
	}))
	defer srv.Close()

	s := NewRoutingService()
	s.base = srv.URL
	s.client.Transport = rewriteTransport(srv.URL)

	if _, err := s.Directions(context.Background(), "bike", 0, 0, 1, 1); err != nil {
		t.Fatal(err)
	}
}

// TestGeocodingSearchParsesResult stubs Nominatim.
func TestGeocodingSearchParsesResult(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.UserAgent() == "" {
			t.Errorf("Nominatim requires a descriptive User-Agent")
		}
		_, _ = w.Write([]byte(`[
            {"display_name":"Lisbon, Portugal","lat":"38.7223","lon":"-9.1393","type":"city"}
        ]`))
	}))
	defer srv.Close()

	s := NewGeocodingService()
	s.client.Transport = rewriteTransport(srv.URL)

	out, err := s.Search(context.Background(), "Lisbon", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Lat < 38 {
		t.Fatalf("unexpected results: %+v", out)
	}
}

func TestGeocodingEmptyQuery(t *testing.T) {
	s := NewGeocodingService()
	out, err := s.Search(context.Background(), "  ", 5)
	if err != nil || out != nil {
		t.Errorf("empty query should return nil/nil, got %v/%v", out, err)
	}
}
