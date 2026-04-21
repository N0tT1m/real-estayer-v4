package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"

	"github.com/realestayer/v4/internal/config"
	"github.com/realestayer/v4/internal/middleware"
	"github.com/realestayer/v4/internal/repository"
	"github.com/realestayer/v4/internal/service"
	"github.com/realestayer/v4/internal/service/booking"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Handler holds all dependencies for HTTP handlers
type Handler struct {
	config             *config.Config
	authService        *service.AuthService
	resetService       *service.PasswordResetService
	userService        *service.UserService
	listingService     *service.ListingService
	scraperService     *service.ScraperService
	tripService        *service.TripService
	watchlistService   *service.WatchlistService
	savedSearchSvc     *service.SavedSearchService
	priceHistorySvc    *service.PriceHistoryService
	commentService     *service.TripCommentService
	expenseService     *service.TripExpenseService
	journalService     *service.TripJournalService
	reviewService      *service.TripReviewService
	weatherService     *service.WeatherService
	currencyService    *service.CurrencyService
	placesService      *service.OverpassService
	eventsService      *service.EventsService
	aiItineraryService *service.AIItineraryService
	countryService     *service.CountryService
	sunService         *service.SunService
	airService         *service.AirQualityService
	routingService     *service.RoutingService
	geocodingService   *service.GeocodingService
	advisoryService    *service.AdvisoryService
	carbonService      *service.CarbonService
	statsService       *service.TravelStatsService
	affiliateService   *service.AffiliateService
	unsplashService    *service.UnsplashService
	airportService     *service.AirportService
	flightStatusSvc    *service.FlightStatusService
	wikidataService    *service.WikidataService
	natureService      *service.NatureService
	bookingPartner     *service.BookingPartnerService
	expediaPartner     *service.ExpediaPartnerService
	emailParser        *service.EmailParserService
	conflictChecker    *service.ConflictChecker
	visaService        *service.VisaService
	notifyWorker       *service.NotificationWorker
	photoStorage       service.PhotoStorage
	transitService     *service.TransitService
	pollService        *service.PollService
	receiptOCR         *service.ReceiptOCRService
	carAffiliates      *service.CarAffiliateService
	auditService       *service.AuditService
	users              *repository.UserRepository
	collections        *repository.CollectionRepository
	flightService      *booking.FlightService
	hotelService       *booking.HotelService
	carService         *booking.CarService
	templates          map[string]*template.Template // Map of page name to template
}

// HandlerDeps is the dependency bag for NewHandler. New fields are added here
// rather than expanding the positional constructor.
type HandlerDeps struct {
	Config         *config.Config
	AuthService    *service.AuthService
	ResetService   *service.PasswordResetService
	UserService    *service.UserService
	ListingService *service.ListingService
	ScraperService *service.ScraperService
	TripService    *service.TripService
	Watchlist      *service.WatchlistService
	SavedSearch    *service.SavedSearchService
	PriceHistory   *service.PriceHistoryService
	TripComment    *service.TripCommentService
	TripExpense    *service.TripExpenseService
	TripJournal    *service.TripJournalService
	TripReview     *service.TripReviewService
	Weather        *service.WeatherService
	Currency       *service.CurrencyService
	Places         *service.OverpassService
	Events         *service.EventsService
	AIItinerary    *service.AIItineraryService
	Country        *service.CountryService
	Sun            *service.SunService
	Air            *service.AirQualityService
	Routing        *service.RoutingService
	Geocoding      *service.GeocodingService
	Advisory       *service.AdvisoryService
	Carbon         *service.CarbonService
	Stats          *service.TravelStatsService
	Affiliate      *service.AffiliateService
	Unsplash       *service.UnsplashService
	Airport        *service.AirportService
	FlightStatus   *service.FlightStatusService
	Wikidata       *service.WikidataService
	Nature         *service.NatureService
	BookingPartner *service.BookingPartnerService
	ExpediaPartner *service.ExpediaPartnerService
	EmailParser    *service.EmailParserService
	Conflict       *service.ConflictChecker
	Visa           *service.VisaService
	Notify         *service.NotificationWorker
	Photos         service.PhotoStorage
	Transit        *service.TransitService
	Poll           *service.PollService
	ReceiptOCR     *service.ReceiptOCRService
	CarAffiliates  *service.CarAffiliateService
	Audit          *service.AuditService
	Users          *repository.UserRepository
	Collections    *repository.CollectionRepository
	Flight         *booking.FlightService
	Hotel          *booking.HotelService
	Car            *booking.CarService
}

// NewHandler creates a new handler from a dependency bag.
func NewHandler(d HandlerDeps) *Handler {
	h := &Handler{
		config:             d.Config,
		authService:        d.AuthService,
		resetService:       d.ResetService,
		userService:        d.UserService,
		listingService:     d.ListingService,
		scraperService:     d.ScraperService,
		tripService:        d.TripService,
		watchlistService:   d.Watchlist,
		savedSearchSvc:     d.SavedSearch,
		priceHistorySvc:    d.PriceHistory,
		commentService:     d.TripComment,
		expenseService:     d.TripExpense,
		journalService:     d.TripJournal,
		reviewService:      d.TripReview,
		weatherService:     d.Weather,
		currencyService:    d.Currency,
		placesService:      d.Places,
		eventsService:      d.Events,
		aiItineraryService: d.AIItinerary,
		countryService:     d.Country,
		sunService:         d.Sun,
		airService:         d.Air,
		routingService:     d.Routing,
		geocodingService:   d.Geocoding,
		advisoryService:    d.Advisory,
		carbonService:      d.Carbon,
		statsService:       d.Stats,
		affiliateService:   d.Affiliate,
		unsplashService:    d.Unsplash,
		airportService:     d.Airport,
		flightStatusSvc:    d.FlightStatus,
		wikidataService:    d.Wikidata,
		natureService:      d.Nature,
		bookingPartner:     d.BookingPartner,
		expediaPartner:     d.ExpediaPartner,
		emailParser:        d.EmailParser,
		conflictChecker:    d.Conflict,
		visaService:        d.Visa,
		notifyWorker:       d.Notify,
		photoStorage:       d.Photos,
		transitService:     d.Transit,
		pollService:        d.Poll,
		receiptOCR:         d.ReceiptOCR,
		carAffiliates:      d.CarAffiliates,
		auditService:       d.Audit,
		users:              d.Users,
		collections:        d.Collections,
		flightService:      d.Flight,
		hotelService:       d.Hotel,
		carService:         d.Car,
	}
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
	if h.config != nil && h.config.MapTilerKey != "" {
		return "https://api.maptiler.com/maps/streets-v2/style.json?key=" + h.config.MapTilerKey
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
	json.NewEncoder(w).Encode(data)
}

// getUserFromContext retrieves user from context
func getUserFromContext(ctx context.Context) interface{} {
	return middleware.GetUser(ctx)
}
