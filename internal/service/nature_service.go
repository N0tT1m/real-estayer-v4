package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// NatureService bundles two complementary nature datasets:
//
//   iNaturalist — free, keyless; any taxon, crowd-sourced observations.
//   eBird       — bird sightings; free with an API key signup.
//
// Both are read-only for our purposes. We expose a single `NearbyObservations`
// call per source and return a unified `Sighting` shape so the UI can mix the
// results.
type NatureService struct {
	client  *http.Client
	eBirdKey string
}

var ErrEBirdNotConfigured = errors.New("ebird: EBIRD_API_KEY not set")

func NewNatureService(eBirdKey string) *NatureService {
	return &NatureService{
		client:   &http.Client{Timeout: 10 * time.Second},
		eBirdKey: eBirdKey,
	}
}

// Sighting is the unified record rendered by the UI.
type Sighting struct {
	Source       string    `json:"source"`      // "iNaturalist" | "eBird"
	CommonName   string    `json:"common_name"`
	ScientificName string  `json:"scientific_name,omitempty"`
	Lat          float64   `json:"lat,omitempty"`
	Lng          float64   `json:"lng,omitempty"`
	ObservedAt   time.Time `json:"observed_at"`
	PhotoURL     string    `json:"photo_url,omitempty"`
	URL          string    `json:"url,omitempty"`
	Location     string    `json:"location,omitempty"`
	Count        int       `json:"count,omitempty"`
}

// INatNearby returns recent verifiable research-grade observations within
// `radiusKm`.
func (s *NatureService) INatNearby(ctx context.Context, lat, lng float64, radiusKm int, limit int) ([]Sighting, error) {
	if radiusKm <= 0 {
		radiusKm = 20
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	u := fmt.Sprintf(
		"https://api.inaturalist.org/v1/observations?verifiable=true&quality_grade=research&per_page=%d&order=desc&order_by=observed_on&lat=%f&lng=%f&radius=%d&photos=true",
		limit, lat, lng, radiusKm,
	)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("User-Agent", "Real-Estayer/1.0 (https://real-estayer.app)")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("inaturalist: status %d", resp.StatusCode)
	}
	var raw struct {
		Results []struct {
			ID             int64  `json:"id"`
			ObservedOn     string `json:"observed_on"`
			URI            string `json:"uri"`
			PlaceGuess     string `json:"place_guess"`
			Geojson        struct {
				Coordinates []float64 `json:"coordinates"`
			} `json:"geojson"`
			Photos []struct {
				URL string `json:"url"`
			} `json:"photos"`
			Taxon struct {
				Name         string `json:"name"`
				PreferredCommonName string `json:"preferred_common_name"`
			} `json:"taxon"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]Sighting, 0, len(raw.Results))
	for _, r := range raw.Results {
		s := Sighting{
			Source:     "iNaturalist",
			CommonName: firstNonEmpty(r.Taxon.PreferredCommonName, r.Taxon.Name),
			ScientificName: r.Taxon.Name,
			Location:   r.PlaceGuess,
			URL:        r.URI,
		}
		if len(r.Geojson.Coordinates) == 2 {
			// iNat returns [lng, lat].
			s.Lng = r.Geojson.Coordinates[0]
			s.Lat = r.Geojson.Coordinates[1]
		}
		if t, err := time.Parse("2006-01-02", r.ObservedOn); err == nil {
			s.ObservedAt = t
		}
		if len(r.Photos) > 0 {
			// Medium size — replace "/square.jpg" → "/medium.jpg".
			s.PhotoURL = strings.Replace(r.Photos[0].URL, "/square.", "/medium.", 1)
		}
		out = append(out, s)
	}
	return out, nil
}

// EBirdRecent returns recent bird observations within radius (km). Requires
// EBIRD_API_KEY.
func (s *NatureService) EBirdRecent(ctx context.Context, lat, lng float64, radiusKm int, limit int) ([]Sighting, error) {
	if s.eBirdKey == "" {
		return nil, ErrEBirdNotConfigured
	}
	if radiusKm <= 0 {
		radiusKm = 25
	}
	if limit <= 0 || limit > 100 {
		limit = 30
	}
	u := fmt.Sprintf("https://api.ebird.org/v2/data/obs/geo/recent?lat=%f&lng=%f&dist=%d&maxResults=%d", lat, lng, radiusKm, limit)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("X-eBirdApiToken", s.eBirdKey)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ebird: status %d", resp.StatusCode)
	}
	var raw []struct {
		SpeciesCode string  `json:"speciesCode"`
		ComName     string  `json:"comName"`
		SciName     string  `json:"sciName"`
		ObsDt       string  `json:"obsDt"`
		HowMany     int     `json:"howMany"`
		Lat         float64 `json:"lat"`
		Lng         float64 `json:"lng"`
		LocName     string  `json:"locName"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]Sighting, 0, len(raw))
	for _, r := range raw {
		sighting := Sighting{
			Source:         "eBird",
			CommonName:     r.ComName,
			ScientificName: r.SciName,
			Lat:            r.Lat,
			Lng:            r.Lng,
			Count:          r.HowMany,
			Location:       r.LocName,
			URL:            fmt.Sprintf("https://ebird.org/species/%s", r.SpeciesCode),
		}
		// ObsDt looks like "2025-05-30 14:32".
		if t, err := time.Parse("2006-01-02 15:04", r.ObsDt); err == nil {
			sighting.ObservedAt = t
		} else if t, err := time.Parse("2006-01-02", r.ObsDt); err == nil {
			sighting.ObservedAt = t
		}
		out = append(out, sighting)
	}
	return out, nil
}
