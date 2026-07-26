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

// A struct field plus a feature check is not enough: the loader has to
// actually read the variable. AIBaseURL shipped without its Load() assignment
// and silently reported the AI feature as disabled, so pin the wiring here.
func TestLoadReadsAIBaseURL(t *testing.T) {
	t.Setenv("SESSION_SECRET", strings.Repeat("x", 32))
	t.Setenv("AI_BASE_URL", "http://REMOTE_HOST_REMOVED:11434/v1")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.AIBaseURL != "http://REMOTE_HOST_REMOVED:11434/v1" {
		t.Errorf("AIBaseURL = %q, want the AI_BASE_URL value", cfg.AIBaseURL)
	}
}

func TestAIBackendNoteNamesTheBackend(t *testing.T) {
	if got := aiBackendNote(&Config{AIBaseURL: "http://h/v1"}); !strings.Contains(got, "http://h/v1") {
		t.Errorf("note = %q, want it to name the local endpoint", got)
	}
	if got := aiBackendNote(&Config{AnthropicAPIKey: "k"}); !strings.Contains(got, "anthropic") {
		t.Errorf("note = %q, want it to name Anthropic", got)
	}
	if got := aiBackendNote(&Config{}); got != "" {
		t.Errorf("note = %q, want empty when no backend is configured", got)
	}
}

// Every env-backed field should round-trip through Load. This catches the
// "declared but never assigned" class of bug generically.
func TestLoadReadsCommonEnvVars(t *testing.T) {
	t.Setenv("SESSION_SECRET", strings.Repeat("x", 32))
	cases := map[string]struct {
		env, val string
		get      func(*Config) string
	}{
		"AI_BASE_URL":          {"AI_BASE_URL", "http://x/v1", func(c *Config) string { return c.AIBaseURL }},
		"ANTHROPIC_API_KEY":    {"ANTHROPIC_API_KEY", "sk-test", func(c *Config) string { return c.AnthropicAPIKey }},
		"DISCORD_WEBHOOK_URL":  {"DISCORD_WEBHOOK_URL", "https://discord.com/api/webhooks/1/a", func(c *Config) string { return c.DiscordWebhookURL }},
		"FIELD_ENCRYPTION_KEY": {"FIELD_ENCRYPTION_KEY", strings.Repeat("a", 64), func(c *Config) string { return c.FieldEncryptionKey }},
		"SMTP_HOST":            {"SMTP_HOST", "box.example", func(c *Config) string { return c.Email.SMTPHost }},
		"MONGODB_DATABASE":     {"MONGODB_DATABASE", "somedb", func(c *Config) string { return c.MongoDatabase }},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Setenv(tc.env, tc.val)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if got := tc.get(cfg); got != tc.val {
				t.Errorf("%s: got %q, want %q — is it assigned in Load()?", tc.env, got, tc.val)
			}
		})
	}
}
