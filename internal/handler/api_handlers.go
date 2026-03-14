package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/realestayer/v3/internal/models"
	"github.com/realestayer/v3/internal/service"
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
	result, err := h.listingService.Search(r.Context(), params)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, result)
}

// GetListing returns a single listing
func (h *Handler) GetListing(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	listing, err := h.listingService.GetByID(r.Context(), id)
	if err != nil {
		h.jsonError(w, http.StatusNotFound, "Listing not found")
		return
	}
	h.jsonResponse(w, http.StatusOK, listing)
}

// SearchListings searches listings with filters
func (h *Handler) SearchListings(w http.ResponseWriter, r *http.Request) {
	params := h.parseListingParams(r)
	result, err := h.listingService.Search(r.Context(), params)
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

	airports, err := h.flightService.SearchAirports(r.Context(), keyword)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusOK, airports)
}

// SearchCities searches for cities
func (h *Handler) SearchCities(w http.ResponseWriter, r *http.Request) {
	keyword := r.URL.Query().Get("q")
	if keyword == "" {
		h.jsonError(w, http.StatusBadRequest, "Query parameter 'q' is required")
		return
	}

	cities, err := h.hotelService.SearchCities(r.Context(), keyword)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusOK, cities)
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

	offers, err := h.flightService.Search(r.Context(), req)
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
	provider := r.URL.Query().Get("provider")
	if provider == "" {
		provider = "amadeus"
	}

	offer, err := h.flightService.GetOffer(r.Context(), provider, offerID)
	if err != nil {
		h.jsonError(w, http.StatusNotFound, "Offer not found")
		return
	}

	h.jsonResponse(w, http.StatusOK, offer)
}

// SearchHotels searches for hotels
func (h *Handler) SearchHotels(w http.ResponseWriter, r *http.Request) {
	req := models.HotelSearchRequest{
		CityCode: r.URL.Query().Get("city_code"),
		Adults:   2,
		Rooms:    1,
		Currency: r.URL.Query().Get("currency"),
	}

	if req.Currency == "" {
		req.Currency = "USD"
	}

	// Parse check-in date
	if checkIn := r.URL.Query().Get("check_in"); checkIn != "" {
		t, err := time.Parse("2006-01-02", checkIn)
		if err != nil {
			h.jsonError(w, http.StatusBadRequest, "Invalid check-in date format")
			return
		}
		req.CheckIn = t
	} else {
		h.jsonError(w, http.StatusBadRequest, "Check-in date is required")
		return
	}

	// Parse check-out date
	if checkOut := r.URL.Query().Get("check_out"); checkOut != "" {
		t, err := time.Parse("2006-01-02", checkOut)
		if err != nil {
			h.jsonError(w, http.StatusBadRequest, "Invalid check-out date format")
			return
		}
		req.CheckOut = t
	} else {
		h.jsonError(w, http.StatusBadRequest, "Check-out date is required")
		return
	}

	if adults := r.URL.Query().Get("adults"); adults != "" {
		n, _ := strconv.Atoi(adults)
		if n > 0 {
			req.Adults = n
		}
	}

	if rooms := r.URL.Query().Get("rooms"); rooms != "" {
		n, _ := strconv.Atoi(rooms)
		if n > 0 {
			req.Rooms = n
		}
	}

	if radius := r.URL.Query().Get("radius"); radius != "" {
		n, _ := strconv.Atoi(radius)
		req.Radius = n
	}

	offers, err := h.hotelService.Search(r.Context(), req)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"hotels": offers,
		"count":  len(offers),
	})
}

// GetHotelDetails retrieves hotel details
func (h *Handler) GetHotelDetails(w http.ResponseWriter, r *http.Request) {
	hotelID := chi.URLParam(r, "id")
	provider := r.URL.Query().Get("provider")
	if provider == "" {
		provider = "amadeus"
	}

	details, err := h.hotelService.GetDetails(r.Context(), provider, hotelID)
	if err != nil {
		h.jsonError(w, http.StatusNotFound, "Hotel not found")
		return
	}

	h.jsonResponse(w, http.StatusOK, details)
}

// GetRoomAvailability returns available rooms
func (h *Handler) GetRoomAvailability(w http.ResponseWriter, r *http.Request) {
	hotelID := chi.URLParam(r, "id")
	checkIn := r.URL.Query().Get("check_in")
	checkOut := r.URL.Query().Get("check_out")
	guests, _ := strconv.Atoi(r.URL.Query().Get("guests"))
	if guests == 0 {
		guests = 2
	}

	rooms, err := h.hotelService.GetRoomAvailability(r.Context(), "amadeus", hotelID, checkIn, checkOut, guests)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusOK, rooms)
}

