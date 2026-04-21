package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// Config holds all application configuration
type Config struct {
	// Server
	Env           string
	Port          string
	SessionSecret string

	// Security
	AllowedOrigins []string
	ScraperAPIKey  string
	AdminBootstrap []string // emails that should be promoted to admin on registration

	// Database
	MongoURI        string
	MongoDatabase   string

	// Redis
	RedisURL string

	// Rust Scraper
	ScraperURL string

	// Discord
	DiscordWebhookURL string

	// MapTiler key. Optional. When set, templates should build a style URL
	// against https://api.maptiler.com; when empty, fall back to CARTO tiles.
	MapTilerKey string

	// Ticketmaster Discovery API key for the events feature. Optional.
	TicketmasterAPIKey string

	// Anthropic key for the AI itinerary builder. Optional.
	AnthropicAPIKey string
	AIModel         string

	// Optional enrichment keys
	OpenAQAPIKey   string
	UnsplashKey    string
	AffiliateTag   string // utm_source / partner id for outbound links
	AviationStackAPIKey string
	EBirdAPIKey         string
	BookingAffiliateID  string
	BookingDemandKey    string
	ExpediaAPIKey       string
	ExpediaSharedSecret string

	// Google Directions key enables the transit-routing widget; when empty
	// the service gracefully falls back to OSRM driving.
	GoogleDirectionsKey string

	// Car-rental affiliate IDs. All three are optional — when empty the
	// deep-link still renders, just without the tracking parameter.
	RentalcarsAffiliateID string
	PricelineAffiliateID  string
	KayakAffiliateID      string

	// Public URL of this deployment — used in email links. Falls back to
	// http://localhost:<port> in dev.
	AppBaseURL string

	// Observability
	ErrorWebhookURL string // POSTed on panic
	MetricsEnabled  bool   // exposes /metrics when true

	// 32-byte key (64 hex chars or 32 raw bytes) used to AES-GCM encrypt
	// sensitive user fields — passport number, KTN, loyalty numbers. When
	// empty, fields are stored plain-text and the profile UI shows a warning.
	FieldEncryptionKey string

	// Amadeus API (legacy — kept for backwards compat while we migrate off)
	Amadeus AmadeusConfig

	// Duffel API — flights replacement for Amadeus. Single access token.
	Duffel DuffelConfig

	// Email
	Email EmailConfig
}

type AmadeusConfig struct {
	ClientID     string
	ClientSecret string
	BaseURL      string
}

// DuffelConfig holds Duffel Air API credentials. BaseURL defaults to
// https://api.duffel.com; Duffel doesn't ship a separate test host — test
// tokens return sandbox data against the same URL.
type DuffelConfig struct {
	AccessToken string
	BaseURL     string
}

type EmailConfig struct {
	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string
	FromAddress  string
	FromName     string
	ImplicitTLS  bool
}

