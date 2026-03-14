package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/realestayer/v3/internal/config"
	"github.com/realestayer/v3/internal/database"
	"github.com/realestayer/v3/internal/handler"
	authMiddleware "github.com/realestayer/v3/internal/middleware"
	"github.com/realestayer/v3/internal/provider"
	"github.com/realestayer/v3/internal/provider/amadeus"
	"github.com/realestayer/v3/internal/repository"
	"github.com/realestayer/v3/internal/service"
	"github.com/realestayer/v3/internal/service/booking"
)

func main() {
	// Initialize structured logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	// Load configuration
	cfg, err := config.Load()
	if err != nil {
		slog.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Connect to MongoDB
	db, err := database.Connect(cfg.MongoURI)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Disconnect(context.Background())

	// Initialize repositories
	repos := repository.NewRepositories(db)

	// Initialize providers
	providerRegistry := provider.NewRegistry()

	// Register Amadeus provider
	amadeusClient := amadeus.NewClient(cfg.Amadeus.ClientID, cfg.Amadeus.ClientSecret, cfg.Amadeus.BaseURL)
	providerRegistry.RegisterFlight("amadeus", amadeusClient)
	providerRegistry.RegisterHotel("amadeus", amadeusClient)
	providerRegistry.RegisterCar("amadeus", amadeusClient)

	// Initialize destination repository
	destRepo := repository.NewDestinationRepository(db.Database)

	// Initialize services
	authService := service.NewAuthService(repos.User, repos.Session, cfg.SessionSecret)
	userService := service.NewUserService(repos.User)
	listingService := service.NewListingService(repos.Listing)
	scraperService := service.NewScraperService(cfg.ScraperURL)
	tripService := service.NewTripService(repos.Trip)
	watchlistService := service.NewWatchlistService(repos.Watchlist)
	destService := service.NewDestinationService(destRepo, amadeusClient)

	// Initialize booking services
	flightService := booking.NewFlightService(providerRegistry, repos.Booking)
	hotelService := booking.NewHotelService(providerRegistry, repos.Booking)
	carService := booking.NewCarService(providerRegistry, repos.Booking)

	// Initialize handlers
	h := handler.NewHandler(
		cfg,
		authService,
		userService,
		listingService,
		scraperService,
		tripService,
		watchlistService,
		flightService,
		hotelService,
		carService,
	)

	// Initialize destination handler
	destHandler := handler.NewDestinationHandler(h.Templates(), destService)

	// Set up router
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Serve static files
	fileServer := http.FileServer(http.Dir("web/static"))
	r.Handle("/static/*", http.StripPrefix("/static/", fileServer))

	// Public routes
	r.Get("/health", h.Health)
	r.With(authMiddleware.OptionalAuth(authService)).Get("/", h.Home)

	// Auth routes
	r.Route("/auth", func(r chi.Router) {
		r.Get("/login", h.LoginPage)
		r.Get("/register", h.RegisterPage)
		r.Post("/login", h.Login)
		r.Post("/register", h.Register)
		r.Post("/logout", h.Logout)
	})

	// Public browsing (with optional auth so logged-in users stay logged in)
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware.OptionalAuth(authService))

		r.Get("/listings", h.ListingsPage)
		r.Get("/listings/{id}", h.ListingDetailPage)
		r.Get("/flights", h.FlightsPage)
		r.Get("/hotels", h.HotelsPage)
		r.Get("/cars", h.CarsPage)
		r.Get("/scrape", h.ScrapePage)

		// Explore destinations
		r.Get("/explore", destHandler.ExplorePage)
		r.Get("/explore/{id}", destHandler.DestinationPage)
	})

	// API routes
	r.Route("/api/v1", func(r chi.Router) {
		// Public API
		r.Get("/health", h.HealthAPI)
		r.Get("/listings", h.GetListings)
		r.Get("/listings/{id}", h.GetListing)
		r.Get("/listings/search", h.SearchListings)

		// Location search
		r.Get("/locations/airports", h.SearchAirports)
		r.Get("/locations/cities", h.SearchCities)

		// Destinations API
		r.Get("/destinations", destHandler.SearchAPI)
		r.Get("/destinations/featured", destHandler.FeaturedAPI)

		// Public Scraper API
		r.Get("/scraper/status", h.PublicScraperStatus)
		r.Post("/scraper/scrape", h.PublicTriggerScrape)

		// Discord integration
		r.Post("/send-to-discord", h.SendToDiscord)
		r.Post("/test-discord", h.TestDiscord)

		// Flight API
		r.Get("/flights/search", h.SearchFlights)
		r.Get("/flights/offers/{id}", h.GetFlightOffer)

		// Hotel API
		r.Get("/hotels/search", h.SearchHotels)
		r.Get("/hotels/{id}", h.GetHotelDetails)
		r.Get("/hotels/{id}/rooms", h.GetRoomAvailability)

		// Car API
		r.Get("/cars/search", h.SearchCars)
		r.Get("/cars/offers/{id}", h.GetCarOffer)

		// Auth required routes
		r.Group(func(r chi.Router) {
			r.Use(authMiddleware.RequireAuth(authService))

			// User
			r.Get("/users/me", h.GetCurrentUser)
			r.Put("/users/me", h.UpdateUser)
			r.Put("/users/me/password", h.ChangePassword)

			// Bookings
			r.Post("/flights/book", h.BookFlight)
			r.Post("/hotels/book", h.BookHotel)
			r.Post("/cars/book", h.BookCar)
			r.Get("/bookings", h.GetUserBookings)
			r.Get("/bookings/{id}", h.GetBooking)

			// Trips
			r.Get("/trips", h.GetTrips)
			r.Post("/trips", h.CreateTrip)
			r.Get("/trips/{id}", h.GetTrip)
			r.Put("/trips/{id}", h.UpdateTrip)
			r.Delete("/trips/{id}", h.DeleteTrip)
			r.Post("/trips/{id}/items", h.AddTripItem)
			r.Delete("/trips/{id}/items/{itemId}", h.RemoveTripItem)

			// Watchlist
			r.Get("/watchlist", h.GetWatchlist)
			r.Post("/watchlist", h.AddToWatchlist)
			r.Delete("/watchlist/{id}", h.RemoveFromWatchlist)
		})

		// Admin routes
		r.Route("/admin", func(r chi.Router) {
			r.Use(authMiddleware.RequireAuth(authService))
			r.Use(authMiddleware.RequireAdmin)

			r.Get("/dashboard", h.AdminDashboard)
			r.Get("/users", h.AdminListUsers)
			r.Put("/users/{id}/role", h.AdminUpdateUserRole)
			r.Get("/scraping/status", h.ScrapingStatus)
			r.Post("/scraping/trigger", h.TriggerScraping)
			r.Delete("/listings/{id}", h.AdminDeleteListing)
		})
	})

	// Protected pages
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware.RequireAuth(authService))

		r.Get("/dashboard", h.DashboardPage)
		r.Get("/trips", h.TripsPage)
		r.Get("/trips/{id}", h.TripDetailPage)
		r.Get("/watchlist", h.WatchlistPage)
		r.Get("/profile", h.ProfilePage)
		r.Get("/bookings", h.BookingsPage)
	})

	// Admin pages
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware.RequireAuth(authService))
		r.Use(authMiddleware.RequireAdmin)

		r.Get("/admin", h.AdminPage)
	})

	// Start server
	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Seed/refresh destinations from Amadeus in the background on every startup.
	go func() {
		seedCtx := context.Background()
		if err := destService.SeedFromAmadeus(seedCtx); err != nil {
			slog.Warn("destination seed failed", "error", err)
		}
	}()

	// Graceful shutdown
	go func() {
		slog.Info("server starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server failed", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("server forced to shutdown", "error", err)
	}

	slog.Info("server stopped")
}
