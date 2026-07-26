package wikipedia

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newTestServer(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return newClientForTest(srv.URL)
}

func TestNewClientUsesDefaultBaseURL(t *testing.T) {
	if got := NewClient().baseURL; got != defaultBaseURL {
		t.Errorf("baseURL = %q, want %q", got, defaultBaseURL)
	}
}

const summaryPayload = `{
  "extract": "Paris is the capital of France.",
  "thumbnail": {"source": "https://img.example/paris-320.jpg", "width": 320},
  "originalimage": {"source": "https://img.example/paris-full.jpg", "width": 4000}
}`

func TestGetCitySummary(t *testing.T) {
	var gotPath, gotUA, gotAccept string
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotUA = r.Header.Get("User-Agent")
		gotAccept = r.Header.Get("Accept")
		_, _ = w.Write([]byte(summaryPayload))
	})

	info, err := c.GetCitySummary(context.Background(), "Paris")
	if err != nil {
		t.Fatalf("GetCitySummary: %v", err)
	}
	if info.Description != "Paris is the capital of France." {
		t.Errorf("Description = %q", info.Description)
	}
	// The thumbnail is used verbatim — rewriting the width causes 429s from
	// the on-demand resize service, so this must not silently change.
	if info.ImageURL != "https://img.example/paris-320.jpg" {
		t.Errorf("ImageURL = %q, want the thumbnail source verbatim", info.ImageURL)
	}
	if gotPath != "/page/summary/Paris" {
		t.Errorf("path = %q", gotPath)
	}
	// Wikimedia's robot policy requires a contact-bearing UA.
	if !strings.Contains(gotUA, "real-estayer") {
		t.Errorf("User-Agent = %q, want the policy-compliant agent", gotUA)
	}
	if gotAccept != "application/json" {
		t.Errorf("Accept = %q", gotAccept)
	}
}

// City names with spaces or slashes must not break out of the path.
func TestGetCitySummaryEscapesTitle(t *testing.T) {
	var gotPath string
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_, _ = w.Write([]byte(`{"extract":"x"}`))
	})
	if _, err := c.GetCitySummary(context.Background(), "San Sebastián/Donostia"); err != nil {
		t.Fatalf("GetCitySummary: %v", err)
	}
	if strings.Contains(strings.TrimPrefix(gotPath, "/page/summary/"), "/") {
		t.Errorf("path %q contains an unescaped slash", gotPath)
	}
}

func TestGetCitySummaryMissingThumbnail(t *testing.T) {
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"extract":"No image here."}`))
	})
	info, err := c.GetCitySummary(context.Background(), "Nowhere")
	if err != nil {
		t.Fatalf("GetCitySummary: %v", err)
	}
	if info.ImageURL != "" {
		t.Errorf("ImageURL = %q, want empty when no thumbnail is present", info.ImageURL)
	}
	if info.Description != "No image here." {
		t.Errorf("Description = %q", info.Description)
	}
}

// A 404 is routine (many seed cities have no exact article) and must be a
// distinguishable error rather than a generic status failure.
func TestGetCitySummaryNotFound(t *testing.T) {
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	_, err := c.GetCitySummary(context.Background(), "Atlantis")
	if err == nil {
		t.Fatal("expected an error for 404")
	}
	if !strings.Contains(err.Error(), "not found") || !strings.Contains(err.Error(), "Atlantis") {
		t.Errorf("err = %v, want a not-found naming the city", err)
	}
}

func TestGetCitySummaryServerError(t *testing.T) {
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	if _, err := c.GetCitySummary(context.Background(), "Paris"); err == nil {
		t.Error("expected an error for 500")
	}
}

func TestGetCitySummaryMalformedJSON(t *testing.T) {
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"extract": `))
	})
	if _, err := c.GetCitySummary(context.Background(), "Paris"); err == nil {
		t.Error("expected a decode error for truncated JSON")
	}
}

