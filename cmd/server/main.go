package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/realestayer/v4/internal/config"
	"github.com/realestayer/v4/internal/crypto"
	"github.com/realestayer/v4/internal/database"
	"github.com/realestayer/v4/internal/handler"
	"github.com/realestayer/v4/internal/mailer"
	authMiddleware "github.com/realestayer/v4/internal/middleware"
	"github.com/realestayer/v4/internal/migrations"
	"github.com/realestayer/v4/internal/provider"
	"github.com/realestayer/v4/internal/provider/duffel"
	"github.com/realestayer/v4/internal/provider/wikipedia"
	"github.com/realestayer/v4/internal/repository"
	"github.com/realestayer/v4/internal/service"
	"github.com/realestayer/v4/internal/service/booking"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}
	cfg.LogFeatureSummary()

	db, err := database.Connect(cfg.MongoURI, cfg.MongoDatabase)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := db.Disconnect(ctx); err != nil {
			slog.Warn("mongo disconnect failed", "error", err)
		}
	}()

	// Run pending schema migrations before anything else touches the DB so
	// handlers never observe a half-migrated state. Small deployments run
	// the server directly (no separate `migrate` step); a dedicated binary
	// still exists for teams that prefer to split migration from release.
	migrateCtx, cancelMigrate := context.WithTimeout(context.Background(), 30*time.Minute)
	if err := migrations.Run(migrateCtx, db); err != nil {
		cancelMigrate()
		slog.Error("migrations failed", "error", err)
		os.Exit(1)
	}
	cancelMigrate()

	repos := repository.NewRepositories(db)

	providerRegistry := provider.NewRegistry()

	// Duffel is the flight provider. When no token is set the client returns
	// ErrNotConfigured for every call; we still register it so
	// `/api/v1/providers` and future fallback logic can see it.
	duffelClient := duffel.NewClient(cfg.Duffel.AccessToken, cfg.Duffel.BaseURL)
	providerRegistry.RegisterFlight("duffel", duffelClient)

	destRepo := repository.NewDestinationRepository(db.Database)

	mailerClient := mailer.New(mailer.Config{
		Host:        cfg.Email.SMTPHost,
		Port:        cfg.Email.SMTPPort,
		Username:    cfg.Email.SMTPUser,
		Password:    cfg.Email.SMTPPassword,
		FromAddress: cfg.Email.FromAddress,
		FromName:    cfg.Email.FromName,
		ImplicitTLS: cfg.Email.ImplicitTLS,
	})

	authService := service.NewAuthService(repos.User, repos.Session, cfg.SessionSecret, cfg)
	resetService := service.NewPasswordResetService(repos.User, repos.Session, repos.PasswordReset, mailerClient, cfg.SessionSecret)
	fieldCipher, err := crypto.NewFieldCipher(cfg.FieldEncryptionKey)
	if err != nil {
		slog.Error("field encryption key invalid", "error", err)
		os.Exit(1)
	}
	userService := service.NewUserService(repos.User, fieldCipher)
	listingService := service.NewListingService(repos.Listing)
	scraperService := service.NewScraperService(cfg.ScraperURL, cfg.ScraperAPIKey)
	tripService := service.NewTripService(repos.Trip)
	watchlistService := service.NewWatchlistService(repos.Watchlist)
	priceHistoryService := service.NewPriceHistoryService(repos.PriceHistory)
	savedSearchService := service.NewSavedSearchService(repos.SavedSearch, listingService, repos.User, cfg.DiscordWebhookURL)
	commentService := service.NewTripCommentService(tripService, repos.TripComment, repos.User)
	expenseService := service.NewTripExpenseService(tripService, repos.TripExpense, repos.User)
	journalService := service.NewTripJournalService(tripService, repos.TripJournal, repos.User)
	reviewService := service.NewTripReviewService(tripService, repos.TripReview)
	weatherService := service.NewWeatherService()
	currencyService := service.NewCurrencyService()
	placesService := service.NewOverpassService()
	eventsService := service.NewEventsService(cfg.TicketmasterAPIKey)
	aiItineraryService := service.NewAIItineraryService(cfg.AnthropicAPIKey, cfg.AIModel, cfg.AIBaseURL)
	countryService := service.NewCountryService()
	sunService := service.NewSunService()
	airService := service.NewAirQualityService(cfg.OpenAQAPIKey)
	routingService := service.NewRoutingService()
	geocodingService := service.NewGeocodingService()
	advisoryService := service.NewAdvisoryService()
	carbonService := service.NewCarbonService()
	statsService := service.NewTravelStatsService(repos.Trip, repos.TripExpense)
	affiliateService := service.NewAffiliateService(cfg.AffiliateTag)
	unsplashService := service.NewUnsplashService(cfg.UnsplashKey)
	airportService := service.NewAirportService()
	destService := service.NewDestinationService(destRepo, geocodingService, airportService)
	flightStatusService := service.NewFlightStatusService(cfg.AviationStackAPIKey)
	wikidataService := service.NewWikidataService()
	natureService := service.NewNatureService(cfg.EBirdAPIKey)
	emailParser := service.NewEmailParserService(aiItineraryService)
	conflictChecker := service.NewConflictChecker()
	visaService := service.NewVisaService()
	photoStorage := service.NewPhotoStorage()
	transitService := service.NewTransitService(cfg.GoogleDirectionsKey, routingService)
	pollService := service.NewPollService(repos.Poll, repos.User)
	receiptOCR := service.NewReceiptOCRService(aiItineraryService)
	auditService := service.NewAuditService(repos.Audit)

	// Public URL used in notification emails — strip trailing slash once.
	appBase := cfg.AppBaseURL
	if appBase == "" {
		appBase = "http://localhost:" + cfg.Port
	}
	notifyWorker := service.NewNotificationWorker(
		repos.User, repos.Trip, repos.Watchlist, repos.PriceHistory,
		flightStatusService, mailerClient, appBase,
	)

	flightService := booking.NewFlightService(providerRegistry, repos.Booking)

	h := handler.NewHandler(handler.HandlerDeps{
		Config:         cfg,
		AuthService:    authService,
		ResetService:   resetService,
		UserService:    userService,
		ListingService: listingService,
		ScraperService: scraperService,
		TripService:    tripService,
		Watchlist:      watchlistService,
		SavedSearch:    savedSearchService,
		PriceHistory:   priceHistoryService,
		TripComment:    commentService,
		TripExpense:    expenseService,
		TripJournal:    journalService,
		TripReview:     reviewService,
		Weather:        weatherService,
		Currency:       currencyService,
		Places:         placesService,
		Events:         eventsService,
		AIItinerary:    aiItineraryService,
		Country:        countryService,
		Sun:            sunService,
		Air:            airService,
		Routing:        routingService,
		Geocoding:      geocodingService,
		Advisory:       advisoryService,
		Carbon:         carbonService,
		Stats:          statsService,
		Affiliate:      affiliateService,
		Unsplash:       unsplashService,
		Airport:        airportService,
		FlightStatus:   flightStatusService,
		Wikidata:       wikidataService,
		Nature:         natureService,
		EmailParser:    emailParser,
		Conflict:       conflictChecker,
		Visa:           visaService,
		Notify:         notifyWorker,
		Photos:         photoStorage,
		Transit:        transitService,
		Poll:           pollService,
		ReceiptOCR:     receiptOCR,
		Audit:          auditService,
		Users:          repos.User,
		Collections:    repos.Collection,
		Flight:         flightService,
	})

	discoveryService := service.NewDestinationDiscoveryService(destRepo, wikidataService, wikipedia.NewClient())
	destHandler := handler.NewDestinationHandler(h.Templates(), destService, discoveryService, scraperService)

	r := chi.NewRouter()

	metrics := authMiddleware.NewMetrics()
	errorHook := authMiddleware.WebhookErrorHook(cfg.ErrorWebhookURL, nil)

	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(authMiddleware.RequestLogger())
	r.Use(metrics.Middleware())
	r.Use(authMiddleware.Recoverer(h.ServerError, errorHook))
	r.Use(middleware.Timeout(60 * time.Second))

	allowedOrigins := cfg.AllowedOrigins
	if len(allowedOrigins) == 0 {
		allowedOrigins = []string{"http://localhost:8347"}
	}
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Issue/verify CSRF tokens globally; safe methods just get a cookie.
	r.Use(authMiddleware.CSRF(cfg.SessionSecret, cfg.IsProduction()))

	// Static files: resolve once to a clean root to prevent directory traversal.
	staticRoot, err := filepath.Abs(filepath.Join(handler.AssetRoot(), "web", "static"))
	if err != nil {
		slog.Error("failed to resolve static root", "error", err)
		os.Exit(1)
	}
	fileServer := http.FileServer(http.Dir(staticRoot))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// User-uploaded photos live outside the bundled static dir so volume
	// mounts work. Only served when the local backend is in use; S3 backend
	// returns absolute URLs and never hits this handler.
	uploadsDir := os.Getenv("UPLOADS_DIR")
	if uploadsDir == "" {
		uploadsDir = "data/uploads"
	}
	if absUploads, err := filepath.Abs(uploadsDir); err == nil {
		uploadsFS := http.FileServer(http.Dir(absUploads))
		r.Handle("/uploads/*", http.StripPrefix("/uploads/", uploadsFS))
	}

	r.Get("/health", h.Health)
	if cfg.MetricsEnabled {
		r.Get("/metrics", metrics.MetricsHandler())
	}
	r.NotFound(h.NotFound)
	r.With(authMiddleware.OptionalAuth(authService)).Get("/", h.Home)

	// Rate-limited auth endpoints (10/min per IP, burst 5) to blunt brute force.
	authLimiter := authMiddleware.NewRateLimiter(cfg.RedisURL, "auth", 10, 5).Middleware()
	// Password reset is much stricter: 3/min per IP, burst 2 (effective 5 / 60s).
	// Generating tokens and sending emails is expensive, and a loose limit here
	// lets an attacker spam reset emails to known addresses.
	resetLimiter := authMiddleware.NewRateLimiter(cfg.RedisURL, "pwreset", 3, 2).Middleware()
	r.Route("/auth", func(r chi.Router) {
		r.Get("/login", h.LoginPage)
		r.Get("/register", h.RegisterPage)
		r.With(authLimiter).Post("/login", h.Login)
		r.With(authLimiter).Post("/register", h.Register)
		r.Post("/logout", h.Logout)
		r.Get("/forgot", h.ForgotPasswordPage)
		r.With(resetLimiter).Post("/forgot", h.RequestPasswordReset)
		r.Get("/reset", h.ResetPasswordPage)
		r.With(resetLimiter).Post("/reset", h.ResetPassword)
	})

	r.Group(func(r chi.Router) {
		r.Use(authMiddleware.OptionalAuth(authService))

		r.Get("/listings", h.ListingsPage)
		r.Get("/listings/{id}", h.ListingDetailPage)
		r.Get("/flights", h.FlightsPage)
		r.Get("/scrape", h.ScrapePage)

		r.Get("/explore", destHandler.ExplorePage)
		r.Get("/explore/{id}", destHandler.DestinationPage)

		r.Get("/trips/shared/{slug}", h.SharedTripPage)
		r.Get("/around-me", h.AroundMePage)
		r.Get("/collections/{slug}", h.CollectionPage)

		// Public polls — anyone with the slug can view + vote.
		r.Get("/polls/{slug}", h.PublicPollPage)
		r.With(authLimiter).Post("/api/public/polls/{slug}/vote", h.PublicPollVote)
	})

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/health", h.HealthAPI)
		r.Get("/listings", h.GetListings)
		r.Get("/listings/{id}", h.GetListing)
		r.Get("/listings/search", h.SearchListings)

		r.Get("/locations/airports", h.SearchAirports)

		r.Get("/destinations", destHandler.SearchAPI)
		r.Get("/destinations/featured", destHandler.FeaturedAPI)

		r.Get("/scraper/status", h.PublicScraperStatus)
		r.With(authLimiter).Post("/scraper/scrape", h.PublicTriggerScrape)

		// Public discovery endpoints
		r.Get("/places/nearby", h.PlacesNearby)
		r.Get("/events/nearby", h.EventsNearby)
		r.Get("/collections", h.CollectionsForDestination)
		r.Get("/collections/featured", h.CollectionsFeatured)

		// Enrichment — all keyless/free unless otherwise noted
		r.Get("/countries/{code}", h.CountryBasics)
		r.Get("/countries/{code}/holidays", h.CountryHolidays)
		r.Get("/countries/{code}/advisory", h.CountryAdvisory)
		r.Get("/sun", h.SunTimes)
		r.Get("/air", h.AirQuality)
		r.Get("/directions", h.Directions)
		r.Get("/geocode", h.Geocode)
		r.Get("/climate", h.ClimateNormals)
		r.Get("/affiliate", h.AffiliateLinks)
		r.Get("/unsplash", h.UnsplashHero)

		// Airports + flight status + city facts + nature
		r.Get("/airports/search", h.AirportSearch)
		r.Get("/airports/distance", h.AirportDistance)
		r.Get("/airports/{iata}", h.AirportLookup)
		r.Get("/flights/status", h.FlightStatus)
		r.Get("/cities/facts", h.CityFacts)
		r.Get("/nature/nearby", h.NearbyNature)
		r.Get("/birds/nearby", h.NearbyBirds)

		// Visa (public reference)
		r.Get("/visa/{destination}", h.VisaCheck)
		// Transit routing (public; Google key, if any, is held server-side)
		r.Get("/transit", h.Transit)

		r.Get("/flights/search", h.SearchFlights)
		r.Get("/flights/offers/{id}", h.GetFlightOffer)

		r.Group(func(r chi.Router) {
			r.Use(authMiddleware.RequireAuth(authService))

			// Discord relays: these spend a shared, operator-owned webhook, so
			// they require a session and carry the auth limiter to keep an
			// authenticated client from flooding the channel.
			r.With(authLimiter).Post("/send-to-discord", h.SendToDiscord)
			r.With(authLimiter).Post("/test-discord", h.TestDiscord)

			r.Get("/users/me", h.GetCurrentUser)
			r.Put("/users/me", h.UpdateUser)
			r.Put("/users/me/password", h.ChangePassword)
			r.Put("/users/me/notifications", h.UpdateNotifications)
			r.Post("/users/me/notifications/test-discord", h.TestDiscordWebhook)

			// Two-factor auth
			r.Post("/users/me/totp/start", h.StartTOTP)
			r.Post("/users/me/totp/confirm", h.ConfirmTOTP)
			r.Post("/users/me/totp/disable", h.DisableTOTP)

			r.Post("/flights/book", h.BookFlight)
			r.Get("/bookings", h.GetUserBookings)
			r.Get("/bookings/{id}", h.GetBooking)

			r.Get("/trips", h.GetTrips)
			r.Post("/trips", h.CreateTrip)
			r.Get("/trips/{id}", h.GetTrip)
			r.Put("/trips/{id}", h.UpdateTrip)
			r.Delete("/trips/{id}", h.DeleteTrip)
			r.Post("/trips/{id}/items", h.AddTripItem)
			r.Delete("/trips/{id}/items/{itemId}", h.RemoveTripItem)

			r.Get("/watchlist", h.GetWatchlist)
			r.Post("/watchlist", h.AddToWatchlist)
			r.Delete("/watchlist/{id}", h.RemoveFromWatchlist)
			r.Get("/watchlist/{id}/history.svg", h.WatchlistPriceHistorySVG)

			r.Get("/saved-searches", h.ListSavedSearches)
			r.Post("/saved-searches", h.CreateSavedSearch)
			r.Delete("/saved-searches/{id}", h.DeleteSavedSearch)

			r.Post("/trips/{id}/share", h.CreateTripShare)
			r.Delete("/trips/{id}/share", h.RevokeTripShare)

			// Enhanced trip features
			r.Put("/trips/{id}/reorder", h.TripReorderItems)
			r.Post("/trips/{id}/clone", h.TripClone)
			r.Post("/trips/{id}/collaborators", h.TripAddCollaborator)
			r.Delete("/trips/{id}/collaborators/{userId}", h.TripRemoveCollaborator)
			r.Put("/trips/{id}/packing", h.TripSetPackingList)
			r.Get("/trips/{id}/packing/suggest", h.TripSuggestPacking)
			r.Put("/trips/{id}/checklist", h.TripSetChecklist)
			r.Get("/trips/{id}/checklist/suggest", h.TripSuggestChecklist)
			r.Get("/trips/{id}/comments", h.TripListComments)
			r.Post("/trips/{id}/comments", h.TripAddComment)
			r.Delete("/trips/{id}/comments/{commentId}", h.TripDeleteComment)
			r.Get("/trips/{id}/expenses", h.TripListExpenses)
			r.Post("/trips/{id}/expenses", h.TripAddExpense)
			r.Delete("/trips/{id}/expenses/{expenseId}", h.TripDeleteExpense)
			r.Get("/trips/{id}/budget", h.TripBudgetSummary)
			r.Get("/trips/{id}/settle", h.TripSettleUp)
			r.Get("/trips/{id}/journal", h.TripListJournal)
			r.Post("/trips/{id}/journal", h.TripAddJournal)
			r.Delete("/trips/{id}/journal/{entryId}", h.TripDeleteJournal)
			r.Get("/trips/{id}/reviews", h.TripListReviews)
			r.Put("/trips/{id}/reviews", h.TripUpsertReview)
			r.Get("/trips/{id}/weather", h.TripWeather)
			r.Post("/ai/itinerary", h.AIItinerary)
			r.Post("/currency/convert", h.ConvertCurrency)
			r.Get("/trips/{id}/carbon", h.TripCarbon)
			r.Get("/me/travel-stats", h.UserTravelStats)
			r.Get("/me/travel-profile", h.UserTravelProfile)

			// Email confirmation parser
			r.Post("/trips/{id}/import-email", h.ImportEmail)

			// Itinerary conflict checker
			r.Get("/trips/{id}/conflicts", h.TripConflicts)

			// Traveler identity (KTN/loyalty/emergency contact)
			r.Put("/users/me/identity", h.UpdateTravelerIdentity)

			// Photo uploads + receipt OCR
			r.Post("/uploads/photo", h.UploadPhoto)
			r.Post("/receipts/ocr", h.ReceiptOCRUpload)

			// Availability polls
			r.Post("/polls", h.CreatePoll)
			r.Get("/polls", h.ListUserPolls)
			r.Delete("/polls/{id}", h.DeletePoll)

			// AI itinerary refinement
			r.Post("/ai/itinerary/refine", h.AIItineraryRefine)
		})

		r.Route("/admin", func(r chi.Router) {
			r.Use(authMiddleware.RequireAuth(authService))
			r.Use(authMiddleware.RequireAdmin)

			r.Get("/dashboard", h.AdminDashboard)
			r.Get("/users", h.AdminListUsers)
			r.Put("/users/{id}/role", h.AdminUpdateUserRole)
			r.Get("/scraping/status", h.ScrapingStatus)
			r.Post("/scraping/trigger", h.TriggerScraping)
			r.Post("/scraping/trigger-region", h.TriggerRegionScrape)
			r.Delete("/listings/{id}", h.AdminDeleteListing)
			r.Post("/destinations", destHandler.AdminAddDestination)
			r.Post("/destinations/reseed", destHandler.AdminReseedDestinations)
			r.Post("/destinations/discover", destHandler.AdminDiscoverDestinations)
			r.Post("/destinations/discover/confirm", destHandler.AdminConfirmDiscovered)
		})
	})

	r.Group(func(r chi.Router) {
		r.Use(authMiddleware.RequireAuth(authService))

		r.Get("/dashboard", h.DashboardPage)
		r.Get("/travel-stats", h.TravelStatsPage)
		r.Get("/trips", h.TripsPage)
		r.Get("/trips/calendar", h.TripCalendarPage)
		r.Get("/trips/{id}", h.TripDetailPage)
		r.Get("/trips/{id}/print", h.PrintableTripPage)
		r.Get("/trips/{id}.ics", h.TripICS)
		r.Get("/watchlist", h.WatchlistPage)
		r.Get("/profile", h.ProfilePage)
		r.Get("/profile/security", h.SecurityPage)
		r.Get("/bookings", h.BookingsPage)
		r.Get("/flights/book", h.FlightBookingPage)
		r.Get("/bookings/confirmation", h.BookingConfirmationPage)
	})

	r.Group(func(r chi.Router) {
		r.Use(authMiddleware.RequireAuth(authService))
		r.Use(authMiddleware.RequireAdmin)

		r.Get("/admin", h.AdminPage)
	})

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Background workers tied to server lifecycle so shutdown cancels them.
	bgCtx, cancelBG := context.WithCancel(context.Background())
	defer cancelBG()

	go func() {
		// The seed pulls a catalog of destinations once at boot.
		// Cap it so a stuck upstream can't keep this goroutine alive through
		// shutdown — bgCtx cancel covers the happy exit path, but a hung
		// TLS handshake without a deadline would ignore it.
		seedCtx, cancel := context.WithTimeout(bgCtx, 10*time.Minute)
		defer cancel()
		if err := destService.SeedDestinations(seedCtx); err != nil {
			slog.Warn("destination seed failed", "error", err)
		}
	}()

	// Saved-search worker: check every 15 minutes whether any searches are
	// due (hourly cadence). Runs inline via the service.
	go func() {
		ticker := time.NewTicker(15 * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-bgCtx.Done():
				return
			case <-ticker.C:
				// Per-tick deadline so a stuck HTTP call to an affiliate
				// can't pile tick goroutines on top of each other.
				tickCtx, cancel := context.WithTimeout(bgCtx, 10*time.Minute)
				savedSearchService.RunDue(tickCtx)
				cancel()
			}
		}
	}()

	// Notification worker: every hour, send trip reminders, price-drop
	// alerts, flight delays, and the Monday-morning digest. Each pass
	// deduplicates via the `sent` map on User so cadence doesn't matter
	// beyond "at least daily."
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-bgCtx.Done():
				return
			case <-ticker.C:
				runCtx, cancel := context.WithTimeout(bgCtx, 30*time.Minute)
				summary := notifyWorker.Run(runCtx)
				cancel()
				if summary.TripReminders+summary.PriceDrops+summary.FlightAlerts+summary.WeeklyDigests > 0 {
					slog.Info("notifications sent",
						"reminders", summary.TripReminders,
						"price_drops", summary.PriceDrops,
						"flight_alerts", summary.FlightAlerts,
						"weekly_digests", summary.WeeklyDigests)
				}
			}
		}
	}()

	go func() {
		slog.Info("server starting", "port", cfg.Port, "env", cfg.Env)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server...")
	cancelBG()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
	}

	slog.Info("server stopped")
}
