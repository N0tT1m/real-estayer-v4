package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/realestayer/v4/internal/service"
)

// AdminPage renders the admin dashboard page
func (h *Handler) AdminPage(w http.ResponseWriter, r *http.Request) {
	h.render(w, r, "admin.html", map[string]interface{}{
		"Title": "Admin Dashboard",
	})
}

// AdminDashboard returns admin dashboard stats
func (h *Handler) AdminDashboard(w http.ResponseWriter, r *http.Request) {
	userStats, _ := h.userService.GetStats(r.Context())
	listingStats, _ := h.listingService.GetStats(r.Context())
	scraperStatus, _ := h.scraperService.GetStatus(r.Context())

	h.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"users":    userStats,
		"listings": listingStats,
		"scraper":  scraperStatus,
	})
}

// AdminListUsers returns paginated users
func (h *Handler) AdminListUsers(w http.ResponseWriter, r *http.Request) {
	users, total, err := h.userService.List(r.Context(), 1, 50)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusOK, map[string]interface{}{
		"users": users,
		"total": total,
	})
}

// ScrapingStatus returns the scraper status
func (h *Handler) ScrapingStatus(w http.ResponseWriter, r *http.Request) {
	status, err := h.scraperService.GetStatus(r.Context())
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusOK, status)
}

// TriggerScraping triggers a scraping operation
func (h *Handler) TriggerScraping(w http.ResponseWriter, r *http.Request) {
	city := r.URL.Query().Get("city")
	state := r.URL.Query().Get("state")
	country := r.URL.Query().Get("country")
	checkIn := r.URL.Query().Get("check_in")
	checkOut := r.URL.Query().Get("check_out")

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

	// Parse badges_only filter (only scrape listings with badges like Superhost, Guest favorite, etc.)
	badgesOnly := r.URL.Query().Get("badges_only") == "true"

	// Parse amenity filters
	hotTub := r.URL.Query().Get("hot_tub") == "true"
	pool := r.URL.Query().Get("pool") == "true"
	waterfront := r.URL.Query().Get("waterfront") == "true"

	var result interface{}
	var err error

	if city != "" {
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
		result, err = h.scraperService.ScrapeCity(r.Context(), params)
	} else {
		result, err = h.scraperService.ScrapeNorthAmerica(r.Context())
	}

	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusOK, result)
}

// TriggerRegionScrape kicks off a large-area "scrape everything" run via
// top-down map tiling. Configurable by preset (test|usa|north-america|world)
// or an explicit bounding box, plus tiling depth, enrichment, guests and
// amenity filters. Returns the scraper's "started" acknowledgement immediately.
func (h *Handler) TriggerRegionScrape(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()

	params := service.RegionScrapeParams{
		Preset:     q.Get("preset"),
		Enrich:     q.Get("enrich") != "false", // default true
		HotTub:     q.Get("hot_tub") == "true",
		Pool:       q.Get("pool") == "true",
		Waterfront: q.Get("waterfront") == "true",
	}
	if params.Preset == "" {
		params.Preset = "test" // safest default if the UI sends nothing
	}

	// Explicit bounding box: only used when all four corners parse.
	neLat, e1 := strconv.ParseFloat(q.Get("ne_lat"), 64)
	neLng, e2 := strconv.ParseFloat(q.Get("ne_lng"), 64)
	swLat, e3 := strconv.ParseFloat(q.Get("sw_lat"), 64)
	swLng, e4 := strconv.ParseFloat(q.Get("sw_lng"), 64)
	if e1 == nil && e2 == nil && e3 == nil && e4 == nil {
		params.HasBBox = true
		params.NeLat, params.NeLng, params.SwLat, params.SwLng = neLat, neLng, swLat, swLng
	}

	if d, err := strconv.Atoi(q.Get("max_depth")); err == nil && d > 0 {
		params.MaxDepth = d
	}
	if a, err := strconv.Atoi(q.Get("adults")); err == nil && a > 0 {
		params.Adults = a
	}
	if c, err := strconv.Atoi(q.Get("children")); err == nil && c >= 0 {
		params.Children = c
	}
	if i, err := strconv.Atoi(q.Get("infants")); err == nil && i >= 0 {
		params.Infants = i
	}
	if p, err := strconv.Atoi(q.Get("pets")); err == nil && p >= 0 {
		params.Pets = p
	}

	result, err := h.scraperService.ScrapeEverything(r.Context(), params)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusOK, result)
}

// AdminDeleteListing deletes a listing by ID
func (h *Handler) AdminDeleteListing(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		h.jsonError(w, http.StatusBadRequest, "listing ID required")
		return
	}

	err := h.listingService.Delete(r.Context(), id)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// AdminUpdateUserRole updates a user's role
func (h *Handler) AdminUpdateUserRole(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		h.jsonError(w, http.StatusBadRequest, "user ID required")
		return
	}

	var req struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.jsonError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Role != "user" && req.Role != "admin" {
		h.jsonError(w, http.StatusBadRequest, "invalid role")
		return
	}

	err := h.userService.UpdateRole(r.Context(), id, req.Role)
	if err != nil {
		h.jsonError(w, http.StatusInternalServerError, err.Error())
		return
	}

	h.jsonResponse(w, http.StatusOK, map[string]string{"status": "updated", "role": req.Role})
}
