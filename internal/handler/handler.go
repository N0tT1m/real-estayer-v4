package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/realestayer/v4/internal/config"
	"github.com/realestayer/v4/internal/middleware"
	"github.com/realestayer/v4/internal/repository"
	"github.com/realestayer/v4/internal/service"
	"github.com/realestayer/v4/internal/service/booking"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Handler holds every dependency the HTTP handlers need. Dependencies are
// grouped by domain rather than listed flat: with ~45 services a single flat
// struct had to be edited in three places (the struct, the deps bag, and the
// copy block in NewHandler) every time a service was added. Now HandlerDeps is
// embedded directly, so adding a service means adding one field to one group.
type Handler struct {
	HandlerDeps
	templates map[string]*template.Template // Map of page name to template
}

// CoreDeps covers identity, credentials, and cross-cutting infrastructure that
// is not specific to any one feature area.
type CoreDeps struct {
	Auth   *service.AuthService
	Reset  *service.PasswordResetService
	User   *service.UserService
	Users  *repository.UserRepository
	Audit  *service.AuditService
	Photos service.PhotoStorage
}

// ListingDeps covers property search, scraping, and price tracking — the
// "find a place" half of the app.
type ListingDeps struct {
	Listing      *service.ListingService
	Scraper      *service.ScraperService
	Watchlist    *service.WatchlistService
	SavedSearch  *service.SavedSearchService
	PriceHistory *service.PriceHistoryService
	Collections  *repository.CollectionRepository
}

// TripDeps covers trip planning plus everything that hangs off a trip:
// collaboration, expenses, journals, reviews, polls, and the AI/parsing
// helpers that only ever operate on trip data.
type TripDeps struct {
	Trip        *service.TripService
	Comment     *service.TripCommentService
	Expense     *service.TripExpenseService
	Journal     *service.TripJournalService
	Review      *service.TripReviewService
	Poll        *service.PollService
	Stats       *service.TravelStatsService
	Conflict    *service.ConflictChecker
	Notify      *service.NotificationWorker
	EmailParser *service.EmailParserService
	ReceiptOCR  *service.ReceiptOCRService
	AIItinerary *service.AIItineraryService
}

// EnrichDeps covers read-only third-party lookups. These share a shape:
// stateless, cacheable, and degrading to an empty result when the upstream key
// is unset — so they are the group most likely to grow, and the one where a
// new member needs the least thought.
type EnrichDeps struct {
	Weather      *service.WeatherService
	Currency     *service.CurrencyService
	Places       *service.OverpassService
	Events       *service.EventsService
	Country      *service.CountryService
	Sun          *service.SunService
	Air          *service.AirQualityService
	Routing      *service.RoutingService
	Geocoding    *service.GeocodingService
	Advisory     *service.AdvisoryService
	Carbon       *service.CarbonService
	Affiliate    *service.AffiliateService
	Unsplash     *service.UnsplashService
	Airport      *service.AirportService
	FlightStatus *service.FlightStatusService
	Wikidata     *service.WikidataService
	Nature       *service.NatureService
	Visa         *service.VisaService
	Transit      *service.TransitService
}

// HandlerDeps is the dependency bag for NewHandler, and is embedded in Handler
// verbatim. New dependencies go into whichever group they belong to; only add a
// top-level field for something that genuinely fits no group.
type HandlerDeps struct {
	Config   *config.Config
	Core     CoreDeps
	Listings ListingDeps
	Trips    TripDeps
	Enrich   EnrichDeps
	Flight   *booking.FlightService
}

// NewHandler creates a new handler from a dependency bag.
func NewHandler(d HandlerDeps) *Handler {
	h := &Handler{HandlerDeps: d}
	h.loadTemplates()
	return h
}

// templateFuncs returns the common template functions
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		// json serialises a value for embedding in a JS context. json.Marshal
		// alone leaves "<", ">" and "&" untouched, which can break out of
		// <script> blocks; we replace them with \uXXXX escapes. Unicode line
		// separators U+2028 / U+2029 are likewise escaped because they are
		// literal line terminators in JS but not in JSON.
		"json": func(v interface{}) template.JS {
			b, err := json.Marshal(v)
			if err != nil {
				return template.JS("null")
			}
			b = bytes.ReplaceAll(b, []byte("<"), []byte(`\u003c`))
			b = bytes.ReplaceAll(b, []byte(">"), []byte(`\u003e`))
			b = bytes.ReplaceAll(b, []byte("&"), []byte(`\u0026`))
			b = bytes.ReplaceAll(b, []byte("\u2028"), []byte(`\u2028`))
			b = bytes.ReplaceAll(b, []byte("\u2029"), []byte(`\u2029`))
			// #nosec G203 -- <, >, & and U+2028/9 are escaped above before the
			// value is marked as JS; that escaping is the point of this helper.
			return template.JS(b)
		},
		"hasPrefix": strings.HasPrefix,
		// dict builds a map from alternating key/value args, letting templates
		// pass named parameters into {{template "name" (dict "K" v)}}.
		"dict": func(values ...interface{}) (map[string]interface{}, error) {
			if len(values)%2 != 0 {
				return nil, fmt.Errorf("dict requires an even number of args, got %d", len(values))
			}
			m := make(map[string]interface{}, len(values)/2)
			for i := 0; i < len(values); i += 2 {
				key, ok := values[i].(string)
				if !ok {
					return nil, fmt.Errorf("dict keys must be strings, got %T at arg %d", values[i], i)
				}
				m[key] = values[i+1]
			}
			return m, nil
		},
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

