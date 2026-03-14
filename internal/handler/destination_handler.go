package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/realestayer/v3/internal/models"
	"github.com/realestayer/v3/internal/service"
)

type DestinationHandler struct {
	tmpl        *TemplateRenderer
	destService *service.DestinationService
}

func NewDestinationHandler(tmpl *TemplateRenderer, destService *service.DestinationService) *DestinationHandler {
	return &DestinationHandler{
		tmpl:        tmpl,
		destService: destService,
	}
}

func (h *DestinationHandler) ExplorePage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	filter := models.DestinationFilter{
		Category: r.URL.Query().Get("category"),
		Region:   r.URL.Query().Get("region"),
		BestFor:  r.URL.Query().Get("best_for"),
		Search:   r.URL.Query().Get("q"),
		SortBy:   r.URL.Query().Get("sort"),
		Limit:    24,
	}

	if month := r.URL.Query().Get("month"); month != "" {
		if m, err := strconv.Atoi(month); err == nil {
			filter.Month = m
		}
	}
	if minBudget := r.URL.Query().Get("min_budget"); minBudget != "" {
		if b, err := strconv.ParseFloat(minBudget, 64); err == nil {
			filter.MinBudget = b
		}
	}
	if maxBudget := r.URL.Query().Get("max_budget"); maxBudget != "" {
		if b, err := strconv.ParseFloat(maxBudget, 64); err == nil {
			filter.MaxBudget = b
		}
	}
	if page := r.URL.Query().Get("page"); page != "" {
		if p, err := strconv.Atoi(page); err == nil && p > 1 {
			filter.Offset = (p - 1) * filter.Limit
		}
	}

	destinations, total, err := h.destService.SearchDestinations(ctx, filter)
	if err != nil || len(destinations) == 0 {
		// DB is empty — seed from Amadeus then re-query (runs once).
		if seedErr := h.destService.SeedFromAmadeus(ctx); seedErr == nil {
			destinations, total, _ = h.destService.SearchDestinations(ctx, filter)
		}
	}

	categories := []string{"beach", "city", "nature", "adventure", "cultural", "romantic", "luxury", "island"}
	regions := []string{"Europe", "Asia", "North America", "South America", "Africa", "Oceania", "Middle East"}

	data := map[string]interface{}{
		"Destinations": destinations,
		"Total":        total,
		"Filter":       filter,
		"Categories":   categories,
		"Regions":      regions,
	}

	if user := getUserFromContext(ctx); user != nil {
		data["User"] = user
	}

	h.tmpl.RenderWithRequest(w, r, "explore.html", data)
}

func (h *DestinationHandler) DestinationPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	dest, err := h.destService.GetDestination(ctx, id)
	if err != nil {
		dest, err = h.destService.GetDestinationByName(ctx, id)
		if err != nil {
			http.Error(w, "Destination not found", http.StatusNotFound)
			return
		}
	}

	// Fetch live highlights from Amadeus if the destination has none stored.
	if len(dest.Highlights) == 0 && dest.Latitude != 0 {
		if highlights, hErr := h.destService.GetHighlights(ctx, dest.Latitude, dest.Longitude); hErr == nil {
			dest.Highlights = highlights
		}
	}

	// Get similar destinations by first category.
	var similar []models.Destination
	if len(dest.Categories) > 0 {
		similar, _ = h.destService.GetDestinationsByCategory(ctx, dest.Categories[0], 4)
	}
	// Fall back to popular destinations if no similar found.
	if len(similar) == 0 {
		popular, _ := h.destService.GetPopularDestinations(ctx, 4)
		for _, d := range popular {
			if d.Name != dest.Name {
				similar = append(similar, d)
			}
		}
	}

	data := map[string]interface{}{
		"Destination": dest,
		"Similar":     similar,
	}

	if user := getUserFromContext(ctx); user != nil {
		data["User"] = user
	}

	h.tmpl.RenderWithRequest(w, r, "destination.html", data)
}

func (h *DestinationHandler) FeaturedAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	limit := 6
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			limit = parsed
		}
	}

	destinations, err := h.destService.GetFeaturedDestinations(ctx, limit)
	if err != nil {
		destinations, _ = h.destService.GetPopularDestinations(ctx, limit)
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{"destinations": destinations})
}

// AdminReseedDestinations re-seeds all destinations from Amadeus + Wikipedia.
// POST /api/v1/admin/destinations/reseed
func (h *DestinationHandler) AdminReseedDestinations(w http.ResponseWriter, r *http.Request) {
	go func() {
		if err := h.destService.SeedFromAmadeus(r.Context()); err != nil {
			return
		}
	}()
	respondJSON(w, http.StatusAccepted, map[string]string{"status": "reseed started in background"})
}

// AdminAddDestination adds any city to the destinations collection by calling Amadeus + Wikipedia.
// POST /api/v1/admin/destinations  {"name":"Lisbon","country_code":"PT","region":"Europe"}
func (h *DestinationHandler) AdminAddDestination(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		CountryCode string `json:"country_code"`
		Region      string `json:"region"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	dest, err := h.destService.AddDestination(r.Context(), body.Name, body.CountryCode, body.Region)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	respondJSON(w, http.StatusCreated, dest)
}

func (h *DestinationHandler) SearchAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	filter := models.DestinationFilter{
		Category: r.URL.Query().Get("category"),
		Region:   r.URL.Query().Get("region"),
		BestFor:  r.URL.Query().Get("best_for"),
		Search:   r.URL.Query().Get("q"),
		SortBy:   r.URL.Query().Get("sort"),
		Limit:    24,
	}

	if limit := r.URL.Query().Get("limit"); limit != "" {
		if l, err := strconv.Atoi(limit); err == nil {
			filter.Limit = l
		}
	}

	destinations, total, _ := h.destService.SearchDestinations(ctx, filter)

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"destinations": destinations,
		"total":        total,
	})
}
