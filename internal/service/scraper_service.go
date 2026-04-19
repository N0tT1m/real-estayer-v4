package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

// ScraperService handles communication with the Rust scraper
type ScraperService struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client
}

// NewScraperService creates a new scraper service. apiKey is sent on every
// outbound request so the scraper can reject unauthenticated callers.
func NewScraperService(baseURL, apiKey string) *ScraperService {
	return &ScraperService{
		baseURL: baseURL,
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: 5 * time.Minute,
		},
	}
}

// ScrapingStatus represents the current scraper status
type ScrapingStatus struct {
	Status        string                 `json:"status"`
	Message       string                 `json:"message"`
	LastScrape    *time.Time             `json:"last_scrape,omitempty"`
	TotalListings int                    `json:"total_listings"`
	Details       map[string]interface{} `json:"details,omitempty"`
}

// ScrapingResult represents the result of a scraping operation
type ScrapingResult struct {
	Success        bool     `json:"success"`
	Message        string   `json:"message"`
	TotalListings  int      `json:"total_listings"`
	CanadaListings int      `json:"canada_listings"`
	USListings     int      `json:"us_listings"`
	InsertedIDs    []string `json:"inserted_ids,omitempty"`
}

// ScrapeParams holds parameters for a scrape request
type ScrapeParams struct {
	City       string
	State      string
	Country    string
	CheckIn    string
	CheckOut   string
	Adults     int
	Children   int
	Infants    int
	Pets       int
	Limit      int
	BadgesOnly bool
	HotTub     bool
	Pool       bool
	Waterfront bool
}

func (s *ScraperService) newRequest(ctx context.Context, method, path string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.baseURL+path, body)
	if err != nil {
		return nil, err
	}
	if s.apiKey != "" {
		req.Header.Set("X-API-Key", s.apiKey)
	}
	return req, nil
}

func (s *ScraperService) do(req *http.Request) (*http.Response, error) {
	return s.httpClient.Do(req)
}

// GetStatus returns the current status of the scraper
func (s *ScraperService) GetStatus(ctx context.Context) (*ScrapingStatus, error) {
	req, err := s.newRequest(ctx, "GET", "/health", nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.do(req)
	if err != nil {
		return &ScrapingStatus{
			Status:  "offline",
			Message: "Scraper is not reachable: " + err.Error(),
		}, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &ScrapingStatus{
			Status:  "error",
			Message: fmt.Sprintf("Scraper returned status %d", resp.StatusCode),
		}, nil
	}

	var health map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&health); err != nil {
		return &ScrapingStatus{
			Status:  "online",
			Message: "Scraper is online",
		}, nil
	}

	return &ScrapingStatus{
		Status:  "online",
		Message: "Scraper is online and ready",
		Details: health,
	}, nil
}

// ScrapeCity triggers a scrape for a specific city
func (s *ScraperService) ScrapeCity(ctx context.Context, p ScrapeParams) (*ScrapingResult, error) {
	params := url.Values{}
	params.Set("city", p.City)
	if p.State != "" {
		params.Set("state", p.State)
	}
	if p.Country != "" {
		params.Set("country", p.Country)
	}
	if p.CheckIn != "" {
		params.Set("check_in", p.CheckIn)
	}
	if p.CheckOut != "" {
		params.Set("check_out", p.CheckOut)
	}
	if p.Adults > 0 {
		params.Set("adults", fmt.Sprintf("%d", p.Adults))
	}
	if p.Children > 0 {
		params.Set("children", fmt.Sprintf("%d", p.Children))
	}
	if p.Infants > 0 {
		params.Set("infants", fmt.Sprintf("%d", p.Infants))
	}
	if p.Pets > 0 {
		params.Set("pets", fmt.Sprintf("%d", p.Pets))
	}
	params.Set("limit", fmt.Sprintf("%d", p.Limit))
	if p.BadgesOnly {
		params.Set("badges_only", "true")
	}
	if p.HotTub {
		params.Set("hot_tub", "true")
	}
	if p.Pool {
		params.Set("pool", "true")
	}
	if p.Waterfront {
		params.Set("waterfront", "true")
	}

	req, err := s.newRequest(ctx, "GET", "/scrape-city-data?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach scraper: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("scraper returned error: %s", string(body))
	}

	var result ScrapingResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse scraper response: %w", err)
	}

	result.Success = true
	return &result, nil
}

// ScrapeNorthAmerica triggers a full North America scrape
func (s *ScraperService) ScrapeNorthAmerica(ctx context.Context) (*ScrapingResult, error) {
	req, err := s.newRequest(ctx, "GET", "/scrape-north-america", nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to reach scraper: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("scraper returned error: %s", string(body))
	}

	var result ScrapingResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to parse scraper response: %w", err)
	}

	result.Success = true
	return &result, nil
}

// GetListings retrieves listings from the scraper (for direct access)
func (s *ScraperService) GetListings(ctx context.Context, location string, limit int) ([]map[string]interface{}, error) {
	endpoint := fmt.Sprintf("/get-listings/%s", url.PathEscape(location))
	if limit > 0 {
		endpoint = fmt.Sprintf("%s/%d", endpoint, limit)
	}

	req, err := s.newRequest(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}

	resp, err := s.do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get listings: status %d", resp.StatusCode)
	}

	var listings []map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&listings); err != nil {
		return nil, err
	}

	return listings, nil
}

// TestEmail tests the email notification system
func (s *ScraperService) TestEmail(ctx context.Context) error {
	req, err := s.newRequest(ctx, "GET", "/test-email", nil)
	if err != nil {
		return err
	}

	resp, err := s.do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("email test failed: status %d", resp.StatusCode)
	}

	return nil
}