// doWithRetry retries once on 429; the second attempt's response is the one
// returned.
func TestRetriesOnce429(t *testing.T) {
	var calls int
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_, _ = w.Write([]byte(summaryPayload))
	})

	info, err := c.GetCitySummary(context.Background(), "Paris")
	if err != nil {
		t.Fatalf("GetCitySummary: %v", err)
	}
	if calls != 2 {
		t.Errorf("upstream calls = %d, want 2 (one retry)", calls)
	}
	if info.Description == "" {
		t.Error("retry succeeded but the payload was not parsed")
	}
}

// Only one retry — a persistently rate-limited endpoint must not loop.
func TestDoesNotRetryTwice(t *testing.T) {
	var calls int
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	if _, err := c.GetCitySummary(context.Background(), "Paris"); err == nil {
		t.Error("expected an error when 429 persists")
	}
	if calls != 2 {
		t.Errorf("upstream calls = %d, want exactly 2", calls)
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := map[string]time.Duration{
		"":             2 * time.Second, // documented default
		"5":            5 * time.Second,
		"not-a-number": 2 * time.Second,
		// Non-positive values carry no useful information, so the client backs
		// off by the default rather than hot-looping.
		"0":  2 * time.Second,
		"-1": 2 * time.Second,
		// Upstream can ask for a very long wait; the client caps it so one
		// unlucky response can't stall a whole discovery fan-out.
		"600": 30 * time.Second,
		"31":  30 * time.Second,
		"30":  30 * time.Second,
	}
	for in, want := range tests {
		if got := parseRetryAfter(in); got != want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", in, got, want)
		}
	}
}

// HTTP-date form is also legal in Retry-After.
func TestParseRetryAfterHTTPDate(t *testing.T) {
	future := time.Now().Add(5 * time.Second).UTC().Format(http.TimeFormat)
	got := parseRetryAfter(future)
	if got <= 0 || got > 30*time.Second {
		t.Errorf("parseRetryAfter(future date) = %v, want a bounded positive wait", got)
	}
	past := time.Now().Add(-time.Hour).UTC().Format(http.TimeFormat)
	if got := parseRetryAfter(past); got != 2*time.Second {
		t.Errorf("parseRetryAfter(past date) = %v, want the 2s fallback", got)
	}
}

func TestContextCancellationIsRespected(t *testing.T) {
	c := newTestServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := c.GetCitySummary(ctx, "Paris"); err == nil {
		t.Error("expected a context deadline error")
	}
}

// A disambiguation page must never be stored as a city description. Three of
// the 97 seeded destinations (San Jose, Cartagena, Queenstown) shipped with
// "X may refer to:" text because the bare title indexes several places.
func TestGetCitySummaryRejectsDisambiguation(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"typed", `{"type":"disambiguation","extract":"Cartagena or Carthagena may refer to:"}`},
		// Queenstown is typed "standard" but is still an index of places.
		{"untyped list", `{"type":"standard","extract":"Queenstown is the name of several human settlements around the world, nearly all in countries that are part of the Commonwealth."}`},
		{"most often refers", `{"type":"standard","extract":"San José or San Jose most often refers to:San Jose, California, United States"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			if _, err := newClientForTest(srv.URL).GetCitySummary(context.Background(), "Whatever"); !errors.Is(err, ErrDisambiguation) {
				t.Fatalf("want ErrDisambiguation, got %v", err)
			}
		})
	}
}

// A real article must still come back cleanly — the phrase check is a prefix
// scan, so an article merely containing "may refer to" later on is unaffected.
func TestGetCitySummaryAcceptsRealArticle(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"type":"standard","extract":"Cartagena, known since the imperial era as Cartagena de Indias, is a city on the Caribbean coast of Colombia.","thumbnail":{"source":"https://example.org/c.jpg","width":320}}`))
	}))
	defer srv.Close()

	info, err := newClientForTest(srv.URL).GetCitySummary(context.Background(), "Cartagena, Colombia")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(info.Description, "Cartagena, known since") {
		t.Errorf("description = %q", info.Description)
	}
	if info.ImageURL != "https://example.org/c.jpg" {
		t.Errorf("image = %q", info.ImageURL)
	}
}
