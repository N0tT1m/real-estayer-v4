package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// ---- Country basics + holidays ----

func (h *Handler) CountryBasics(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	b, err := h.Enrich.Country.Basics(r.Context(), code)
	if err != nil {
		h.jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	if b == nil {
		h.jsonResponse(w, http.StatusOK, map[string]interface{}{"basics": nil})
		return
	}
	h.jsonResponse(w, http.StatusOK, b)
}

func (h *Handler) CountryHolidays(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	q := r.URL.Query()
	year, _ := parseIntParam(q.Get("year"))
	if year == 0 {
		year = time.Now().Year()
	}
	if startStr, endStr := q.Get("start"), q.Get("end"); startStr != "" && endStr != "" {
		start, err1 := time.Parse("2006-01-02", startStr)
		end, err2 := time.Parse("2006-01-02", endStr)
		if err1 == nil && err2 == nil {
			out, err := h.Enrich.Country.HolidaysInRange(r.Context(), code, start, end)
			if err != nil {
				h.jsonError(w, http.StatusBadGateway, err.Error())
				return
			}
			h.jsonResponse(w, http.StatusOK, out)
			return
		}
	}
	out, err := h.Enrich.Country.Holidays(r.Context(), code, year)
	if err != nil {
		h.jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, out)
}

// ---- Advisory ----

func (h *Handler) CountryAdvisory(w http.ResponseWriter, r *http.Request) {
	code := chi.URLParam(r, "code")
	h.jsonResponse(w, http.StatusOK, h.Enrich.Advisory.ForCountry(r.Context(), code))
}

// ---- Sunrise / sunset ----

func (h *Handler) SunTimes(w http.ResponseWriter, r *http.Request) {
	lat, _ := parseFloatParam(r.URL.Query().Get("lat"))
	lng, _ := parseFloatParam(r.URL.Query().Get("lng"))
	date := r.URL.Query().Get("date")
	if lat == 0 && lng == 0 {
		h.jsonError(w, http.StatusBadRequest, "lat & lng required")
		return
	}
	t, err := h.Enrich.Sun.Get(r.Context(), lat, lng, date)
	if err != nil {
		h.jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, t)
}

// ---- Air quality ----

func (h *Handler) AirQuality(w http.ResponseWriter, r *http.Request) {
	lat, _ := parseFloatParam(r.URL.Query().Get("lat"))
	lng, _ := parseFloatParam(r.URL.Query().Get("lng"))
	radius, _ := parseIntParam(r.URL.Query().Get("radius"))
	aq, err := h.Enrich.Air.Get(r.Context(), lat, lng, radius)
	if err != nil {
		h.jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, aq)
}

// ---- Routing ----

func (h *Handler) Directions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	profile := q.Get("profile")
	fLat, _ := parseFloatParam(q.Get("from_lat"))
	fLng, _ := parseFloatParam(q.Get("from_lng"))
	tLat, _ := parseFloatParam(q.Get("to_lat"))
	tLng, _ := parseFloatParam(q.Get("to_lng"))
	route, err := h.Enrich.Routing.Directions(r.Context(), profile, fLat, fLng, tLat, tLng)
	if err != nil {
		h.jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, route)
}

// ---- Geocoding ----

func (h *Handler) Geocode(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		h.jsonError(w, http.StatusBadRequest, "q is required")
		return
	}
	limit, _ := parseIntParam(r.URL.Query().Get("limit"))
	out, err := h.Enrich.Geocoding.Search(r.Context(), q, limit)
	if err != nil {
		h.jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, map[string]interface{}{"results": out})
}

// ---- Climate normals ----

func (h *Handler) ClimateNormals(w http.ResponseWriter, r *http.Request) {
	lat, _ := parseFloatParam(r.URL.Query().Get("lat"))
	lng, _ := parseFloatParam(r.URL.Query().Get("lng"))
	out, err := h.Enrich.Weather.ClimateNormals(r.Context(), lat, lng)
	if err != nil {
		h.jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, out)
}

// ---- Carbon ----

func (h *Handler) TripCarbon(w http.ResponseWriter, r *http.Request) {
	trip, err := h.Trips.Trip.GetByID(r.Context(), h.getUserID(r), chi.URLParam(r, "id"))
	if err != nil {
		h.jsonError(w, http.StatusNotFound, "not found")
		return
	}
	h.jsonResponse(w, http.StatusOK, h.Enrich.Carbon.EstimateTrip(trip))
}

// ---- Travel stats + profile ----

func (h *Handler) UserTravelStats(w http.ResponseWriter, r *http.Request) {
	out, err := h.Trips.Stats.ForUser(r.Context(), h.getUserID(r))
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, out)
}

func (h *Handler) UserTravelProfile(w http.ResponseWriter, r *http.Request) {
	out, err := h.Trips.Stats.ProfileFor(r.Context(), h.getUserID(r), h.Listings.Collections)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, out)
}

// Page handler for the /travel-stats dashboard.
func (h *Handler) TravelStatsPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "travel_stats.html", map[string]interface{}{"Title": "Your travel stats"})
}

// ---- Affiliate quick-links ----

func (h *Handler) AffiliateLinks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	dest := q.Get("destination")
	if dest == "" {
		h.jsonError(w, http.StatusBadRequest, "destination is required")
		return
	}
	var in, out *time.Time
	if v := q.Get("in"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			in = &t
		}
	}
	if v := q.Get("out"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			out = &t
		}
	}
	origin := q.Get("origin")
	h.jsonResponse(w, http.StatusOK, h.Enrich.Affiliate.ForDestination(dest, in, out, origin))
}

// ---- Unsplash hero ----

func (h *Handler) UnsplashHero(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		h.jsonError(w, http.StatusBadRequest, "q is required")
		return
	}
	photo, err := h.Enrich.Unsplash.ForQuery(r.Context(), q)
	if err != nil {
		h.jsonError(w, http.StatusBadGateway, err.Error())
		return
	}
	h.jsonResponse(w, http.StatusOK, photo)
}
