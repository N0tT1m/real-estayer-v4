package amadeus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
)

// --- City Search ---

type citySearchResponse struct {
	Data []cityData `json:"data"`
}

type cityData struct {
	Name    string `json:"name"`
	IataCode string `json:"iataCode"`
	Address struct {
		CityName    string `json:"cityName"`
		CountryCode string `json:"countryCode"`
		CountryName string `json:"countryName"`
	} `json:"address"`
	GeoCode struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	} `json:"geoCode"`
}

// CityResult holds the essential data returned from a city search.
type CityResult struct {
	Name        string
	CountryCode string
	CountryName string
	IataCode    string
	Latitude    float64
	Longitude   float64
}

// SearchCity returns the best-matching city for the given keyword.
func (c *Client) SearchCity(ctx context.Context, keyword string) (*CityResult, error) {
	params := url.Values{}
	params.Set("subType", "CITY")
	params.Set("keyword", keyword)
	params.Set("page[limit]", "1")

	resp, err := c.get(ctx, "/v1/reference-data/locations?"+params.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var result citySearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode city search response: %w", err)
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("city not found: %s", keyword)
	}

	d := result.Data[0]
	name := d.Address.CityName
	if name == "" {
		name = d.Name
	}

	return &CityResult{
		Name:        name,
		CountryCode: d.Address.CountryCode,
		CountryName: d.Address.CountryName,
		IataCode:    d.IataCode,
		Latitude:    d.GeoCode.Latitude,
		Longitude:   d.GeoCode.Longitude,
	}, nil
}

// --- Points of Interest ---

type poiResponse struct {
	Data []poiData `json:"data"`
}

type poiData struct {
	Name     string   `json:"name"`
	Category string   `json:"category"`
	Rank     int      `json:"rank"`
	Tags     []string `json:"tags"`
}

// POI is a point of interest near a destination.
type POI struct {
	Name     string
	Category string
	Tags     []string
}

// GetPointsOfInterest returns top POIs near the given coordinates.
func (c *Client) GetPointsOfInterest(ctx context.Context, latitude, longitude float64) ([]POI, error) {
	params := url.Values{}
	params.Set("latitude", fmt.Sprintf("%.6f", latitude))
	params.Set("longitude", fmt.Sprintf("%.6f", longitude))
	params.Set("radius", "20")
	params.Set("page[limit]", "10")

	resp, err := c.get(ctx, "/v1/reference-data/locations/pois?"+params.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var result poiResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode POI response: %w", err)
	}

	pois := make([]POI, 0, len(result.Data))
	for _, d := range result.Data {
		pois = append(pois, POI{
			Name:     d.Name,
			Category: d.Category,
			Tags:     d.Tags,
		})
	}
	return pois, nil
}
