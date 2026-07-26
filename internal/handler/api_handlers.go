package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/realestayer/v4/internal/middleware"
	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/service"
)

// HealthAPI returns health status as JSON
func (h *Handler) HealthAPI(w http.ResponseWriter, r *http.Request) {
	h.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"status":    "ok",
		"timestamp": time.Now().UTC(),
	})
}

// GetListings returns paginated listings
func (h *Handler) GetListings(w http.ResponseWriter, r *http.Request) {
	params := h.parseListingParams(r)
	result, err := h.Listings.Listing.Search(r.Context(), params)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, result)
}

// GetListing returns a single listing
func (h *Handler) GetListing(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	listing, err := h.Listings.Listing.GetByID(r.Context(), id)
	if err != nil {
		h.jsonError(w, http.StatusNotFound, "Listing not found")
		return
	}
	h.jsonResponse(w, http.StatusOK, listing)
}

// SearchListings searches listings with filters
func (h *Handler) SearchListings(w http.ResponseWriter, r *http.Request) {
	params := h.parseListingParams(r)
	result, err := h.Listings.Listing.Search(r.Context(), params)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, result)
}

// SearchAirports searches for airports
func (h *Handler) SearchAirports(w http.ResponseWriter, r *http.Request) {
	keyword := r.URL.Query().Get("q")
	if keyword == "" {
		h.jsonError(w, http.StatusBadRequest, "Query parameter 'q' is required")
		return
	}

	airports, err := h.Flight.SearchAirports(r.Context(), keyword)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusOK, airports)
}

// SearchFlights searches for flights
func (h *Handler) SearchFlights(w http.ResponseWriter, r *http.Request) {
	req := models.FlightSearchRequest{
		Origin:      r.URL.Query().Get("origin"),
		Destination: r.URL.Query().Get("destination"),
		Adults:      1,
		Currency:    r.URL.Query().Get("currency"),
	}

	if req.Currency == "" {
		req.Currency = "USD"
	}

	// Parse departure date
	if depDate := r.URL.Query().Get("departure_date"); depDate != "" {
		t, err := time.Parse("2006-01-02", depDate)
		if err != nil {
			h.jsonError(w, http.StatusBadRequest, "Invalid departure date format")
			return
		}
		req.DepartureDate = t
	} else {
		h.jsonError(w, http.StatusBadRequest, "Departure date is required")
		return
	}

	// Parse return date
	if retDate := r.URL.Query().Get("return_date"); retDate != "" {
		t, err := time.Parse("2006-01-02", retDate)
		if err != nil {
			h.jsonError(w, http.StatusBadRequest, "Invalid return date format")
			return
		}
		req.ReturnDate = &t
	}

	// Parse other params
	if adults := r.URL.Query().Get("adults"); adults != "" {
		n, _ := strconv.Atoi(adults)
		if n > 0 {
			req.Adults = n
		}
	}

	if children := r.URL.Query().Get("children"); children != "" {
		n, _ := strconv.Atoi(children)
		req.Children = n
	}

	if cabin := r.URL.Query().Get("cabin_class"); cabin != "" {
		req.CabinClass = models.CabinClass(cabin)
	}

	if direct := r.URL.Query().Get("direct_only"); direct == "true" {
		req.DirectOnly = true
	}

	offers, err := h.Flight.Search(r.Context(), req)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"offers": offers,
		"count":  len(offers),
	})
}

// GetFlightOffer retrieves a specific flight offer
func (h *Handler) GetFlightOffer(w http.ResponseWriter, r *http.Request) {
	offerID := chi.URLParam(r, "id")
	// Empty means "use the registry default" — provider selection lives in
	// cmd/server/main.go, not here.
	provider := r.URL.Query().Get("provider")

	offer, err := h.Flight.GetOffer(r.Context(), provider, offerID)
	if err != nil {
		h.jsonError(w, http.StatusNotFound, "Offer not found")
		return
	}

	h.jsonResponse(w, http.StatusOK, offer)
}

