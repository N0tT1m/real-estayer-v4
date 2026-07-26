package wikipedia

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const pageviewsBaseURL = "https://wikimedia.org/api/rest_v1/metrics/pageviews/per-article/en.wikipedia.org/all-access/all-agents"

// MonthlyViews returns the total English-Wikipedia pageview count for the
// given article title over the last ~12 months. Used as a popularity signal
// when ranking discovered destinations — the highest-viewed articles in a
// region are almost always the places travellers actually care about.
//
// Returns (0, nil) when the article has no view data (very new or very
// obscure pages). Unmapped status codes (404 in particular) are treated as
// zero rather than errors so a single missing article doesn't tank a batch.
func (c *Client) MonthlyViews(ctx context.Context, articleTitle string) (int64, error) {
	if articleTitle == "" {
		return 0, nil
	}

	end := time.Now().UTC()
	start := end.AddDate(-1, 0, 0)
	startStr := start.Format("2006010100")
	endStr := end.Format("2006010100")

	title := strings.ReplaceAll(articleTitle, " ", "_")
	escaped := url.PathEscape(title)
	reqURL := fmt.Sprintf("%s/%s/monthly/%s/%s", pageviewsBaseURL, escaped, startStr, endStr)

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")

	resp, err := c.doWithRetry(req)
	if err != nil {
		return 0, fmt.Errorf("pageviews request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return 0, nil
	}
	// A persistent 429 after one retry: return zero so the ranker doesn't
	// drop the candidate — the sitelink count tiebreak still ranks it.
	if resp.StatusCode == http.StatusTooManyRequests {
		return 0, nil
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("pageviews returned status %d", resp.StatusCode)
	}

	var out struct {
		Items []struct {
			Views int64 `json:"views"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return 0, fmt.Errorf("failed to decode pageviews response: %w", err)
	}

	var total int64
	for _, it := range out.Items {
		total += it.Views
	}
	return total, nil
}
