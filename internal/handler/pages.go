package handler

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/realestayer/v4/internal/models"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// Home renders the home page
func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	// Get some featured listings
	params := models.ListingSearchParams{
		Limit:  6,
		SortBy: "rating_desc",
	}
	result, _ := h.Listings.Listing.Search(r.Context(), params)

	h.render(w, r, "home.html", map[string]interface{}{
		"Title":            "Real-Estayer - Find Your Perfect Stay",
		"FeaturedListings": result.Listings,
	})
}

// DashboardPage renders the user dashboard
func (h *Handler) DashboardPage(w http.ResponseWriter, r *http.Request) {
	userID := h.getUserID(r)

	// Get upcoming trips
	trips, _ := h.Trips.Trip.GetUpcoming(r.Context(), userID, 5)

	// Get watchlist stats
	watchlistStats, _ := h.Listings.Watchlist.GetStats(r.Context(), userID)

	// Lifetime-count check drives the onboarding nudge. Cheap on small N;
	// flip to an aggregation count later if users frequently have dozens.
	allTrips, _, _ := h.Trips.Trip.GetUserTrips(r.Context(), userID, 1, 1)

	h.render(w, r, "dashboard.html", map[string]interface{}{
		"Title":          "Dashboard",
		"UpcomingTrips":  trips,
		"WatchlistStats": watchlistStats,
		"HasTrips":       len(allTrips) > 0,
	})
}

// ListingsPage renders the listings browse page
func (h *Handler) ListingsPage(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	amenities := q["amenities"]

	page := 1
	if pageStr := q.Get("page"); pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	// locType controls how the single "Location" dropdown is interpreted:
	// state/province populate Region (with a matching Country constraint so a
	// New York state match doesn't leak into a New York city in Ontario),
	// city populates the City prefix filter. Defaults to state.
	locType := q.Get("loc_type")
	locValue := q.Get("loc_value")
	if locType == "" {
		locType = "state"
	}

	params := models.ListingSearchParams{
		Query:    q.Get("q"),
		Location: q.Get("location"),
		Features: amenities,
		SortBy:   q.Get("sort"),
		Page:     page,
		Limit:    20,
	}
	switch locType {
	case "state":
		if locValue != "" {
			params.Region = locValue
			params.Country = "United States"
		}
	case "province":
		if locValue != "" {
			params.Region = locValue
			params.Country = "Canada"
		}
	case "city":
		params.City = locValue
	}

	result, _ := h.Listings.Listing.Search(r.Context(), params)
	features, _ := h.Listings.Listing.GetFeatures(r.Context())
	states, _ := h.Listings.Listing.GetStates(r.Context())
	provinces, _ := h.Listings.Listing.GetProvinces(r.Context())
	cities, _ := h.Listings.Listing.GetCities(r.Context())

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
		"States":            states,
		"Provinces":         provinces,
		"Cities":            cities,
		"LocType":           locType,
		"LocValue":          locValue,
		"Params":            params,
		"SelectedAmenities": selectedAmenities,
	})
}

// ListingDetailPage renders a single listing
func (h *Handler) ListingDetailPage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	listing, err := h.Listings.Listing.GetByID(r.Context(), id)
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

// TripsPage renders the trips list page
func (h *Handler) TripsPage(w http.ResponseWriter, r *http.Request) {
	userID := h.getUserID(r)
	trips, total, _ := h.Trips.Trip.GetUserTrips(r.Context(), userID, 1, 20)

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

	trip, err := h.Trips.Trip.GetByID(r.Context(), userID, tripID)
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
	items, _ := h.Listings.Watchlist.GetWithListings(r.Context(), userID)

	h.render(w, r, "watchlist.html", map[string]interface{}{
		"Title":          "My Watchlist",
		"WatchlistItems": items,
	})
}

// ProfilePage renders the user profile page
func (h *Handler) ProfilePage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "profile.html", map[string]interface{}{
		"Title": "My Profile",
	})
}

// SecurityPage shows the caller the last 100 sensitive events on their own
// account — password changes, identity edits, share links. It reads from the
// audit log that the rest of the app writes to. Admins can't see other
// users' events from here; that belongs behind a RequireAdmin route.
func (h *Handler) SecurityPage(w http.ResponseWriter, r *http.Request) {
	uid := h.getUserOID(r)
	events, err := h.Core.Audit.ListByUser(r.Context(), uid, 100)
	if err != nil {
		slog.Warn("security page: list audit events failed", "user_id", uid.Hex(), "error", err)
		h.ServerError(w, r)
		return
	}
	h.render(w, r, "security.html", map[string]interface{}{
		"Title":  "Security activity",
		"Events": events,
	})
}

// BookingsPage renders the user's bookings
func (h *Handler) BookingsPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "bookings.html", map[string]interface{}{
		"Title": "My Bookings",
	})
}

// FlightBookingPage renders the passenger-details form for a flight offer
// selected on /flights. The offer ID is carried through from the search
// page — the template posts back to /api/v1/flights/book to create the
// actual order via whichever provider is configured (Duffel by default).
func (h *Handler) FlightBookingPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "flight_book.html", map[string]interface{}{
		"Title":    "Complete your flight booking",
		"OfferID":  r.URL.Query().Get("offer"),
		"Provider": r.URL.Query().Get("provider"),
	})
}

// BookingConfirmationPage is the landing after a successful flight booking.
// Reference ID comes in via ?ref=. We don't fetch
// the booking server-side here — the page reads it for display only; the
// real source of truth is /bookings.
func (h *Handler) BookingConfirmationPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "booking_confirmation.html", map[string]interface{}{
		"Title":     "Booking confirmed",
		"Reference": r.URL.Query().Get("ref"),
	})
}

// Health reports whether this instance can actually serve traffic, which
// means reaching MongoDB — every meaningful route reads from it.
//
// This used to write "OK" unconditionally, so Docker's HEALTHCHECK and any
// load balancer in front would keep a instance in rotation with a dead
// database. A liveness probe that cannot fail is not a probe.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	// Well inside the 15s WriteTimeout: a health check that hangs is itself a
	// failure mode, so bound it tighter than the request would be.
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	if h.Core.DB != nil {
		if err := h.Core.DB.Client.Ping(ctx, readpref.Primary()); err != nil {
			slog.Warn("health: database ping failed", "error", err)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"status": "degraded",
				"error":  "database unreachable",
			})
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// ScrapePage renders the public scraping page
func (h *Handler) ScrapePage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "scrape.html", map[string]interface{}{
		"Title": "Find Vacation Rentals",
	})
}
