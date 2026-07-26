package handler

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/realestayer/v4/internal/service"
)

// ---- Airports ----

func (h *Handler) AirportLookup(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "iata")
	a, err := h.airportService.Lookup(r.Context(), code)
	if err != nil {
		h.jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, a)
}

func (h *Handler) AirportSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	limit, _ := parseIntParam(r.URL.Query().Get("limit"))
	out := h.airportService.Search(r.Context(), q, limit)
	h.jsonResponse(w, http.StatusOK, map[string]interface{}{"airports": out})
}

// AirportDistance returns the great-circle distance between two IATA codes
// in km. Useful both for flight-leg rendering and for auto-filling
// carbon-estimate distances.
func (h *Handler) AirportDistance(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	km, err := h.airportService.DistanceBetween(r.Context(), q.Get("from"), q.Get("to"))
	if err != nil {
		h.jsonError(w, http.StatusBadRequest, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"from": q.Get("from"), "to": q.Get("to"), "distance_km": km,
	})
}

// ---- Flight status ----

func (h *Handler) FlightStatus(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	flight := q.Get("flight")
	date := q.Get("date")
	status, err := h.flightStatusSvc.Lookup(r.Context(), flight, date)
	if err != nil {
		if errors.Is(err, service.ErrFlightStatusNotConfigured) {
			h.jsonError(w, http.StatusServiceUnavailable, "flight status not configured")
			return
		}
		h.jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, status)
}

// ---- Wikidata ----

func (h *Handler) CityFacts(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		h.jsonError(w, http.StatusBadRequest, "name required")
		return
	}
	out, err := h.wikidataService.CityByName(r.Context(), name)
	if err != nil {
		h.jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, out)
}

// ---- Nature (iNaturalist + eBird) ----

func (h *Handler) NearbyNature(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lat, _ := parseFloatParam(q.Get("lat"))
	lng, _ := parseFloatParam(q.Get("lng"))
	radius, _ := parseIntParam(q.Get("radius"))
	limit, _ := parseIntParam(q.Get("limit"))
	out, err := h.natureService.INatNearby(r.Context(), lat, lng, radius, limit)
	if err != nil {
		h.jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]interface{}{"observations": out})
}

func (h *Handler) NearbyBirds(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	lat, _ := parseFloatParam(q.Get("lat"))
	lng, _ := parseFloatParam(q.Get("lng"))
	radius, _ := parseIntParam(q.Get("radius"))
	limit, _ := parseIntParam(q.Get("limit"))
	out, err := h.natureService.EBirdRecent(r.Context(), lat, lng, radius, limit)
	if err != nil {
		if errors.Is(err, service.ErrEBirdNotConfigured) {
			h.jsonError(w, http.StatusServiceUnavailable, "ebird not configured")
			return
		}
		h.jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]interface{}{"sightings": out})
}

// ---- Partner stay search (Booking / Expedia) ----