// AssetRoot returns the directory containing web/templates and web/static.
//
// Resolution order: ASSETS_DIR, then the working directory, then the directory
// holding the executable. That last step matters on Windows, where the binary
// is typically launched from wherever it happens to sit — there is no systemd
// WorkingDirectory convention — and a wrong CWD would otherwise leave the
// server running with zero templates, 500-ing every page.
func AssetRoot() string {
	if v := strings.TrimSpace(os.Getenv("ASSETS_DIR")); v != "" {
		return v
	}
	if hasTemplateDir(".") {
		return "."
	}
	if exe, err := os.Executable(); err == nil {
		if dir := filepath.Dir(exe); hasTemplateDir(dir) {
			return dir
		}
	}
	return "."
}

func hasTemplateDir(root string) bool {
	_, err := os.Stat(filepath.Join(root, "web", "templates", "layouts"))
	return err == nil
}

// loadTemplates loads all HTML templates
func (h *Handler) loadTemplates() {
	templateDir := filepath.Join(AssetRoot(), "web", "templates")
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

	// Booting with zero templates yields a server that starts cleanly and then
	// 500s on every page — the worst failure mode. Fail at startup instead,
	// consistent with how config and database errors are handled in main.
	if len(h.templates) == 0 {
		slog.Error("no templates were loaded; every page would fail",
			"template_dir", templateDir,
			"hint", "run from the project root or set ASSETS_DIR to the directory containing web/")
		os.Exit(1)
	}

	slog.Info("loaded templates", "count", len(h.templates), "dir", templateDir)
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
	data["CSRFToken"] = middleware.CSRFToken(r.Context())
	data["MapStyleURL"] = h.mapStyleURL()

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

// getUserOID returns the current user's Mongo ObjectID. Returns NilObjectID
// when no session is attached or the hex is malformed; callers that need an
// ID should usually be behind RequireAuth, so the nil case is a safety net.
func (h *Handler) getUserOID(r *http.Request) primitive.ObjectID {
	id := h.getUserID(r)
	if id == "" {
		return primitive.NilObjectID
	}
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return primitive.NilObjectID
	}
	return oid
}

// getUser returns the current user
func (h *Handler) getUser(r *http.Request) interface{} {
	return middleware.GetUser(r.Context())
}

// Templates returns a TemplateRenderer for use by other handlers
func (h *Handler) Templates() *TemplateRenderer {
	return &TemplateRenderer{templates: h.templates, mapStyleURL: h.mapStyleURL()}
}

// mapStyleURL chooses the MapLibre style URL for the whole app. Uses MapTiler
// Streets when a key is set; falls back to CARTO's free Voyager basemap (no
// key required but intended only for low-traffic / dev use).
func (h *Handler) mapStyleURL() string {
	if h.Config != nil && h.Config.MapTilerKey != "" {
		return "https://api.maptiler.com/maps/streets-v2/style.json?key=" + h.Config.MapTilerKey
	}
	return "https://basemaps.cartocdn.com/gl/voyager-gl-style/style.json"
}

// TemplateRenderer wraps template rendering for shared use
type TemplateRenderer struct {
	templates   map[string]*template.Template
	mapStyleURL string
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
		data["User"] = middleware.GetUser(r.Context())
		data["CSRFToken"] = middleware.CSRFToken(r.Context())
	} else {
		if _, ok := data["Path"]; !ok {
			data["Path"] = ""
		}
		if _, ok := data["CSRFToken"]; !ok {
			data["CSRFToken"] = ""
		}
	}
	if _, ok := data["MapStyleURL"]; !ok {
		data["MapStyleURL"] = tr.mapStyleURL
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
	_ = json.NewEncoder(w).Encode(data)
}

// Note: there is deliberately no getUserFromContext helper returning
// interface{} here. Boxing middleware.GetUser's *models.User into an
// interface makes a nil user compare non-nil, so `user != nil` guards on the
// result are always true. Call middleware.GetUser directly and compare the
// typed pointer.
