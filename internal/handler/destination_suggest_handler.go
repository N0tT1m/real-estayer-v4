package handler

import (
	"errors"
	"net/http"

	"github.com/realestayer/v4/internal/service"
)

// SuggestDestinations answers "I want to do X — where should I go?".
//
// Unlike AIItinerary this takes no destination: the activities are the input
// and destinations are the output. Results are grounded against the
// destinations collection, so every entry is a real row the rest of the app
// can already handle.
func (h *Handler) SuggestDestinations(w http.ResponseWriter, r *http.Request) {
	var req service.ActivitySuggestRequest
	if err := h.parseJSON(r, &req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "invalid body")
		return
	}

	out, err := h.Enrich.DestinationSuggest.Suggest(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrAIItineraryNotConfigured):
			h.jsonError(w, http.StatusServiceUnavailable, "destination suggestions are not configured on this server")
		case errors.Is(err, service.ErrInvalidSuggestRequest):
			// Bad input reads as 400, not the 502 the older AI endpoints
			// return for a blank destination.
			h.jsonError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, service.ErrNoDestinationCatalog):
			// Not the caller's fault and not an upstream failure — the
			// constraints just excluded everything, or the boot seed is still
			// running.
			h.jsonError(w, http.StatusNotFound, err.Error())
		default:
			h.jsonError(w, http.StatusBadGateway, err.Error())
		}
		return
	}

	h.jsonResponse(w, http.StatusOK, out)
}
