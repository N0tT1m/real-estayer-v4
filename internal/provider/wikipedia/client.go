package wikipedia

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"time"
)

const baseURL = "https://en.wikipedia.org/api/rest_v1"

var thumbWidthRe = regexp.MustCompile(`/\d+px-`)

type Client struct {
	http *http.Client
}

func NewClient() *Client {
	return &Client{
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

// CityInfo holds the data Wikipedia returns for a city page.
type CityInfo struct {
	Description string
	ImageURL    string
}

type summaryResponse struct {
	Extract   string `json:"extract"`
	Thumbnail *struct {
		Source string `json:"source"`
	} `json:"thumbnail"`
}

// GetCitySummary fetches a description and image for the given city from Wikipedia.
func (c *Client) GetCitySummary(ctx context.Context, cityName string) (*CityInfo, error) {
	escaped := url.PathEscape(cityName)
	req, err := http.NewRequestWithContext(ctx, "GET", baseURL+"/page/summary/"+escaped, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "real-estayer/1.0 (travel app)")
	req.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wikipedia request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("wikipedia page not found: %s", cityName)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("wikipedia returned status %d", resp.StatusCode)
	}

	var s summaryResponse
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return nil, fmt.Errorf("failed to decode wikipedia response: %w", err)
	}

	info := &CityInfo{
		Description: s.Extract,
	}
	if s.Thumbnail != nil {
		// Replace the thumbnail width with 1200px for a high-res version.
		info.ImageURL = thumbWidthRe.ReplaceAllString(s.Thumbnail.Source, "/1200px-")
	}
	return info, nil
}