// Load reads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{
		Env:               getEnv("APP_ENV", getEnv("ENV", "development")),
		Port:              getEnv("PORT", "8080"),
		SessionSecret:     os.Getenv("SESSION_SECRET"),
		AllowedOrigins:    splitCSV(getEnv("ALLOWED_ORIGINS", "http://localhost:8080")),
		ScraperAPIKey:     os.Getenv("SCRAPER_API_KEY"),
		AdminBootstrap:    splitCSV(os.Getenv("ADMIN_BOOTSTRAP_EMAILS")),
		MongoURI:          getEnv("MONGODB_URI", "mongodb://localhost:27017/real_estayer"),
		MongoDatabase:     getEnv("MONGODB_DATABASE", "real_estayer"),
		RedisURL:          getEnv("REDIS_URL", "redis://localhost:6379"),
		ScraperURL:        getEnv("RUST_SCRAPER_URL", "http://localhost:3001"),
		DiscordWebhookURL: os.Getenv("DISCORD_WEBHOOK_URL"),
		MapTilerKey:        os.Getenv("MAPTILER_KEY"),
		TicketmasterAPIKey: os.Getenv("TICKETMASTER_API_KEY"),
		AnthropicAPIKey:    os.Getenv("ANTHROPIC_API_KEY"),
		AIModel:            getEnv("ANTHROPIC_MODEL", "claude-sonnet-4-6"),
		OpenAQAPIKey:       os.Getenv("OPENAQ_API_KEY"),
		UnsplashKey:        os.Getenv("UNSPLASH_ACCESS_KEY"),
		AffiliateTag:       getEnv("AFFILIATE_TAG", "realestayer"),
		AviationStackAPIKey: os.Getenv("AVIATIONSTACK_API_KEY"),
		EBirdAPIKey:        os.Getenv("EBIRD_API_KEY"),
		BookingAffiliateID: os.Getenv("BOOKING_AFFILIATE_ID"),
		BookingDemandKey:   os.Getenv("BOOKING_DEMAND_KEY"),
		ExpediaAPIKey:      os.Getenv("EPS_API_KEY"),
		ExpediaSharedSecret: os.Getenv("EPS_SHARED_SECRET"),
		GoogleDirectionsKey:   os.Getenv("GOOGLE_DIRECTIONS_KEY"),
		RentalcarsAffiliateID: os.Getenv("RENTALCARS_AFFILIATE_ID"),
		PricelineAffiliateID:  os.Getenv("PRICELINE_AFFILIATE_ID"),
		KayakAffiliateID:      os.Getenv("KAYAK_AFFILIATE_ID"),
		AppBaseURL:         os.Getenv("APP_BASE_URL"),
		ErrorWebhookURL:    os.Getenv("ERROR_WEBHOOK_URL"),
		MetricsEnabled:     getEnvBool("METRICS_ENABLED", false),
		FieldEncryptionKey: os.Getenv("FIELD_ENCRYPTION_KEY"),
		Amadeus: AmadeusConfig{
			ClientID:     getEnv("AMADEUS_API_KEY", os.Getenv("AMADEUS_CLIENT_ID")),
			ClientSecret: getEnv("AMADEUS_API_SECRET", os.Getenv("AMADEUS_CLIENT_SECRET")),
			// Default to the production host. Test mode (https://test.api.amadeus.com)
			// must be opted into explicitly — AMADEUS_ENV=test or AMADEUS_API_BASE_URL
			// override — so we don't silently route real bookings through the
			// sandbox because an env var got missed.
			BaseURL: ensureHTTPS(resolveAmadeusBaseURL()),
		},
		Duffel: DuffelConfig{
			AccessToken: os.Getenv("DUFFEL_ACCESS_TOKEN"),
			BaseURL:     getEnv("DUFFEL_BASE_URL", "https://api.duffel.com"),
		},
		Email: EmailConfig{
			SMTPHost:     os.Getenv("SMTP_HOST"),
			SMTPPort:     getEnvInt("SMTP_PORT", 587),
			SMTPUser:     os.Getenv("SMTP_USER"),
			SMTPPassword: os.Getenv("SMTP_PASSWORD"),
			FromAddress:  getEnv("EMAIL_FROM", "noreply@realestayer.com"),
			FromName:     getEnv("EMAIL_FROM_NAME", "Real-Estayer"),
			ImplicitTLS:  getEnvBool("SMTP_IMPLICIT_TLS", false),
		},
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	// Session secret must be set and strong in production; auto-generate in dev with a warning.
	if c.SessionSecret == "" || c.SessionSecret == "change-me-in-production" || c.SessionSecret == "your-super-secret-session-key-change-me" {
		if c.IsProduction() {
			return fmt.Errorf("SESSION_SECRET must be set to a strong random value in production")
		}
		generated, err := randomHex(32)
		if err != nil {
			return fmt.Errorf("failed to generate ephemeral session secret: %w", err)
		}
		c.SessionSecret = generated
		slog.Warn("SESSION_SECRET not set; generated an ephemeral secret (sessions will not survive restart)")
	}
	if len(c.SessionSecret) < 32 {
		return fmt.Errorf("SESSION_SECRET must be at least 32 characters")
	}

	if c.IsProduction() {
		if c.ScraperAPIKey == "" {
			return fmt.Errorf("SCRAPER_API_KEY must be set in production")
		}
		for _, o := range c.AllowedOrigins {
			if o == "*" {
				return fmt.Errorf("ALLOWED_ORIGINS cannot contain '*' in production")
			}
		}
	}

	for _, o := range c.AllowedOrigins {
		if o == "*" {
			continue
		}
		if _, err := url.Parse(o); err != nil {
			return fmt.Errorf("invalid ALLOWED_ORIGINS entry %q: %w", o, err)
		}
	}
	return nil
}

