package wikipedia

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

// defaultBaseURL is Wikimedia's REST endpoint. Client.baseURL defaults to it;
// tests point a client at an httptest server instead.
const defaultBaseURL = "https://en.wikipedia.org/api/rest_v1"

// userAgent must include contact info per Wikimedia's robot policy
// (https://meta.wikimedia.org/wiki/User-Agent_policy). Override in prod by
// setting a real contact URL / email in a build-time variable if needed.
const userAgent = "real-estayer/1.0 (https://github.com/realestayer/v4; ops@realestayer.local)"

type Client struct {
	http    *http.Client
	limiter *rate.Limiter
	baseURL string
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
		baseURL: defaultBaseURL,
	}
}

// newClientForTest returns a client pointed at an arbitrary base URL with the
// rate limiter opened up, so tests don't pay the production throttle.
func newClientForTest(baseURL string) *Client {
	return &Client{
		http:    &http.Client{Timeout: 5 * time.Second},
		limiter: rate.NewLimiter(rate.Inf, 1),
		baseURL: baseURL,
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

// isDisambiguation trusts the REST API's own page type, falling back to the
// stock lead-sentence phrasing. The phrasing check is not redundant: pages
// that list places without carrying the disambiguation template (Queenstown's
// "is the name of several human settlements around the world") are typed
// "standard" yet are just as useless as a description.
func isDisambiguation(s summaryResponse) bool {
	if s.Type == "disambiguation" {
		return true
	}
	lead := strings.ToLower(s.Extract)
	if len(lead) > 200 {
		lead = lead[:200]
	}
	for _, phrase := range []string{
		"may refer to", "most often refers to", "can refer to",
		"is the name of several", "usually refers to",
	} {
		if strings.Contains(lead, phrase) {
			return true
		}
	}
	return false
}

// CityInfo holds the data Wikipedia returns for a city page.
type CityInfo struct {
	Description string
	ImageURL    string
}

// ErrDisambiguation means the title matched Wikipedia's "X may refer to:"
// index page rather than an article. The extract on such a page is a list of
// other places, so storing it as a city description is always wrong — a bare
// "Cartagena" or "Queenstown" lands here. Callers should retry with a
// qualified title such as "Cartagena, Colombia".
var ErrDisambiguation = errors.New("wikipedia: title is a disambiguation page")

type summaryResponse struct {
	// Type is "standard", "disambiguation", "no-extract", ...
	Type      string `json:"type"`
	Extract   string `json:"extract"`
	Thumbnail *struct {
		Source string `json:"source"`
		Width  int    `json:"width"`
	} `json:"thumbnail"`
	OriginalImage *struct {
		Source string `json:"source"`
		Width  int    `json:"width"`
	} `json:"originalimage"`
}

// GetCitySummary fetches a description and image for the given city from Wikipedia.
func (c *Client) GetCitySummary(ctx context.Context, cityName string) (*CityInfo, error) {
	escaped := url.PathEscape(cityName)
	req, err := http.NewRequestWithContext(ctx, "GET", c.baseURL+"/page/summary/"+escaped, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.doWithRetry(req)
	if err != nil {
		return nil, fmt.Errorf("wikipedia request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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

	if isDisambiguation(s) {
		return nil, fmt.Errorf("%w: %s", ErrDisambiguation, cityName)
	}

	info := &CityInfo{
		Description: s.Extract,
	}
	if s.Thumbnail != nil {
		// Use the thumbnail Wikipedia picked, verbatim. Rewriting the width
		// (e.g. forcing 640px or 1200px) hammers the on-demand resize
		// service, which 429s aggressively on any width the article page
		// doesn't already render. The REST summary's chosen width is the
		// one guaranteed to be hot-cached.
		info.ImageURL = s.Thumbnail.Source
	}
	return info, nil
}
