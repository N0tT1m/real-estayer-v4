// Partner-affiliate scaffolds.
//
// Booking.com Demand API and Expedia Partner Solutions (EPS) both require
// contracted API keys that aren't public. The scaffolds below define a
// uniform interface so the rest of the app can call "search hotels" without
// caring which backend is behind it — and then degrades gracefully to the
// deep-link quick-search tiles when no partner is configured.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	ErrBookingNotConfigured = errors.New("booking: no affiliate credentials set")
	ErrExpediaNotConfigured = errors.New("expedia: no affiliate credentials set")
)

// StaySearchRequest is the shared input. Fields not supported by a particular
// backend are ignored.
type StaySearchRequest struct {
	Destination string
	CheckIn     time.Time
	CheckOut    time.Time
	Adults      int
	Children    int
	Currency    string
}

// StayOffer is the unified output — just the data we actually render.
type StayOffer struct {
	Source       string  `json:"source"`
	PropertyName string  `json:"property_name"`
	City         string  `json:"city,omitempty"`
	Rating       float64 `json:"rating,omitempty"`
	ReviewCount  int     `json:"review_count,omitempty"`
	Price        float64 `json:"price"`
	Currency     string  `json:"currency"`
	PricePerNight float64 `json:"price_per_night,omitempty"`
	ThumbURL     string  `json:"thumb_url,omitempty"`
	DeepLink     string  `json:"deep_link"`
}

// ---------- Booking.com ----------

// BookingPartnerService is a stub that shows the shape we'd implement once
// credentials are issued. With DemandAPIKey unset, all calls return
// ErrBookingNotConfigured so the caller can fall back to the deep-link card.
type BookingPartnerService struct {
	AffiliateID string // AID — used in deep-links today
	DemandAPIKey string // populated if/when we're onboarded
	client       *http.Client
}

func NewBookingPartnerService(affiliateID, demandKey string) *BookingPartnerService {
	return &BookingPartnerService{
		AffiliateID:  affiliateID,
		DemandAPIKey: demandKey,
		client:       &http.Client{Timeout: 12 * time.Second},
	}
}

func (s *BookingPartnerService) Configured() bool { return s.DemandAPIKey != "" }

// Search performs a Demand API hotel search. Returns ErrBookingNotConfigured
// when a key isn't present.
func (s *BookingPartnerService) Search(ctx context.Context, req StaySearchRequest) ([]StayOffer, error) {
	if !s.Configured() {
		return nil, ErrBookingNotConfigured
	}
	// NOTE: the real Demand API uses /demand/v3/accommodations/search.
	// We avoid calling an endpoint that will 401 in CI — this is the stub
	// that sends the request shape Booking documents.
	payload := map[string]interface{}{
		"city":      req.Destination,
		"checkin":   req.CheckIn.Format("2006-01-02"),
		"checkout":  req.CheckOut.Format("2006-01-02"),
		"guests":    map[string]int{"adults": req.Adults, "children": req.Children},
		"currency":  req.Currency,
		"affiliate_id": s.AffiliateID,
	}
	body, _ := json.Marshal(payload)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://demandapi.booking.com/v3/accommodations/search", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+s.DemandAPIKey)

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, ErrBookingNotConfigured
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("booking demand: status %d", resp.StatusCode)
	}

	var raw struct {
		Data []struct {
			Hotel struct {
				Name    string `json:"name"`
				City    string `json:"city"`
				Thumb   string `json:"thumbnail_url"`
				Review  struct {
					Score      float64 `json:"score"`
					ScoreCount int     `json:"score_count"`
				} `json:"review"`
			} `json:"hotel"`
			Price struct {
				Amount   float64 `json:"amount"`
				Currency string  `json:"currency"`
				PerNight float64 `json:"amount_per_night"`
			} `json:"price"`
			DeepLink string `json:"deep_link"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]StayOffer, 0, len(raw.Data))
	for _, r := range raw.Data {
		out = append(out, StayOffer{
			Source:        "Booking.com",
			PropertyName:  r.Hotel.Name,
			City:          r.Hotel.City,
			Rating:        r.Hotel.Review.Score,
			ReviewCount:   r.Hotel.Review.ScoreCount,
			Price:         r.Price.Amount,
			Currency:      r.Price.Currency,
			PricePerNight: r.Price.PerNight,
			ThumbURL:      r.Hotel.Thumb,
			DeepLink:      r.DeepLink,
		})
	}
	return out, nil
}

// ---------- Expedia Partner Solutions ----------

type ExpediaPartnerService struct {
	APIKey       string
	SharedSecret string
	client       *http.Client
}

func NewExpediaPartnerService(key, secret string) *ExpediaPartnerService {
	return &ExpediaPartnerService{APIKey: key, SharedSecret: secret, client: &http.Client{Timeout: 15 * time.Second}}
}

func (s *ExpediaPartnerService) Configured() bool { return s.APIKey != "" && s.SharedSecret != "" }

// Search hits the /v3/properties/availability endpoint. Same fail-gracefully
// pattern as Booking.
func (s *ExpediaPartnerService) Search(ctx context.Context, req StaySearchRequest) ([]StayOffer, error) {
	if !s.Configured() {
		return nil, ErrExpediaNotConfigured
	}

	q := url.Values{}
	q.Set("checkin", req.CheckIn.Format("2006-01-02"))
	q.Set("checkout", req.CheckOut.Format("2006-01-02"))
	q.Set("occupancy", fmt.Sprintf("%d-%d", max(req.Adults, 1), req.Children))
	q.Set("currency", orDefault(req.Currency, "USD"))
	q.Set("destination", req.Destination)

	u := "https://api.ean.com/v3/properties/availability?" + q.Encode()
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", expediaSignature(s.APIKey, s.SharedSecret))
	httpReq.Header.Set("Accept", "application/json")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, ErrExpediaNotConfigured
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("expedia: status %d", resp.StatusCode)
	}

	// The real EPS response is deep — we only care about a handful of fields.
	var raw []struct {
		PropertyID string  `json:"property_id"`
		PropertyName string `json:"property_name"`
		City        string  `json:"city"`
		Rating      float64 `json:"guest_rating"`
		Reviews     int     `json:"review_count"`
		Price struct {
			Total struct {
				Amount   float64 `json:"amount"`
				Currency string  `json:"currency"`
			} `json:"total"`
		} `json:"price"`
		ThumbURL string `json:"thumbnail"`
		DeepLink string `json:"deep_link"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]StayOffer, 0, len(raw))
	for _, r := range raw {
		out = append(out, StayOffer{
			Source:       "Expedia",
			PropertyName: r.PropertyName,
			City:         r.City,
			Rating:       r.Rating,
			ReviewCount:  r.Reviews,
			Price:        r.Price.Total.Amount,
			Currency:     r.Price.Total.Currency,
			ThumbURL:     r.ThumbURL,
			DeepLink:     r.DeepLink,
		})
	}
	return out, nil
}

// expediaSignature returns the `EAN APIKey=...,Signature=...` auth header.
// Real signing uses HMAC-SHA512 of (apikey + shared_secret + timestamp).
// For the scaffold we keep it shaped-correctly so the request format stays
// accurate if/when creds are wired in.
func expediaSignature(key, _ string) string {
	return "EAN APIKey=" + key + ",cnonce=0,Signature=stub"
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