// parseListingParams extracts listing search parameters from request
func (h *Handler) parseListingParams(r *http.Request) models.ListingSearchParams {
	params := models.DefaultListingSearchParams()

	params.Query = r.URL.Query().Get("q")
	params.Location = r.URL.Query().Get("location")
	params.Region = r.URL.Query().Get("region")
	params.Country = r.URL.Query().Get("country")
	params.SortBy = r.URL.Query().Get("sort")
	params.PropertyType = r.URL.Query().Get("property_type")

	if minPrice := r.URL.Query().Get("min_price"); minPrice != "" {
		params.MinPrice, _ = strconv.ParseFloat(minPrice, 64)
	}
	if maxPrice := r.URL.Query().Get("max_price"); maxPrice != "" {
		params.MaxPrice, _ = strconv.ParseFloat(maxPrice, 64)
	}
	if minRating := r.URL.Query().Get("min_rating"); minRating != "" {
		params.MinRating, _ = strconv.ParseFloat(minRating, 64)
	}
	if features := r.URL.Query().Get("features"); features != "" {
		params.Features = strings.Split(features, ",")
	}
	if amenities := r.URL.Query().Get("amenities"); amenities != "" {
		params.Amenities = strings.Split(amenities, ",")
	}
	if v := r.URL.Query().Get("min_bedrooms"); v != "" {
		params.MinBedrooms, _ = strconv.Atoi(v)
	}
	if v := r.URL.Query().Get("min_sleeps"); v != "" {
		params.MinSleeps, _ = strconv.Atoi(v)
	}
	if page := r.URL.Query().Get("page"); page != "" {
		params.Page, _ = strconv.Atoi(page)
	}
	if limit := r.URL.Query().Get("limit"); limit != "" {
		params.Limit, _ = strconv.Atoi(limit)
	}

	return params
}

// PublicScraperStatus returns the scraper status (public endpoint)
func (h *Handler) PublicScraperStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.Listings.Scraper.GetStatus(r.Context())
	if err != nil {
		h.jsonResponse(w, http.StatusOK, map[string]interface{}{
			"status":  "offline",
			"message": "Scraper service unavailable",
		})
		return
	}

	h.jsonResponse(w, http.StatusOK, status)
}

// PublicTriggerScrape triggers a city scraping operation (public endpoint - no North America scrape)
func (h *Handler) PublicTriggerScrape(w http.ResponseWriter, r *http.Request) {
	city := r.URL.Query().Get("city")
	if city == "" {
		h.jsonError(w, http.StatusBadRequest, "City is required")
		return
	}

	state := r.URL.Query().Get("state")
	country := r.URL.Query().Get("country")
	checkIn := r.URL.Query().Get("check_in")
	checkOut := r.URL.Query().Get("check_out")

	// Validate dates
	if checkIn == "" || checkOut == "" {
		h.jsonError(w, http.StatusBadRequest, "Check-in and check-out dates are required")
		return
	}

	// Parse integer params with defaults
	limit := 0 // 0 means unlimited
	if l, err := strconv.Atoi(r.URL.Query().Get("limit")); err == nil && l >= 0 {
		limit = l
	}

	adults := 2 // default
	if a, err := strconv.Atoi(r.URL.Query().Get("adults")); err == nil && a > 0 {
		adults = a
	}

	children := 0
	if c, err := strconv.Atoi(r.URL.Query().Get("children")); err == nil && c >= 0 {
		children = c
	}

	infants := 0
	if i, err := strconv.Atoi(r.URL.Query().Get("infants")); err == nil && i >= 0 {
		infants = i
	}

	pets := 0
	if p, err := strconv.Atoi(r.URL.Query().Get("pets")); err == nil && p >= 0 {
		pets = p
	}

	badgesOnly := r.URL.Query().Get("badges_only") == "true"

	// Parse amenity filters
	hotTub := r.URL.Query().Get("hot_tub") == "true"
	pool := r.URL.Query().Get("pool") == "true"
	waterfront := r.URL.Query().Get("waterfront") == "true"

	params := service.ScrapeParams{
		City:       city,
		State:      state,
		Country:    country,
		CheckIn:    checkIn,
		CheckOut:   checkOut,
		Adults:     adults,
		Children:   children,
		Infants:    infants,
		Pets:       pets,
		Limit:      limit,
		BadgesOnly: badgesOnly,
		HotTub:     hotTub,
		Pool:       pool,
		Waterfront: waterfront,
	}

	result, err := h.Listings.Scraper.ScrapeCity(r.Context(), params)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusOK, result)
}

