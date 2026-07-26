package handler

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/service"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// BookFlight handles flight booking. ?provider= selects a specific backend;
// omitting it uses the registry default (Duffel).
func (h *Handler) BookFlight(w http.ResponseWriter, r *http.Request) {
	var req models.FlightBookingRequest
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	provider := r.URL.Query().Get("provider")
	if provider == "" {
		provider = "duffel"
	}

	booking, err := h.Flight.Book(r.Context(), h.getUserID(r), req, provider)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusCreated, booking)
}

// GetUserBookings returns user's bookings
func (h *Handler) GetUserBookings(w http.ResponseWriter, r *http.Request) {
	// This would need a booking service method
	h.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"bookings": []interface{}{},
		"total":    0,
	})
}

// GetBooking returns a single booking
func (h *Handler) GetBooking(w http.ResponseWriter, r *http.Request) {
	bookingID := chi.URLParam(r, "id")
	objID, err := primitive.ObjectIDFromHex(bookingID)
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, "Invalid booking ID")
		return
	}
	_ = objID // Would use this to fetch booking

	h.jsonError(w, http.StatusNotFound, "Booking not found")
}

// GetTrips returns user's trips
func (h *Handler) GetTrips(w http.ResponseWriter, r *http.Request) {
	trips, total, err := h.Trips.Trip.GetUserTrips(r.Context(), h.getUserID(r), 1, 20)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"trips": trips,
		"total": total,
	})
}

// CreateTrip creates a new trip
func (h *Handler) CreateTrip(w http.ResponseWriter, r *http.Request) {
	var req models.CreateTripRequest
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	trip, err := h.Trips.Trip.Create(r.Context(), h.getUserID(r), req)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusCreated, trip)
}

// GetTrip returns a single trip
func (h *Handler) GetTrip(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")

	trip, err := h.Trips.Trip.GetByID(r.Context(), h.getUserID(r), tripID)
	if err != nil {
		h.jsonError(w, http.StatusNotFound, "Trip not found")
		return
	}

	h.jsonResponse(w, http.StatusOK, trip)
}

// UpdateTrip updates a trip
func (h *Handler) UpdateTrip(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")

	var req models.UpdateTripRequest
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	trip, err := h.Trips.Trip.Update(r.Context(), h.getUserID(r), tripID, req)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusOK, trip)
}

// DeleteTrip deletes a trip
func (h *Handler) DeleteTrip(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")

	if err := h.Trips.Trip.Delete(r.Context(), h.getUserID(r), tripID); err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusOK, map[string]string{"message": "Trip deleted"})
}

// AddTripItem adds an item to a trip
func (h *Handler) AddTripItem(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")

	var req models.AddTripItemRequest
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	trip, err := h.Trips.Trip.AddItem(r.Context(), h.getUserID(r), tripID, req)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusOK, trip)
}

// RemoveTripItem removes an item from a trip
func (h *Handler) RemoveTripItem(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")
	itemID := chi.URLParam(r, "itemId")

	if err := h.Trips.Trip.RemoveItem(r.Context(), h.getUserID(r), tripID, itemID); err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusOK, map[string]string{"message": "Item removed"})
}

// GetWatchlist returns user's watchlist
func (h *Handler) GetWatchlist(w http.ResponseWriter, r *http.Request) {
	items, err := h.Listings.Watchlist.GetWithListings(r.Context(), h.getUserID(r))
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusOK, items)
}

// AddToWatchlist adds a listing to watchlist
func (h *Handler) AddToWatchlist(w http.ResponseWriter, r *http.Request) {
	var req models.AddToWatchlistRequest
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	item, err := h.Listings.Watchlist.Add(r.Context(), h.getUserID(r), req)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusCreated, item)
}

// RemoveFromWatchlist removes a listing from watchlist
func (h *Handler) RemoveFromWatchlist(w http.ResponseWriter, r *http.Request) {
	itemID := chi.URLParam(r, "id")

	if err := h.Listings.Watchlist.Remove(r.Context(), h.getUserID(r), itemID); err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusOK, map[string]string{"message": "Removed from watchlist"})
}

// GetCurrentUser returns the current user
func (h *Handler) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	user := h.getUser(r)
	h.jsonResponse(w, http.StatusOK, user)
}

// UpdateUser updates the current user
func (h *Handler) UpdateUser(w http.ResponseWriter, r *http.Request) {
	var req models.UpdateUserRequest
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	user, err := h.Core.User.Update(r.Context(), h.getUserID(r), req)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusOK, user)
}

// ChangePassword changes the user's password
func (h *Handler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var req models.ChangePasswordRequest
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := h.Core.Auth.ChangePassword(r.Context(), h.getUserID(r), req.CurrentPassword, req.NewPassword); err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidCredentials):
			h.jsonError(w, http.StatusUnauthorized, "Current password is incorrect")
		case errors.Is(err, service.ErrWeakPassword):
			h.jsonError(w, http.StatusBadRequest, "Password must be at least 10 characters and include upper, lower, and a digit")
		default:
			h.jsonError(w, http.StatusInternalServerError, "Failed to change password")
		}
		return
	}

	h.Core.Audit.Record(r.Context(), h.getUserOID(r), r, models.AuditActionPasswordChanged, nil)
	h.jsonResponse(w, http.StatusOK, map[string]string{"message": "Password changed"})
}
