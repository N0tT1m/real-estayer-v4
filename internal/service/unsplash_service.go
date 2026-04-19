package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// UnsplashService fetches a hero image per destination. Uses the
// `/search/photos` endpoint with `query` + `orientation=landscape`. When
// UNSPLASH_ACCESS_KEY isn't set the service returns nil so templates can
// fall back gracefully.
//
// Attribution rules require crediting the photographer and linking to their
// Unsplash profile + the image — we return both so the UI can honor that.
type UnsplashService struct {
	key    string
	client *http.Client
	mu     sync.RWMutex
	cache  map[string]unsplashCacheEntry
}

type unsplashCacheEntry struct {
	value     *UnsplashPhoto
	expiresAt time.Time
}

// UnsplashPhoto is a trimmed photo record.
type UnsplashPhoto struct {
	URL         string `json:"url"`          // high-res
	ThumbURL    string `json:"thumb_url"`
	AltText     string `json:"alt_text,omitempty"`
	Photographer string `json:"photographer,omitempty"`
	ProfileURL  string `json:"profile_url,omitempty"`
	PageURL     string `json:"page_url,omitempty"`
}

func NewUnsplashService(key string) *UnsplashService {
	return &UnsplashService{key: key, client: &http.Client{Timeout: 6 * time.Second}, cache: map[string]unsplashCacheEntry{}}
}

func (s *UnsplashService) Configured() bool { return s.key != "" }

// ForQuery returns a single photo (the top search hit) for the given text.
// Cached 7 days — the query is usually a stable destination name.
func (s *UnsplashService) ForQuery(ctx context.Context, q string) (*UnsplashPhoto, error) {
	if !s.Configured() {
		return nil, nil
	}
	key := q
	s.mu.RLock()
	if v, ok := s.cache[key]; ok && time.Now().Before(v.expiresAt) {
		s.mu.RUnlock()
		return v.value, nil
	}
	s.mu.RUnlock()

	u := fmt.Sprintf("https://api.unsplash.com/search/photos?orientation=landscape&per_page=1&query=%s", url.QueryEscape(q))
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("Accept-Version", "v1")
	req.Header.Set("Authorization", "Client-ID "+s.key)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unsplash: status %d", resp.StatusCode)
	}

	var raw struct {
		Results []struct {
			Urls struct {
				Regular string `json:"regular"`
				Thumb   string `json:"thumb"`
			} `json:"urls"`
			AltDescription string `json:"alt_description"`
			Links          struct {
				HTML string `json:"html"`
			} `json:"links"`
			User struct {
				Name  string `json:"name"`
				Links struct {
					HTML string `json:"html"`
				} `json:"links"`
			} `json:"user"`
		} `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	if len(raw.Results) == 0 {
		s.mu.Lock()
		s.cache[key] = unsplashCacheEntry{value: nil, expiresAt: time.Now().Add(24 * time.Hour)}
		s.mu.Unlock()
		return nil, nil
	}
	r := raw.Results[0]
	photo := &UnsplashPhoto{
		URL: r.Urls.Regular, ThumbURL: r.Urls.Thumb, AltText: r.AltDescription,
		Photographer: r.User.Name, ProfileURL: r.User.Links.HTML, PageURL: r.Links.HTML,
	}
	s.mu.Lock()
	s.cache[key] = unsplashCacheEntry{value: photo, expiresAt: time.Now().Add(7 * 24 * time.Hour)}
	s.mu.Unlock()
	return photo, nil
}