// SearchCars searches for rental cars
func (h *Handler) SearchCars(w http.ResponseWriter, r *http.Request) {
	req := models.CarSearchRequest{
		PickupLocation:  r.URL.Query().Get("pickup_location"),
		DropoffLocation: r.URL.Query().Get("dropoff_location"),
		DriverAge:       25,
		Currency:        r.URL.Query().Get("currency"),
	}

	if req.DropoffLocation == "" {
		req.DropoffLocation = req.PickupLocation
	}

	if req.Currency == "" {
		req.Currency = "USD"
	}

	// Parse pickup datetime
	if pickup := r.URL.Query().Get("pickup_datetime"); pickup != "" {
		t, err := time.Parse(time.RFC3339, pickup)
		if err != nil {
			// Try simpler format
			t, err = time.Parse("2006-01-02T15:04", pickup)
			if err != nil {
				h.jsonError(w, http.StatusBadRequest, "Invalid pickup datetime format")
				return
			}
		}
		req.PickupDateTime = t
	} else {
		h.jsonError(w, http.StatusBadRequest, "Pickup datetime is required")
		return
	}

	// Parse dropoff datetime
	if dropoff := r.URL.Query().Get("dropoff_datetime"); dropoff != "" {
		t, err := time.Parse(time.RFC3339, dropoff)
		if err != nil {
			t, err = time.Parse("2006-01-02T15:04", dropoff)
			if err != nil {
				h.jsonError(w, http.StatusBadRequest, "Invalid dropoff datetime format")
				return
			}
		}
		req.DropoffDateTime = t
	} else {
		h.jsonError(w, http.StatusBadRequest, "Dropoff datetime is required")
		return
	}

	if age := r.URL.Query().Get("driver_age"); age != "" {
		n, _ := strconv.Atoi(age)
		if n > 0 {
			req.DriverAge = n
		}
	}

	if category := r.URL.Query().Get("category"); category != "" {
		req.Category = models.CarCategory(category)
	}

	if transmission := r.URL.Query().Get("transmission"); transmission != "" {
		req.TransmissionType = models.TransmissionType(transmission)
	}

	offers, err := h.carService.Search(r.Context(), req)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"cars":  offers,
		"count": len(offers),
	})
}

// GetCarOffer retrieves a specific car offer
func (h *Handler) GetCarOffer(w http.ResponseWriter, r *http.Request) {
	offerID := chi.URLParam(r, "id")
	provider := r.URL.Query().Get("provider")
	if provider == "" {
		provider = "amadeus"
	}

	offer, err := h.carService.GetOffer(r.Context(), provider, offerID)
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
	status, err := h.scraperService.GetStatus(r.Context())
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

	result, err := h.scraperService.ScrapeCity(r.Context(), params)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusOK, result)
}

// SendToDiscord sends a listing URL to Discord via webhook
func (h *Handler) SendToDiscord(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL string `json:"url"`
	}

	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.URL == "" {
		h.jsonError(w, http.StatusBadRequest, "URL is required")
		return
	}

	webhookURL := h.config.DiscordWebhookURL
	if webhookURL == "" {
		h.jsonError(w, http.StatusServiceUnavailable, "Discord webhook not configured")
		return
	}

	// Create Discord message payload
	payload := map[string]interface{}{
		"username": "AirBnB Assistant",
		"content":  fmt.Sprintf("Listing found from AirBnB: %s", req.URL),
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, "Failed to create payload")
		return
	}

	// Send to Discord webhook
	resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		slog.Error("failed to send to Discord", "error", err)
		h.jsonError(w, http.StatusInternalServerError, "Failed to send to Discord")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		slog.Error("Discord webhook returned error", "status", resp.StatusCode)
		h.jsonError(w, http.StatusInternalServerError, "Discord webhook error")
		return
	}

	slog.Info("sent listing to Discord", "url", req.URL)
	h.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message": "Successfully sent to Discord",
		"url":     req.URL,
	})
}

// TestDiscord sends a test message to Discord to verify webhook configuration
func (h *Handler) TestDiscord(w http.ResponseWriter, r *http.Request) {
	webhookURL := h.config.DiscordWebhookURL
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

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, "Failed to create payload")
		return
	}

	resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		slog.Error("failed to send test to Discord", "error", err)
		h.jsonError(w, http.StatusInternalServerError, "Failed to send to Discord")
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		slog.Error("Discord webhook returned error", "status", resp.StatusCode)
		h.jsonError(w, http.StatusInternalServerError, "Discord webhook error")
		return
	}

	slog.Info("sent test message to Discord")
	h.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"message": "Test message sent successfully",
	})
}
