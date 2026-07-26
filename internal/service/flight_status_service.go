package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// FlightStatusService calls AviationStack's free tier for live flight status.
// Returns ErrFlightStatusNotConfigured when no key is present, so the UI can
// hide the widget silently rather than show scary errors.
//
// AviationStack's free tier is 100 req/mo and HTTP-only (no HTTPS) — we wrap
// that by always using https://api.aviationstack.com, which they serve via
// paid tiers. On free tier users will get a 403; caller should catch that.
type FlightStatusService struct {
	apiKey string
	client *http.Client
}

var ErrFlightStatusNotConfigured = errors.New("flight status: AVIATIONSTACK_API_KEY not set")

func NewFlightStatusService(apiKey string) *FlightStatusService {
	return &FlightStatusService{
		apiKey: apiKey,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *FlightStatusService) Configured() bool { return s.apiKey != "" }

// FlightStatus is a small view-model.
type FlightStatus struct {
	FlightNumber string    `json:"flight_number"`
	Airline      string    `json:"airline,omitempty"`
	Status       string    `json:"status"` // scheduled, active, landed, cancelled, incident, diverted
	DepIATA      string    `json:"departure_iata,omitempty"`
	DepCity      string    `json:"departure_city,omitempty"`
	DepScheduled time.Time `json:"departure_scheduled,omitempty"`
	DepEstimated time.Time `json:"departure_estimated,omitempty"`
	DepActual    time.Time `json:"departure_actual,omitempty"`
	ArrIATA      string    `json:"arrival_iata,omitempty"`
	ArrCity      string    `json:"arrival_city,omitempty"`
	ArrScheduled time.Time `json:"arrival_scheduled,omitempty"`
	ArrEstimated time.Time `json:"arrival_estimated,omitempty"`
	ArrActual    time.Time `json:"arrival_actual,omitempty"`
	DelayMinutes int       `json:"delay_minutes,omitempty"`
}

// Lookup queries by IATA flight number (e.g. "BA283"). Date is YYYY-MM-DD;
// empty means today.
func (s *FlightStatusService) Lookup(ctx context.Context, flightIATA, date string) (*FlightStatus, error) {
	if !s.Configured() {
		return nil, ErrFlightStatusNotConfigured
	}
	flightIATA = strings.ToUpper(strings.TrimSpace(flightIATA))
	if flightIATA == "" {
		return nil, errors.New("flight number required")
	}
	q := url.Values{}
	q.Set("access_key", s.apiKey)
	q.Set("flight_iata", flightIATA)
	if date != "" {
		q.Set("flight_date", date)
	}
	endpoint := "https://api.aviationstack.com/v1/flights?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("aviationstack: status %d", resp.StatusCode)
	}

	var raw struct {
		Data []struct {
			FlightStatus string `json:"flight_status"`
			Airline      struct {
				Name string `json:"name"`
			} `json:"airline"`
			Flight struct {
				IATA string `json:"iata"`
			} `json:"flight"`
			Departure struct {
				IATA      string `json:"iata"`
				Airport   string `json:"airport"`
				Timezone  string `json:"timezone"`
				Scheduled string `json:"scheduled"`
				Estimated string `json:"estimated"`
				Actual    string `json:"actual"`
				Delay     int    `json:"delay"`
			} `json:"departure"`
			Arrival struct {
				IATA      string `json:"iata"`
				Airport   string `json:"airport"`
				Timezone  string `json:"timezone"`
				Scheduled string `json:"scheduled"`
				Estimated string `json:"estimated"`
				Actual    string `json:"actual"`
				Delay     int    `json:"delay"`
			} `json:"arrival"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	if len(raw.Data) == 0 {
		return nil, nil
	}
	d := raw.Data[0]
	fs := &FlightStatus{
		FlightNumber: d.Flight.IATA,
		Airline:      d.Airline.Name,
		Status:       d.FlightStatus,
		DepIATA:      d.Departure.IATA,
		DepCity:      d.Departure.Airport,
		ArrIATA:      d.Arrival.IATA,
		ArrCity:      d.Arrival.Airport,
		DelayMinutes: d.Departure.Delay,
	}
	fs.DepScheduled = parseRFC3339Lax(d.Departure.Scheduled)
	fs.DepEstimated = parseRFC3339Lax(d.Departure.Estimated)
	fs.DepActual = parseRFC3339Lax(d.Departure.Actual)
	fs.ArrScheduled = parseRFC3339Lax(d.Arrival.Scheduled)
	fs.ArrEstimated = parseRFC3339Lax(d.Arrival.Estimated)
	fs.ArrActual = parseRFC3339Lax(d.Arrival.Actual)
	return fs, nil
}

func parseRFC3339Lax(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	// AviationStack occasionally omits timezone.
	if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
		return t
	}
	return time.Time{}
}