// LogFeatureSummary prints a one-shot inventory of which optional features
// are live based on the env vars that configure them. Callers typically run
// this right after Load() so misconfigurations surface at boot instead of
// the first request that needs the missing key. Required-in-prod checks
// still live in validate(); this is purely informational.
func (c *Config) LogFeatureSummary() {
	feature := func(name string, on bool, note string) {
		status := "disabled"
		if on {
			status = "enabled"
		}
		attrs := []any{"feature", name, "status", status}
		if note != "" {
			attrs = append(attrs, "note", note)
		}
		if on {
			slog.Info("feature", attrs...)
		} else {
			slog.Warn("feature", attrs...)
		}
	}

	feature("duffel", c.Duffel.AccessToken != "",
		"flights (new primary provider)")
	feature("amadeus", c.Amadeus.ClientID != "" && c.Amadeus.ClientSecret != "",
		"legacy flights/hotels/cars — being decommissioned")
	feature("email_smtp", c.Email.SMTPHost != "",
		"password reset + notification emails")
	feature("redis", c.RedisURL != "" && !strings.HasPrefix(c.RedisURL, "redis://localhost"),
		"shared rate limiter state")
	feature("rust_scraper", c.ScraperURL != "" && c.ScraperAPIKey != "",
		"Airbnb scraping backend")
	feature("field_encryption", c.FieldEncryptionKey != "",
		"AES-GCM for PII — without this, passport/KTN is stored plaintext")
	feature("discord_webhook", c.DiscordWebhookURL != "", "price-drop notifications")
	feature("anthropic", c.AnthropicAPIKey != "", "AI itinerary + receipt OCR")
	feature("maptiler", c.MapTilerKey != "", "vector map tiles (falls back to CARTO)")
	feature("ticketmaster", c.TicketmasterAPIKey != "", "events search")
	feature("openaq", c.OpenAQAPIKey != "", "air quality widget")
	feature("aviationstack", c.AviationStackAPIKey != "", "flight status lookups")
	feature("ebird", c.EBirdAPIKey != "", "nature/birding content")
	feature("unsplash", c.UnsplashKey != "", "destination hero images")
	feature("google_directions", c.GoogleDirectionsKey != "", "transit routing (falls back to OSRM)")
	feature("booking_affiliate", c.BookingAffiliateID != "", "Booking.com affiliate links")
	feature("car_affiliates", c.RentalcarsAffiliateID != "" || c.PricelineAffiliateID != "" || c.KayakAffiliateID != "",
		"/cars deep-link tracking (at least one of rentalcars/priceline/kayak)")
	feature("expedia", c.ExpediaAPIKey != "" && c.ExpediaSharedSecret != "", "Expedia partner search")
	feature("metrics", c.MetricsEnabled, "/metrics endpoint")
	feature("error_webhook", c.ErrorWebhookURL != "", "panic reporting")
}

func (c *Config) IsDevelopment() bool {
	return c.Env == "development"
}

func (c *Config) IsProduction() bool {
	return c.Env == "production"
}

// IsAdminBootstrapEmail reports whether the email is listed in ADMIN_BOOTSTRAP_EMAILS.
// Used only to seed the first admin account; ongoing admin assignment should go through the admin API.
func (c *Config) IsAdminBootstrapEmail(email string) bool {
	e := strings.ToLower(strings.TrimSpace(email))
	for _, bootstrap := range c.AdminBootstrap {
		if strings.EqualFold(strings.TrimSpace(bootstrap), e) {
			return true
		}
	}
	return false
}

func ensureHTTPS(u string) string {
	if u != "" && !strings.HasPrefix(u, "http://") && !strings.HasPrefix(u, "https://") {
		return "https://" + u
	}
	return u
}

// resolveAmadeusBaseURL returns the Amadeus API host to talk to. An explicit
// AMADEUS_API_BASE_URL / AMADEUS_BASE_URL wins; otherwise AMADEUS_ENV=test
// opts into the sandbox; otherwise we default to the production host so a
// misconfigured deployment never quietly routes real bookings through test.
func resolveAmadeusBaseURL() string {
	if v := os.Getenv("AMADEUS_API_BASE_URL"); v != "" {
		return v
	}
	if v := os.Getenv("AMADEUS_BASE_URL"); v != "" {
		return v
	}
	if strings.EqualFold(os.Getenv("AMADEUS_ENV"), "test") {
		return "https://test.api.amadeus.com"
	}
	return "https://api.amadeus.com"
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if intVal, err := strconv.Atoi(value); err == nil {
			return intVal
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return defaultValue
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	return defaultValue
}

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