// discordClient is used for outbound webhook posts. The bare http.Post helper
// uses http.DefaultClient, which has no timeout — a hung Discord endpoint would
// pin the goroutine past the caller's own deadline.
var discordClient = &http.Client{
	Timeout: 10 * time.Second,
	// Refuse redirects. The webhook host is pinned to discord.com on save and
	// re-checked before sending, but the default client would happily follow a
	// 302 to an internal address — which would defeat both checks. A webhook
	// POST has no legitimate reason to be redirected.
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		return fmt.Errorf("discord webhook: refusing redirect to %s", req.URL.Host)
	},
}

// postDiscordWebhook delivers payload to webhookURL, bound to ctx so a
// cancelled request doesn't leave the call in flight.
func postDiscordWebhook(ctx context.Context, webhookURL string, payload any) error {
	// Belt-and-braces: callers already validate, but this is the single choke
	// point where an outbound POST happens, so enforce the host here too.
	if !service.IsDiscordWebhook(webhookURL) {
		return fmt.Errorf("refusing to POST to a non-Discord host")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal payload: %w", err)
	}
	// #nosec G704 -- webhookURL is host-pinned to the discord.com family by
	// service.IsDiscordWebhook above, and discordClient refuses redirects.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	// #nosec G704 -- see the host check and CheckRedirect guard above.
	resp, err := discordClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return fmt.Errorf("discord webhook returned status %d", resp.StatusCode)
	}
	return nil
}

// discordEmbedColour is the accent stripe on the embed (a warm coral).
const discordEmbedColour = 0xFF5A5F

// listingEmbed renders a listing as a Discord embed: a titled card linking to
// the real Airbnb page, with the photo, price, rating and location. Discord
// renders `url` as a clickable title, which is what makes this useful over
// pasting a bare link.
func listingEmbed(l *models.Listing) map[string]any {
	title := strings.TrimSpace(l.Title)
	if title == "" {
		title = "Airbnb listing"
	}
	// Discord rejects embed titles over 256 chars and descriptions over 4096.
	if len(title) > 240 {
		title = title[:240] + "…"
	}

	var fields []map[string]any
	addField := func(name, value string, inline bool) {
		if strings.TrimSpace(value) == "" {
			return
		}
		fields = append(fields, map[string]any{"name": name, "value": value, "inline": inline})
	}
	addField("Price", l.Price, true)

	rating := l.Rating
	if l.ReviewsCount > 0 && rating != "" {
		rating = fmt.Sprintf("%s (%d reviews)", rating, l.ReviewsCount)
	}
	addField("Rating", rating, true)
	addField("Location", l.Location, false)
	if l.PropertyType != "" {
		addField("Type", l.PropertyType, true)
	}
	if len(l.Features) > 0 {
		feats := l.Features
		if len(feats) > 8 {
			feats = feats[:8]
		}
		addField("Features", strings.Join(feats, " · "), false)
	}

	embed := map[string]any{
		"title":  title,
		"url":    l.URL,
		"color":  discordEmbedColour,
		"fields": fields,
		"footer": map[string]string{"text": "Real-Estayer"},
	}
	if d := strings.TrimSpace(l.Description); d != "" {
		if len(d) > 400 {
			d = d[:400] + "…"
		}
		embed["description"] = d
	}
	if l.PictureURL != "" {
		embed["image"] = map[string]string{"url": l.PictureURL}
	}
	return embed
}

