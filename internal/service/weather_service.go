package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// WeatherService fetches daily forecasts from Open-Meteo. Keyless, free,
// rate-limited by fair-use; we cache in-process for an hour so repeated page
// loads don't hammer them.
type WeatherService struct {
	client *http.Client
	mu     sync.RWMutex
	cache  map[string]cachedForecast
}

type cachedForecast struct {
	value     *Forecast
	expiresAt time.Time
}

// Forecast is a small view-model tailored to the trip-detail widget.
type Forecast struct {
	Latitude  float64       `json:"latitude"`
	Longitude float64       `json:"longitude"`
	Timezone  string        `json:"timezone"`
	Daily     []ForecastDay `json:"daily"`
	FetchedAt time.Time     `json:"fetched_at"`
}

// ForecastDay is one row in the daily forecast. WeatherCode follows the
// WMO weather interpretation codes returned by Open-Meteo.
type ForecastDay struct {
	Date         string  `json:"date"`
	TempMaxC     float64 `json:"temp_max_c"`
	TempMinC     float64 `json:"temp_min_c"`
	PrecipProb   int     `json:"precip_prob_pct"`
	PrecipMM     float64 `json:"precip_mm"`
	WindMaxKph   float64 `json:"wind_max_kph"`
	WeatherCode  int     `json:"weather_code"`
	WeatherLabel string  `json:"weather_label"`
	WeatherEmoji string  `json:"weather_emoji"`
}

func NewWeatherService() *WeatherService {
	return &WeatherService{
		client: &http.Client{Timeout: 5 * time.Second},
		cache:  map[string]cachedForecast{},
	}
}

// Get returns up to `days` days of forecast (max 16) around now. Coordinates
// with zero magnitude return (nil, nil) so callers can skip the widget.
func (s *WeatherService) Get(ctx context.Context, lat, lng float64, days int) (*Forecast, error) {
	if lat == 0 && lng == 0 {
		return nil, nil
	}
	if days <= 0 {
		days = 7
	}
	if days > 16 {
		days = 16
	}
	key := fmt.Sprintf("%.4f:%.4f:%d", lat, lng, days)

	s.mu.RLock()
	if v, ok := s.cache[key]; ok && time.Now().Before(v.expiresAt) {
		s.mu.RUnlock()
		return v.value, nil
	}
	s.mu.RUnlock()

	url := fmt.Sprintf(
		"https://api.open-meteo.com/v1/forecast?latitude=%f&longitude=%f&daily=temperature_2m_max,temperature_2m_min,precipitation_probability_max,precipitation_sum,wind_speed_10m_max,weather_code&forecast_days=%d&timezone=auto",
		lat, lng, days,
	)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("open-meteo: status %d", resp.StatusCode)
	}

	var raw struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
		Timezone  string  `json:"timezone"`
		Daily     struct {
			Time        []string  `json:"time"`
			TempMax     []float64 `json:"temperature_2m_max"`
			TempMin     []float64 `json:"temperature_2m_min"`
			PrecipProb  []int     `json:"precipitation_probability_max"`
			PrecipSum   []float64 `json:"precipitation_sum"`
			WindMax     []float64 `json:"wind_speed_10m_max"`
			WeatherCode []int     `json:"weather_code"`
		} `json:"daily"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	n := len(raw.Daily.Time)
	forecast := &Forecast{
		Latitude:  raw.Latitude,
		Longitude: raw.Longitude,
		Timezone:  raw.Timezone,
		Daily:     make([]ForecastDay, 0, n),
		FetchedAt: time.Now(),
	}
	for i := 0; i < n; i++ {
		d := ForecastDay{
			Date:        safeIndexStr(raw.Daily.Time, i),
			TempMaxC:    safeIndexF(raw.Daily.TempMax, i),
			TempMinC:    safeIndexF(raw.Daily.TempMin, i),
			PrecipProb:  safeIndexI(raw.Daily.PrecipProb, i),
			PrecipMM:    safeIndexF(raw.Daily.PrecipSum, i),
			WindMaxKph:  safeIndexF(raw.Daily.WindMax, i),
			WeatherCode: safeIndexI(raw.Daily.WeatherCode, i),
		}
		d.WeatherLabel, d.WeatherEmoji = weatherLabelForCode(d.WeatherCode)
		forecast.Daily = append(forecast.Daily, d)
	}

	s.mu.Lock()
	s.cache[key] = cachedForecast{value: forecast, expiresAt: time.Now().Add(1 * time.Hour)}
	s.mu.Unlock()

	return forecast, nil
}

func safeIndexStr(xs []string, i int) string {
	if i < len(xs) {
		return xs[i]
	}
	return ""
}
func safeIndexF(xs []float64, i int) float64 {
	if i < len(xs) {
		return xs[i]
	}
	return 0
}
func safeIndexI(xs []int, i int) int {
	if i < len(xs) {
		return xs[i]
	}
	return 0
}

// weatherLabelForCode maps Open-Meteo's WMO codes to a human label + emoji.
// See https://open-meteo.com/en/docs for the full table.
func weatherLabelForCode(code int) (string, string) {
	switch {
	case code == 0:
		return "Clear", "☀️"
	case code == 1:
		return "Mostly clear", "🌤"
	case code == 2:
		return "Partly cloudy", "⛅"
	case code == 3:
		return "Overcast", "☁️"
	case code == 45 || code == 48:
		return "Fog", "🌫"
	case code >= 51 && code <= 57:
		return "Drizzle", "🌦"
	case code >= 61 && code <= 67:
		return "Rain", "🌧"
	case code >= 71 && code <= 77:
		return "Snow", "🌨"
	case code >= 80 && code <= 82:
		return "Rain showers", "🌧"
	case code >= 85 && code <= 86:
		return "Snow showers", "🌨"
	case code == 95:
		return "Thunderstorm", "⛈"
	case code == 96 || code == 99:
		return "Thunderstorm w/ hail", "⛈"
	}
	return "Unknown", "❓"
}
