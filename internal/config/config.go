package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config holds all application configuration
type Config struct {
	// Server
	Env           string
	Port          string
	SessionSecret string

	// Database
	MongoURI string

	// Redis
	RedisURL string

	// Rust Scraper
	ScraperURL string

	// Discord
	DiscordWebhookURL string

	// Amadeus API
	Amadeus AmadeusConfig

	// Email
	Email EmailConfig
}

type AmadeusConfig struct {
	ClientID     string
	ClientSecret string
	BaseURL      string
}

type EmailConfig struct {
	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string
	FromAddress  string
}

// Load reads configuration from environment variables
func Load() (*Config, error) {
	cfg := &Config{
		Env:               getEnv("ENV", "development"),
		Port:              getEnv("PORT", "8080"),
		SessionSecret:     getEnv("SESSION_SECRET", "change-me-in-production"),
		MongoURI:          getEnv("MONGODB_URI", "mongodb://localhost:27017/real_estayer"),
		RedisURL:          getEnv("REDIS_URL", "redis://localhost:6379"),
		ScraperURL:        getEnv("RUST_SCRAPER_URL", "http://localhost:3001"),
		DiscordWebhookURL: getEnv("DISCORD_WEBHOOK_URL", ""),
		Amadeus: AmadeusConfig{
			ClientID:     getEnv("AMADEUS_API_KEY", getEnv("AMADEUS_CLIENT_ID", "")),
			ClientSecret: getEnv("AMADEUS_API_SECRET", getEnv("AMADEUS_CLIENT_SECRET", "")),
			BaseURL:      getEnv("AMADEUS_API_BASE_URL", getEnv("AMADEUS_BASE_URL", "https://test.api.amadeus.com")),
		},
		Email: EmailConfig{
			SMTPHost:     getEnv("SMTP_HOST", ""),
			SMTPPort:     getEnvInt("SMTP_PORT", 587),
			SMTPUser:     getEnv("SMTP_USER", ""),
			SMTPPassword: getEnv("SMTP_PASSWORD", ""),
			FromAddress:  getEnv("EMAIL_FROM", "noreply@realestayer.com"),
		},
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	if c.SessionSecret == "change-me-in-production" && c.Env == "production" {
		return fmt.Errorf("SESSION_SECRET must be set in production")
	}
	return nil
}

func (c *Config) IsDevelopment() bool {
	return c.Env == "development"
}

func (c *Config) IsProduction() bool {
	return c.Env == "production"
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
