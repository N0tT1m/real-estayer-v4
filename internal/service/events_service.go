package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

// EventsService queries Ticketmaster's Discovery API. A free consumer key is
// required; without one the service reports ErrNotConfigured so UI can hide
// the section gracefully.
//
// We only use one endpoint (events.json) and only surface the fields used by
// the UI. No booking — tickets link out to Ticketmaster.
type EventsService struct {
	apiKey string
	client *http.Client
}

var ErrEventsNotConfigured = errors.New("events: TICKETMASTER_API_KEY not set")

func NewEventsService(apiKey string) *EventsService {
	return &EventsService{
		apiKey: apiKey,
		client: &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *EventsService) Configured() bool { return s.apiKey != "" }

// Event is a narrow view-model for the events rail / strip.
type Event struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	URL      string    `json:"url"`
	ImageURL string    `json:"image_url,omitempty"`
	Start    time.Time `json:"start"`
	Venue    string    `json:"venue,omitempty"`
	City     string    `json:"city,omitempty"`
	Category string    `json:"category,omitempty"`
}

// Nearby returns events near a coordinate within an optional date window.
func (s *EventsService) Nearby(ctx context.Context, lat, lng float64, radiusKm int, start, end time.Time, limit int) ([]Event, error) {
	if !s.Configured() {
		return nil, ErrEventsNotConfigured
	}
	if radiusKm <= 0 {
		radiusKm = 30
	}
	if limit <= 0 {
		limit = 20
	}

	q := url.Values{}
	q.Set("apikey", s.apiKey)
	q.Set("latlong", fmt.Sprintf("%f,%f", lat, lng))
	q.Set("radius", fmt.Sprintf("%d", radiusKm))
	q.Set("unit", "km")
	q.Set("size", fmt.Sprintf("%d", limit))
	q.Set("sort", "date,asc")
	if !start.IsZero() {
		q.Set("startDateTime", start.UTC().Format("2006-01-02T15:04:05Z"))
	}
	if !end.IsZero() {
		q.Set("endDateTime", end.UTC().Format("2006-01-02T15:04:05Z"))
	}
	endpoint := "https://app.ticketmaster.com/discovery/v2/events.json?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ticketmaster: status %d", resp.StatusCode)
	}

	var raw struct {
		Embedded struct {
			Events []struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				URL    string `json:"url"`
				Images []struct {
					URL   string `json:"url"`
					Ratio string `json:"ratio"`
					Width int    `json:"width"`
				} `json:"images"`
				Dates struct {
					Start struct {
						DateTime string `json:"dateTime"`
						LocalDate string `json:"localDate"`
					} `json:"start"`
				} `json:"dates"`
				Classifications []struct {
					Segment struct {
						Name string `json:"name"`
					} `json:"segment"`
				} `json:"classifications"`
				Embedded struct {
					Venues []struct {
						Name string `json:"name"`
						City struct {
							Name string `json:"name"`
						} `json:"city"`
					} `json:"venues"`
				} `json:"_embedded"`
			} `json:"events"`
		} `json:"_embedded"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	out := make([]Event, 0, len(raw.Embedded.Events))
	for _, e := range raw.Embedded.Events {
		ev := Event{ID: e.ID, Name: e.Name, URL: e.URL}
		if t, err := time.Parse(time.RFC3339, e.Dates.Start.DateTime); err == nil {
			ev.Start = t
		} else if t, err := time.Parse("2006-01-02", e.Dates.Start.LocalDate); err == nil {
			ev.Start = t
		}
		if len(e.Embedded.Venues) > 0 {
			ev.Venue = e.Embedded.Venues[0].Name
			ev.City = e.Embedded.Venues[0].City.Name
		}
		if len(e.Classifications) > 0 {
			ev.Category = e.Classifications[0].Segment.Name
		}
		// Prefer a 16:9 image.
		var best string
		for _, img := range e.Images {
			if img.Ratio == "16_9" && img.Width >= 640 {
				best = img.URL
				break
			}
			if best == "" {
				best = img.URL
			}
		}
		ev.ImageURL = best
		out = append(out, ev)
	}
	return out, nil
}
