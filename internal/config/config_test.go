package config

import (
	"strings"
	"testing"
)

func TestValidateRejectsShortSessionSecret(t *testing.T) {
	c := &Config{
		Env:           "development",
		SessionSecret: "too-short",
	}
	err := c.validate()
	if err == nil || !strings.Contains(err.Error(), "at least 32") {
		t.Fatalf("expected length error, got %v", err)
	}
}

func TestValidateDevelopmentGeneratesEphemeralSecret(t *testing.T) {
	c := &Config{Env: "development", SessionSecret: ""}
	if err := c.validate(); err != nil {
		t.Fatalf("dev should auto-generate, got %v", err)
	}
	if len(c.SessionSecret) < 32 {
		t.Errorf("expected generated secret ≥32 chars, got %d", len(c.SessionSecret))
	}
}

func TestValidateProductionRequiresStrongSecret(t *testing.T) {
	c := &Config{
		Env:           "production",
		SessionSecret: "",
		ScraperAPIKey: "anything",
	}
	err := c.validate()
	if err == nil || !strings.Contains(err.Error(), "SESSION_SECRET") {
		t.Fatalf("prod without secret must fail; got %v", err)
	}

	c.SessionSecret = "change-me-in-production"
	err = c.validate()
	if err == nil || !strings.Contains(err.Error(), "SESSION_SECRET") {
		t.Fatalf("prod with placeholder secret must fail; got %v", err)
	}
}

func TestValidateProductionRequiresScraperAPIKey(t *testing.T) {
	c := &Config{
		Env:           "production",
		SessionSecret: strings.Repeat("a", 40),
		ScraperAPIKey: "",
	}
	err := c.validate()
	if err == nil || !strings.Contains(err.Error(), "SCRAPER_API_KEY") {
		t.Fatalf("prod without scraper key must fail; got %v", err)
	}
}

func TestValidateProductionRejectsWildcardOrigin(t *testing.T) {
	c := &Config{
		Env:            "production",
		SessionSecret:  strings.Repeat("a", 40),
		ScraperAPIKey:  "key",
		AllowedOrigins: []string{"https://app.example.com", "*"},
	}
	err := c.validate()
	if err == nil || !strings.Contains(err.Error(), "ALLOWED_ORIGINS") {
		t.Fatalf("prod with wildcard origin must fail; got %v", err)
	}
}

func TestIsAdminBootstrapEmail(t *testing.T) {
	c := &Config{AdminBootstrap: []string{"admin@example.com", "Founder@Example.com"}}
	cases := map[string]bool{
		"admin@example.com":   true,
		"ADMIN@example.com":   true,
		"  admin@example.com": true,
		"founder@example.com": true,
		"other@example.com":   false,
		"":                    false,
	}
	for in, want := range cases {
		if got := c.IsAdminBootstrapEmail(in); got != want {
			t.Errorf("IsAdminBootstrapEmail(%q) = %v; want %v", in, got, want)
		}
	}
}

func TestSplitCSVTrims(t *testing.T) {
	got := splitCSV(" a , b,,  c  ")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("splitCSV length mismatch: got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("splitCSV[%d] = %q; want %q", i, got[i], want[i])
		}
	}
}
