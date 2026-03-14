package amadeus

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// Client is the Amadeus API client
type Client struct {
	clientID     string
	clientSecret string
	baseURL      string
	httpClient   *http.Client

	mu          sync.RWMutex
	accessToken string
	tokenExpiry time.Time
}

// NewClient creates a new Amadeus API client
func NewClient(clientID, clientSecret, baseURL string) *Client {
	if baseURL == "" {
		baseURL = "https://test.api.amadeus.com"
	}

	return &Client{
		clientID:     clientID,
		clientSecret: clientSecret,
		baseURL:      baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Name returns the provider name
func (c *Client) Name() string {
	return "amadeus"
}

// tokenResponse represents OAuth token response
type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

// getAccessToken retrieves or refreshes the OAuth token
func (c *Client) getAccessToken(ctx context.Context) (string, error) {
	c.mu.RLock()
	if c.accessToken != "" && time.Now().Before(c.tokenExpiry) {
		token := c.accessToken
		c.mu.RUnlock()
		return token, nil
	}
	c.mu.RUnlock()

	c.mu.Lock()
	defer c.mu.Unlock()

	// Double-check after acquiring write lock
	if c.accessToken != "" && time.Now().Before(c.tokenExpiry) {
		return c.accessToken, nil
	}

	// Request new token
	data := url.Values{}
	data.Set("grant_type", "client_credentials")
	data.Set("client_id", c.clientID)
	data.Set("client_secret", c.clientSecret)

	req, err := http.NewRequestWithContext(ctx, "POST", c.baseURL+"/v1/security/oauth2/token", bytes.NewBufferString(data.Encode()))
	if err != nil {
		return "", fmt.Errorf("failed to create token request: %w", err)
	}

	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("token request failed with status %d: %s", resp.StatusCode, string(body))
	}

	var tokenResp tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return "", fmt.Errorf("failed to decode token response: %w", err)
	}

	c.accessToken = tokenResp.AccessToken
	// Set expiry slightly before actual to avoid edge cases
	c.tokenExpiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn-60) * time.Second)

	return c.accessToken, nil
}

// doRequest performs an authenticated API request
func (c *Client) doRequest(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	token, err := c.getAccessToken(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get access token: %w", err)
	}

	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewBuffer(jsonBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}

	return resp, nil
}

// get performs a GET request
func (c *Client) get(ctx context.Context, path string) (*http.Response, error) {
	return c.doRequest(ctx, "GET", path, nil)
}

// post performs a POST request
func (c *Client) post(ctx context.Context, path string, body interface{}) (*http.Response, error) {
	return c.doRequest(ctx, "POST", path, body)
}

// APIError represents an Amadeus API error
type APIError struct {
	StatusCode int
	Errors     []struct {
		Code   int    `json:"code"`
		Title  string `json:"title"`
		Detail string `json:"detail"`
	} `json:"errors"`
}

func (e *APIError) Error() string {
	if len(e.Errors) > 0 {
		return fmt.Sprintf("amadeus API error %d: %s - %s", e.Errors[0].Code, e.Errors[0].Title, e.Errors[0].Detail)
	}
	return fmt.Sprintf("amadeus API error with status %d", e.StatusCode)
}

// parseError parses an error response
func parseError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)

	var apiErr APIError
	apiErr.StatusCode = resp.StatusCode

	if err := json.Unmarshal(body, &apiErr); err != nil {
		return fmt.Errorf("API error with status %d: %s", resp.StatusCode, string(body))
	}

	return &apiErr
}
