package handler

import (
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

	// Parse filters from query params
	filter := models.DestinationFilter{
		Category:  r.URL.Query().Get("category"),
		Region:    r.URL.Query().Get("region"),
		BestFor:   r.URL.Query().Get("best_for"),
		Search:    r.URL.Query().Get("q"),
		SortBy:    r.URL.Query().Get("sort"),
		Limit:     24,
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

	// Get destinations
	destinations, total, err := h.destService.SearchDestinations(ctx, filter)
	if err != nil || len(destinations) == 0 {
		// Use default destinations if DB is empty or error
		destinations = h.destService.GetDefaultDestinations()
		total = int64(len(destinations))

		// Apply search filter to defaults if provided
		if filter.Search != "" || filter.Category != "" || filter.Region != "" {
			destinations = filterDefaultDestinations(destinations, filter)
			total = int64(len(destinations))
		}
	}

	// Get categories for filter sidebar
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
		// Try by name
		dest, err = h.destService.GetDestinationByName(ctx, id)
		if err != nil {
			// Check default destinations
			defaults := h.destService.GetDefaultDestinations()
			for _, d := range defaults {
				if d.Name == id || d.ID.Hex() == id {
					dest = &d
					break
				}
			}
			if dest == nil {
				http.Error(w, "Destination not found", http.StatusNotFound)
				return
			}
		}
	}

	// Get similar destinations
	similar, _ := h.destService.GetDestinationsByCategory(ctx, dest.Categories[0], 4)
	if len(similar) == 0 {
		defaults := h.destService.GetDefaultDestinations()
		for _, d := range defaults {
			if d.Name != dest.Name && len(similar) < 4 {
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
	if err != nil || len(destinations) == 0 {
		// Return defaults
		defaults := h.destService.GetDefaultDestinations()
		featured := make([]models.Destination, 0, limit)
		for _, d := range defaults {
			if d.Featured && len(featured) < limit {
				featured = append(featured, d)
			}
		}
		respondJSON(w, http.StatusOK, map[string]interface{}{"destinations": featured})
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{"destinations": destinations})
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

	destinations, total, err := h.destService.SearchDestinations(ctx, filter)
	if err != nil || len(destinations) == 0 {
		defaults := h.destService.GetDefaultDestinations()
		destinations = filterDefaultDestinations(defaults, filter)
		total = int64(len(destinations))
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"destinations": destinations,
		"total":        total,
	})
}

// filterDefaultDestinations applies filters to default destinations
func filterDefaultDestinations(destinations []models.Destination, filter models.DestinationFilter) []models.Destination {
	var result []models.Destination

	for _, d := range destinations {
		match := true

		if filter.Search != "" {
			searchLower := toLower(filter.Search)
			if !containsLower(d.Name, searchLower) &&
				!containsLower(d.Country, searchLower) &&
				!containsLower(d.Region, searchLower) {
				match = false
			}
		}

		if filter.Category != "" && match {
			found := false
			for _, c := range d.Categories {
				if c == filter.Category {
					found = true
					break
				}
			}
			if !found {
				match = false
			}
		}

		if filter.Region != "" && match {
			if d.Region != filter.Region {
				match = false
			}
		}

		if filter.BestFor != "" && match {
			found := false
			for _, b := range d.BestFor {
				if b == filter.BestFor {
					found = true
					break
				}
			}
			if !found {
				match = false
			}
		}

		if match {
			result = append(result, d)
		}
	}

	return result
}

func toLower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 32
		}
	}
	return string(b)
}

func containsLower(s, substr string) bool {
	s = toLower(s)
	return len(s) >= len(substr) && (s == substr || containsString(s, substr))
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