// resolveDiscordWebhook picks where to send: the caller's own webhook when
// they've configured one, otherwise the operator-wide webhook. The host is
// re-validated here rather than trusting what's stored — see
// service.IsDiscordWebhook.
func (h *Handler) resolveDiscordWebhook(r *http.Request) (string, error) {
	if u := middleware.GetUser(r.Context()); u != nil {
		if hook := strings.TrimSpace(u.Preferences.Notifications.DiscordWebhook); hook != "" {
			if !service.IsDiscordWebhook(hook) {
				return "", errors.New("your saved Discord webhook is not a valid discord.com webhook URL")
			}
			return hook, nil
		}
	}
	if hook := strings.TrimSpace(h.Config.DiscordWebhookURL); hook != "" {
		return hook, nil
	}
	return "", errors.New("no Discord webhook configured — add one under Profile → Notifications")
}

// SendToDiscord posts a listing to Discord as a rich embed linking back to the
// real Airbnb page. Accepts {"listing_id":"..."} to build the full card, or
// {"url":"..."} to relay a bare link.
func (h *Handler) SendToDiscord(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ListingID string `json:"listing_id"`
		URL       string `json:"url"`
	}
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if req.ListingID == "" && req.URL == "" {
		h.jsonError(w, http.StatusBadRequest, "listing_id or url is required")
		return
	}

	webhookURL, err := h.resolveDiscordWebhook(r)
	if err != nil {
		h.jsonError(w, http.StatusServiceUnavailable, err.Error())
		return
	}

	payload := map[string]any{"username": "Real-Estayer"}
	sentURL := req.URL

	if req.ListingID != "" {
		listing, err := h.Listings.Listing.GetByID(r.Context(), req.ListingID)
		if err != nil {
			h.jsonError(w, http.StatusNotFound, "Listing not found")
			return
		}
		payload["embeds"] = []map[string]any{listingEmbed(listing)}
		sentURL = listing.URL
	} else {
		// Bare-link relay. Discord will unfurl it itself.
		payload["content"] = sentURL
	}

	if err := postDiscordWebhook(r.Context(), webhookURL, payload); err != nil {
		slog.Error("failed to send to Discord", "error", err)
		h.jsonError(w, http.StatusBadGateway, "Failed to send to Discord")
		return
	}

	slog.Info("sent listing to Discord", "listing_id", req.ListingID, "url", sentURL)
	h.jsonResponse(w, http.StatusOK, map[string]any{
		"message": "Sent to Discord",
		"url":     sentURL,
	})
}

// TestDiscord sends a test message to Discord to verify webhook configuration
func (h *Handler) TestDiscord(w http.ResponseWriter, r *http.Request) {
	webhookURL := h.Config.DiscordWebhookURL
	if webhookURL == "" {
		h.jsonError(w, http.StatusServiceUnavailable, "Discord webhook not configured")
		return
	}

	// Create Discord test message payload
	payload := map[string]interface{}{
		"username": "Real Estayer",
		"embeds": []map[string]interface{}{
			{
				"title":       "Test Notification",
				"description": "Your Discord integration is working correctly!",
				"color":       5793266, // Green color
				"footer": map[string]string{
					"text": "Real Estayer Discord Integration",
				},
			},
		},
	}

	if err := postDiscordWebhook(r.Context(), webhookURL, payload); err != nil {
		slog.Error("failed to send test to Discord", "error", err)
		h.jsonError(w, http.StatusInternalServerError, "Failed to send to Discord")
		return
	}

	slog.Info("sent test message to Discord")
	h.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message": "Test message sent successfully",
	})
}
