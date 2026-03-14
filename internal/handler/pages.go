package handler

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/realestayer/v3/internal/models"
)

// Home renders the home page
func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	// Get some featured listings
	params := models.ListingSearchParams{
		Limit:  6,
		SortBy: "rating_desc",
	}
	result, _ := h.listingService.Search(r.Context(), params)

	h.render(w, r, "home.html", map[string]interface{}{
		"Title":           "Real-Estayer - Find Your Perfect Stay",
		"FeaturedListings": result.Listings,
	})
}

// DashboardPage renders the user dashboard
func (h *Handler) DashboardPage(w http.ResponseWriter, r *http.Request) {
	userID := h.getUserID(r)

	// Get upcoming trips
	trips, _ := h.tripService.GetUpcoming(r.Context(), userID, 5)

	// Get watchlist stats
	watchlistStats, _ := h.watchlistService.GetStats(r.Context(), userID)

	h.render(w, r, "dashboard.html", map[string]interface{}{
		"Title":          "Dashboard",
		"UpcomingTrips":  trips,
		"WatchlistStats": watchlistStats,
	})
}

// ListingsPage renders the listings browse page
func (h *Handler) ListingsPage(w http.ResponseWriter, r *http.Request) {
	// Parse amenities from query params (can be multiple)
	amenities := r.URL.Query()["amenities"]

	// Parse page number from query params
	page := 1
	if pageStr := r.URL.Query().Get("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	params := models.ListingSearchParams{
		Query:    r.URL.Query().Get("q"),
		Location: r.URL.Query().Get("location"),
		Region:   r.URL.Query().Get("region"),
		Country:  r.URL.Query().Get("country"),
		Features: amenities,
		SortBy:   r.URL.Query().Get("sort"),
		Page:     page,
		Limit:    20,
	}

	result, _ := h.listingService.Search(r.Context(), params)
	features, _ := h.listingService.GetFeatures(r.Context())
	regions, _ := h.listingService.GetRegions(r.Context())

	// Create a map of selected amenities for easy lookup in template
	selectedAmenities := make(map[string]bool)
	for _, a := range amenities {
		selectedAmenities[a] = true
	}

	h.render(w, r, "listings.html", map[string]interface{}{
		"Title":             "Browse Listings",
		"Listings":          result.Listings,
		"Total":             result.Total,
		"Page":              result.Page,
		"TotalPages":        result.TotalPages,
		"Limit":             result.Limit,
		"Features":          features,
		"Regions":           regions,
		"Params":            params,
		"SelectedAmenities": selectedAmenities,
	})
}

// ListingDetailPage renders a single listing
func (h *Handler) ListingDetailPage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	listing, err := h.listingService.GetByID(r.Context(), id)
	if err != nil {
		http.Error(w, "Listing not found", http.StatusNotFound)
		return
	}

	h.render(w, r, "listing.html", map[string]interface{}{
		"Title":   listing.Title,
		"Listing": listing,
	})
}

// FlightsPage renders the flight search page
func (h *Handler) FlightsPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "flights.html", map[string]interface{}{
		"Title": "Search Flights",
	})
}

// HotelsPage renders the hotel search page
func (h *Handler) HotelsPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "hotels.html", map[string]interface{}{
		"Title": "Search Hotels",
	})
}

// CarsPage renders the car rental search page
func (h *Handler) CarsPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "cars.html", map[string]interface{}{
		"Title": "Rent a Car",
	})
}

// TripsPage renders the trips list page
func (h *Handler) TripsPage(w http.ResponseWriter, r *http.Request) {
	userID := h.getUserID(r)
	trips, total, _ := h.tripService.GetUserTrips(r.Context(), userID, 1, 20)

	h.render(w, r, "trips.html", map[string]interface{}{
		"Title": "My Trips",
		"Trips": trips,
		"Total": total,
	})
}

// TripDetailPage renders a single trip
func (h *Handler) TripDetailPage(w http.ResponseWriter, r *http.Request) {
	userID := h.getUserID(r)
	tripID := chi.URLParam(r, "id")

	trip, err := h.tripService.GetByID(r.Context(), userID, tripID)
	if err != nil {
		http.Error(w, "Trip not found", http.StatusNotFound)
		return
	}

	h.render(w, r, "trip_detail.html", map[string]interface{}{
		"Title": trip.Name,
		"Trip":  trip,
	})
}

// WatchlistPage renders the watchlist page
func (h *Handler) WatchlistPage(w http.ResponseWriter, r *http.Request) {
	userID := h.getUserID(r)
	items, _ := h.watchlistService.GetWithListings(r.Context(), userID)

	h.render(w, r, "watchlist.html", map[string]interface{}{
		"Title": "My Watchlist",
		"Items": items,
	})
}

// ProfilePage renders the user profile page
func (h *Handler) ProfilePage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "profile.html", map[string]interface{}{
		"Title": "My Profile",
	})
}

// BookingsPage renders the user's bookings
func (h *Handler) BookingsPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "bookings.html", map[string]interface{}{
		"Title": "My Bookings",
	})
}

// Health check page
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Write([]byte("OK"))
}

// ScrapePage renders the public scraping page
func (h *Handler) ScrapePage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "scrape.html", map[string]interface{}{
		"Title": "Find Vacation Rentals",
	})
}
