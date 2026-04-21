package wikipedia

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"time"

	"golang.org/x/time/rate"
)

const baseURL = "https://en.wikipedia.org/api/rest_v1"

// userAgent must include contact info per Wikimedia's robot policy
// (https://meta.wikimedia.org/wiki/User-Agent_policy). Override in prod by
// setting a real contact URL / email in a build-time variable if needed.
const userAgent = "real-estayer/1.0 (https://github.com/realestayer/v4; ops@realestayer.local)"

var thumbWidthRe = regexp.MustCompile(`/\d+px-`)

type Client struct {
	http    *http.Client
	limiter *rate.Limiter
}

// NewClient builds a Wikipedia/Wikimedia REST client with a shared token-bucket
// limiter (20 req/s, burst 40) — well below the documented 200 req/s ceiling
// but high enough that the destination-discovery fan-out doesn't serialise
// into a multi-minute wait. All calls route through doWithRetry, which retries
// once on 429 honoring Retry-After. One Client should be shared across callers
// so the limiter throttles the whole process, not each goroutine.
func NewClient() *Client {
	return &Client{
		http:    &http.Client{Timeout: 10 * time.Second},
		limiter: rate.NewLimiter(rate.Limit(20), 40),
	}
}

// doWithRetry sends req through the rate limiter and retries once on 429,
// sleeping for Retry-After (if present) or a 2-second default. The caller
// owns the response body.
func (c *Client) doWithRetry(req *http.Request) (*http.Response, error) {
	for attempt := 0; attempt < 2; attempt++ {
		if err := c.limiter.Wait(req.Context()); err != nil {
			return nil, err
		}
		resp, err := c.http.Do(req)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}
		retryAfter := parseRetryAfter(resp.Header.Get("Retry-After"))
		// Drain + close so the connection can be reused for the retry.
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
		if attempt == 1 {
			// Return a synthetic 429 response to the caller — we've burned
			// our one retry.
			return &http.Response{StatusCode: http.StatusTooManyRequests, Header: resp.Header, Body: io.NopCloser(emptyReader{})}, nil
		}
		select {
		case <-time.After(retryAfter):
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
	}
	return nil, fmt.Errorf("unreachable")
}

type emptyReader struct{}

func (emptyReader) Read(p []byte) (int, error) { return 0, io.EOF }

func parseRetryAfter(h string) time.Duration {
	const fallback = 2 * time.Second
	if h == "" {
		return fallback
	}
	if secs, err := strconv.Atoi(h); err == nil && secs > 0 {
		if secs > 30 {
			secs = 30
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(h); err == nil {
		d := time.Until(t)
		if d < 0 {
			return fallback
		}
		if d > 30*time.Second {
			return 30 * time.Second
		}
		return d
	}
	return fallback
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
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.doWithRetry(req)
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
