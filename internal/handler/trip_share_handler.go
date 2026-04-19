package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/realestayer/v4/internal/models"
)

// CreateTripShare enables public sharing for a trip and returns the slug/URL.
func (h *Handler) CreateTripShare(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")
	slug, err := h.tripService.EnableSharing(r.Context(), h.getUserID(r), tripID)
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.auditService.Record(r.Context(), h.getUserOID(r), r, models.AuditActionTripShareEnabled, map[string]any{
		"trip_id": tripID,
	})
	h.jsonResponse(w, http.StatusOK, map[string]string{
		"slug": slug,
		"url":  "/trips/shared/" + slug,
	})
}

// RevokeTripShare disables public sharing for a trip.
func (h *Handler) RevokeTripShare(w http.ResponseWriter, r *http.Request) {
	tripID := chi.URLParam(r, "id")
	if err := h.tripService.DisableSharing(r.Context(), h.getUserID(r), tripID); err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.auditService.Record(r.Context(), h.getUserOID(r), r, models.AuditActionTripShareRevoked, map[string]any{
		"trip_id": tripID,
	})
	h.jsonResponse(w, http.StatusOK, map[string]string{"message": "sharing disabled"})
}

// SharedTripPage renders a read-only view of a trip by slug. No auth needed —
// the slug is the authorisation.
func (h *Handler) SharedTripPage(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	trip, err := h.tripService.GetBySlug(r.Context(), slug)
	if err != nil {
		h.NotFound(w, r)
		return
	}
	h.render(w, r, "shared_trip.html", map[string]interface{}{
		"Title":         trip.Name,
		"Trip":          trip,
		"OGTitle":       trip.Name + " — trip itinerary",
		"OGDescription": shortDescription(trip.Description, 180),
		"OGImage":       trip.CoverImage,
		"OGType":        "article",
		"CanonicalURL":  "/trips/shared/" + slug,
		"JSONLD": map[string]interface{}{
			"@context":    "https://schema.org",
			"@type":       "TouristTrip",
			"name":        trip.Name,
			"description": trip.Description,
			"startDate":   trip.StartDate.Format("2006-01-02"),
			"endDate":     trip.EndDate.Format("2006-01-02"),
		},
	})
}

// shortDescription truncates long descriptions into a safe snippet — OG
// descriptions render awfully when they wrap 2+ lines.
func shortDescription(s string, n int) string {
	if len(s) <= n {
		return s
	}
	trimmed := s[:n]
	// Try not to slice in the middle of a word.
	if space := lastIndexByte(trimmed, ' '); space > n/2 {
		trimmed = trimmed[:space]
	}
	return trimmed + "…"
}

func lastIndexByte(s string, b byte) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == b {
			return i
		}
	}
	return -1
}
