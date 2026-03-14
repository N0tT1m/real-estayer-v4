package handler

import (
	"context"
	"encoding/json"
	"html/template"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/realestayer/v3/internal/config"
	"github.com/realestayer/v3/internal/middleware"
	"github.com/realestayer/v3/internal/service"
	"github.com/realestayer/v3/internal/service/booking"
)

// Handler holds all dependencies for HTTP handlers
type Handler struct {
	config           *config.Config
	authService      *service.AuthService
	userService      *service.UserService
	listingService   *service.ListingService
	scraperService   *service.ScraperService
	tripService      *service.TripService
	watchlistService *service.WatchlistService
	flightService    *booking.FlightService
	hotelService     *booking.HotelService
	carService       *booking.CarService
	templates        map[string]*template.Template // Map of page name to template
}

// NewHandler creates a new handler with all dependencies
func NewHandler(
	cfg *config.Config,
	authService *service.AuthService,
	userService *service.UserService,
	listingService *service.ListingService,
	scraperService *service.ScraperService,
	tripService *service.TripService,
	watchlistService *service.WatchlistService,
	flightService *booking.FlightService,
	hotelService *booking.HotelService,
	carService *booking.CarService,
) *Handler {
	h := &Handler{
		config:           cfg,
		authService:      authService,
		userService:      userService,
		listingService:   listingService,
		scraperService:   scraperService,
		tripService:      tripService,
		watchlistService: watchlistService,
		flightService:    flightService,
		hotelService:     hotelService,
		carService:       carService,
	}

	// Load templates
	h.loadTemplates()

	return h
}

// templateFuncs returns the common template functions
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"json": func(v interface{}) template.JS {
			b, _ := json.Marshal(v)
			return template.JS(b)
		},
		"hasPrefix": strings.HasPrefix,
		"contains": func(slice []int, item int) bool {
			for _, v := range slice {
				if v == item {
					return true
				}
			}
			return false
		},
		"add": func(a, b int) int {
			return a + b
		},
		"subtract": func(a, b int) int {
			return a - b
		},
		"slice": func(start, end int) []int {
			result := make([]int, end-start)
			for i := range result {
				result[i] = start + i
			}
			return result
		},
		// pages generates page numbers for pagination with ellipsis
		// Returns slice of ints where -1 represents ellipsis
		"pages": func(current, total int) []int {
			if total <= 7 {
				// Show all pages
				result := make([]int, total)
				for i := range result {
					result[i] = i + 1
				}
				return result
			}

			var pages []int
			// Always show first page
			pages = append(pages, 1)

			if current > 3 {
				pages = append(pages, -1) // ellipsis
			}

			// Pages around current
			start := current - 1
			end := current + 1
			if start < 2 {
				start = 2
			}
			if end > total-1 {
				end = total - 1
			}

			for i := start; i <= end; i++ {
				pages = append(pages, i)
			}

			if current < total-2 {
				pages = append(pages, -1) // ellipsis
			}

			// Always show last page
			pages = append(pages, total)
			return pages
		},
	}
}

// loadTemplates loads all HTML templates
func (h *Handler) loadTemplates() {
	templateDir := "web/templates"
	h.templates = make(map[string]*template.Template)

	// Get layout files
	layoutPattern := filepath.Join(templateDir, "layouts", "*.html")
	layouts, err := filepath.Glob(layoutPattern)
	if err != nil {
		slog.Error("failed to glob layout templates", "error", err)
		return
	}

	// Get partial files (may be empty)
	partialPattern := filepath.Join(templateDir, "partials", "*.html")
	partials, _ := filepath.Glob(partialPattern)

	// Get all page files
	pagePattern := filepath.Join(templateDir, "pages", "*.html")
	pages, err := filepath.Glob(pagePattern)
	if err != nil {
		slog.Error("failed to glob page templates", "error", err)
		return
	}

	// Create a separate template tree for each page
	for _, page := range pages {
		pageName := filepath.Base(page)

		// Combine: layouts + partials + this specific page
		files := append(layouts, partials...)
		files = append(files, page)

		tmpl, err := template.New(pageName).Funcs(templateFuncs()).ParseFiles(files...)
		if err != nil {
			slog.Error("failed to parse template", "page", pageName, "error", err)
			continue
		}

		h.templates[pageName] = tmpl
		slog.Debug("loaded template", "page", pageName)
	}

	slog.Info("loaded templates", "count", len(h.templates))
}

// render renders a template with data
func (h *Handler) render(w http.ResponseWriter, r *http.Request, name string, data map[string]interface{}) {
	if h.templates == nil {
		slog.Error("templates not loaded")
		http.Error(w, "Templates not loaded", http.StatusInternalServerError)
		return
	}

	tmpl, ok := h.templates[name]
	if !ok {
		slog.Error("template not found", "template", name)
		http.Error(w, "Template not found", http.StatusInternalServerError)
		return
	}

	if data == nil {
		data = make(map[string]interface{})
	}

	// Add common data
	data["User"] = middleware.GetUser(r.Context())
	data["Path"] = r.URL.Path

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Execute the page template (which includes base.html)
	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("failed to render template", "template", name, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// jsonResponse sends a JSON response
func (h *Handler) jsonResponse(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to encode JSON response", "error", err)
	}
}

// jsonError sends a JSON error response
func (h *Handler) jsonError(w http.ResponseWriter, status int, message string) {
	h.jsonResponse(w, status, map[string]string{"error": message})
}

// parseJSON parses JSON request body
func (h *Handler) parseJSON(r *http.Request, v interface{}) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// getUserID returns the current user's ID
func (h *Handler) getUserID(r *http.Request) string {
	return middleware.GetUserID(r.Context())
}

// getUser returns the current user
func (h *Handler) getUser(r *http.Request) interface{} {
	return middleware.GetUser(r.Context())
}

// Templates returns a TemplateRenderer for use by other handlers
func (h *Handler) Templates() *TemplateRenderer {
	return &TemplateRenderer{templates: h.templates}
}

// TemplateRenderer wraps template rendering for shared use
type TemplateRenderer struct {
	templates map[string]*template.Template
}

// Render renders a template with data
func (tr *TemplateRenderer) Render(w http.ResponseWriter, name string, data map[string]interface{}) {
	tr.RenderWithRequest(w, nil, name, data)
}

// RenderWithRequest renders a template with data and request context
func (tr *TemplateRenderer) RenderWithRequest(w http.ResponseWriter, r *http.Request, name string, data map[string]interface{}) {
	if data == nil {
		data = make(map[string]interface{})
	}

	// Add Path if request is provided
	if r != nil {
		data["Path"] = r.URL.Path
	} else if _, ok := data["Path"]; !ok {
		data["Path"] = "" // Default empty string to avoid template error
	}

	tmpl, ok := tr.templates[name]
	if !ok {
		slog.Error("template not found", "template", name)
		http.Error(w, "Template not found", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	if err := tmpl.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("failed to render template", "template", name, "error", err)
		http.Error(w, "Internal Server Error", http.StatusInternalServerError)
	}
}

// respondJSON sends a JSON response
func respondJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// getUserFromContext retrieves user from context
func getUserFromContext(ctx context.Context) interface{} {
	return middleware.GetUser(ctx)
}
