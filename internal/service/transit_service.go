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

// TransitService provides public-transit directions — something OSRM's
// demo server can't do. Uses the Google Maps Directions API when
// GOOGLE_DIRECTIONS_KEY is set; otherwise falls back gracefully to the
// OSRM driving route (better than nothing).
type TransitService struct {
	apiKey string
	client *http.Client
	osrm   *RoutingService // driving fallback
}

var ErrTransitNotConfigured = errors.New("transit: GOOGLE_DIRECTIONS_KEY not set")

func NewTransitService(apiKey string, osrm *RoutingService) *TransitService {
	return &TransitService{
		apiKey: apiKey,
		client: &http.Client{Timeout: 12 * time.Second},
		osrm:   osrm,
	}
}

func (s *TransitService) Configured() bool { return s.apiKey != "" }

// TransitRoute is a narrow view of Google Directions' response. One overall
// duration/distance plus per-step instructions and the transit agency /
// line name so the UI can render bus/metro badges.
type TransitRoute struct {
	Mode         string        `json:"mode"` // transit / driving (fallback)
	DurationS    int           `json:"duration_s"`
	DurationText string        `json:"duration_text"`
	DistanceM    int           `json:"distance_m"`
	Summary      string        `json:"summary,omitempty"`
	Steps        []TransitStep `json:"steps,omitempty"`
	PolylineEnc  string        `json:"polyline,omitempty"` // Google's encoded polyline
}

// TransitStep is a single leg: "Walk 4 min to station", "Metro line 2 for
// 6 stops", etc.
type TransitStep struct {
	Mode          string `json:"mode"` // WALKING, TRANSIT, DRIVING
	Instructions  string `json:"instructions"`
	DurationS     int    `json:"duration_s"`
	DistanceM     int    `json:"distance_m"`
	TransitLine   string `json:"transit_line,omitempty"`
	TransitAgency string `json:"transit_agency,omitempty"`
	DepartureStop string `json:"departure_stop,omitempty"`
	ArrivalStop   string `json:"arrival_stop,omitempty"`
	NumStops      int    `json:"num_stops,omitempty"`
}

// Route returns transit directions from A to B. `departure` is optional
// (Google uses now when zero).
func (s *TransitService) Route(ctx context.Context, fromLat, fromLng, toLat, toLng float64, departure time.Time) (*TransitRoute, error) {
	if !s.Configured() {
		// Fall back to OSRM driving so the UI still gets *something*. We
		// surface a `mode="driving"` marker so it can show a disclaimer.
		r, err := s.osrm.Directions(ctx, "driving", fromLat, fromLng, toLat, toLng)
		if err != nil {
			return nil, err
		}
		return &TransitRoute{
			Mode: "driving", DurationS: int(r.DurationS), DurationText: r.DurationHuman,
			DistanceM: int(r.DistanceM), Summary: "Transit not configured — showing driving route",
		}, nil
	}

	q := url.Values{}
	q.Set("origin", fmt.Sprintf("%f,%f", fromLat, fromLng))
	q.Set("destination", fmt.Sprintf("%f,%f", toLat, toLng))
	q.Set("mode", "transit")
	q.Set("key", s.apiKey)
	if !departure.IsZero() {
		q.Set("departure_time", fmt.Sprintf("%d", departure.Unix()))
	}

	u := "https://maps.googleapis.com/maps/api/directions/json?" + q.Encode()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google directions: status %d", resp.StatusCode)
	}

	var raw struct {
		Status string `json:"status"`
		Routes []struct {
			Summary          string `json:"summary"`
			OverviewPolyline struct {
				Points string `json:"points"`
			} `json:"overview_polyline"`
			Legs []struct {
				Duration struct {
					Value int    `json:"value"`
					Text  string `json:"text"`
				} `json:"duration"`
				Distance struct {
					Value int `json:"value"`
				} `json:"distance"`
				Steps []struct {
					TravelMode       string              `json:"travel_mode"`
					HTMLInstructions string              `json:"html_instructions"`
					Duration         struct{ Value int } `json:"duration"`
					Distance         struct{ Value int } `json:"distance"`
					TransitDetails   *struct {
						Line struct {
							Name      string `json:"name"`
							ShortName string `json:"short_name"`
							Agencies  []struct {
								Name string `json:"name"`
							} `json:"agencies"`
						} `json:"line"`
						DepartureStop struct{ Name string } `json:"departure_stop"`
						ArrivalStop   struct{ Name string } `json:"arrival_stop"`
						NumStops      int                   `json:"num_stops"`
					} `json:"transit_details,omitempty"`
				} `json:"steps"`
			} `json:"legs"`
		} `json:"routes"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	if raw.Status != "OK" || len(raw.Routes) == 0 {
		return nil, fmt.Errorf("google directions returned %q", raw.Status)
	}
	r := raw.Routes[0]
	if len(r.Legs) == 0 {
		return nil, errors.New("no legs in route")
	}
	leg := r.Legs[0]

	out := &TransitRoute{
		Mode: "transit", Summary: r.Summary, PolylineEnc: r.OverviewPolyline.Points,
		DurationS: leg.Duration.Value, DurationText: leg.Duration.Text,
		DistanceM: leg.Distance.Value,
	}
	for _, st := range leg.Steps {
		step := TransitStep{
			Mode: st.TravelMode, Instructions: stripHTML(st.HTMLInstructions),
			DurationS: st.Duration.Value, DistanceM: st.Distance.Value,
		}
		if st.TransitDetails != nil {
			line := st.TransitDetails.Line
			step.TransitLine = firstNonEmpty(line.ShortName, line.Name)
			if len(line.Agencies) > 0 {
				step.TransitAgency = line.Agencies[0].Name
			}
			step.DepartureStop = st.TransitDetails.DepartureStop.Name
			step.ArrivalStop = st.TransitDetails.ArrivalStop.Name
			step.NumStops = st.TransitDetails.NumStops
		}
		out.Steps = append(out.Steps, step)
	}
	return out, nil
}

// stripHTML removes the HTML tags Google inlines in instructions like
// `Turn <b>left</b> onto <span>High St</span>`.
func stripHTML(s string) string {
	var b []byte
	in := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '<':
			in = true
		case c == '>':
			in = false
		case !in:
			b = append(b, c)
		}
	}
	return string(b)
}
