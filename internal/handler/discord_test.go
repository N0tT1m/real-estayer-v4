package handler

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/service"
)

func sampleListing() *models.Listing {
	return &models.Listing{
		URL:          "https://www.airbnb.com/rooms/12345",
		Title:        "Lakeside cabin with hot tub",
		PictureURL:   "https://img.example/cabin.jpg",
		Description:  "A quiet cabin on the water.",
		Price:        "$180",
		Rating:       "4.92",
		ReviewsCount: 214,
		Location:     "Traverse City, Michigan",
		PropertyType: "Cabin",
		Features:     []string{"Hot Tub", "WiFi", "Kitchen"},
	}
}

func TestListingEmbedCarriesTheRealAirbnbURL(t *testing.T) {
	e := listingEmbed(sampleListing())
	// The whole point: Discord renders `url` as the clickable title, so it must
	// be the real listing, not an internal link.
	if e["url"] != "https://www.airbnb.com/rooms/12345" {
		t.Errorf("url = %v, want the Airbnb URL", e["url"])
	}
	if e["title"] != "Lakeside cabin with hot tub" {
		t.Errorf("title = %v", e["title"])
	}
	if img, ok := e["image"].(map[string]string); !ok || img["url"] == "" {
		t.Errorf("image = %v, want the listing photo", e["image"])
	}
}

func TestListingEmbedFieldsIncludePriceRatingLocation(t *testing.T) {
	e := listingEmbed(sampleListing())
	fields, ok := e["fields"].([]map[string]any)
	if !ok {
		t.Fatalf("fields = %T", e["fields"])
	}
	got := map[string]string{}
	for _, f := range fields {
		got[f["name"].(string)] = f["value"].(string)
	}
	if got["Price"] != "$180" {
		t.Errorf("Price = %q", got["Price"])
	}
	// Review count is folded into the rating so the card reads naturally.
	if got["Rating"] != "4.92 (214 reviews)" {
		t.Errorf("Rating = %q", got["Rating"])
	}
	if got["Location"] != "Traverse City, Michigan" {
		t.Errorf("Location = %q", got["Location"])
	}
}

// Discord rejects the whole payload if a field value is empty, so blanks must
// be omitted rather than sent as "".
func TestListingEmbedOmitsEmptyFields(t *testing.T) {
	l := &models.Listing{URL: "https://www.airbnb.com/rooms/1", Title: "Bare"}
	e := listingEmbed(l)
	for _, f := range e["fields"].([]map[string]any) {
		if strings.TrimSpace(f["value"].(string)) == "" {
			t.Errorf("field %v has an empty value", f["name"])
		}
	}
	if _, present := e["image"]; present {
		t.Error("no picture URL should mean no image key")
	}
	if _, present := e["description"]; present {
		t.Error("no description should mean no description key")
	}
}

func TestListingEmbedFallsBackWhenTitleMissing(t *testing.T) {
	e := listingEmbed(&models.Listing{URL: "https://www.airbnb.com/rooms/1"})
	if e["title"] != "Airbnb listing" {
		t.Errorf("title = %v, want a fallback rather than an empty title", e["title"])
	}
}

// Discord caps titles at 256 and descriptions at 4096; over-long values make
// the API reject the message entirely.
func TestListingEmbedTruncatesOverlongText(t *testing.T) {
	l := sampleListing()
	l.Title = strings.Repeat("x", 500)
	l.Description = strings.Repeat("y", 5000)
	e := listingEmbed(l)
	if len([]rune(e["title"].(string))) > 256 {
		t.Errorf("title length %d exceeds Discord's limit", len(e["title"].(string)))
	}
	if len(e["description"].(string)) > 4096 {
		t.Errorf("description length %d exceeds Discord's limit", len(e["description"].(string)))
	}
}

func TestListingEmbedCapsFeatureList(t *testing.T) {
	l := sampleListing()
	l.Features = make([]string, 40)
	for i := range l.Features {
		l.Features[i] = "feature"
	}
	e := listingEmbed(l)
	for _, f := range e["fields"].([]map[string]any) {
		if f["name"] == "Features" {
			if n := strings.Count(f["value"].(string), "·") + 1; n > 8 {
				t.Errorf("listed %d features, want at most 8", n)
			}
		}
	}
}

// The send path re-validates the stored webhook host. A value that predates
// validation, or was edited directly in Mongo, must not be POSTed to.
func TestIsDiscordWebhookRejectsNonDiscordHosts(t *testing.T) {
	valid := []string{
		"https://discord.com/api/webhooks/123/abc",
		"https://discordapp.com/api/webhooks/123/abc",
		"https://canary.discord.com/api/webhooks/123/abc",
		"https://ptb.discord.com/api/webhooks/123/abc",
	}
	for _, u := range valid {
		if !service.IsDiscordWebhook(u) {
			t.Errorf("%q should be accepted", u)
		}
	}
	hostile := []string{
		"http://discord.com/api/webhooks/1/a",               // plain http
		"https://evil.example/api/webhooks/1/a",             // wrong host
		"https://discord.com.evil.example/api/webhooks/1/a", // suffix trick
		"http://169.254.169.254/latest/meta-data/",          // cloud metadata
		"http://127.0.0.1:27017/",                           // internal service
		"file:///etc/passwd",
		"",
	}
	for _, u := range hostile {
		if service.IsDiscordWebhook(u) {
			t.Errorf("%q must be rejected — this is the SSRF guard", u)
		}
	}
}

// postDiscordWebhook is the single choke point for outbound webhook POSTs, so
// it enforces the host itself rather than trusting its callers.
func TestPostDiscordWebhookRefusesNonDiscordHost(t *testing.T) {
	hostile := []string{
		"http://169.254.169.254/latest/meta-data/",
		"http://127.0.0.1:27017/",
		"https://evil.example/api/webhooks/1/a",
		"",
	}
	for _, u := range hostile {
		err := postDiscordWebhook(context.Background(), u, map[string]any{"content": "x"})
		if err == nil {
			t.Errorf("posting to %q should have been refused", u)
			continue
		}
		if !strings.Contains(err.Error(), "non-Discord host") {
			t.Errorf("posting to %q failed with %v, want the host guard", u, err)
		}
	}
}

// A 302 from the webhook endpoint must not be followed — that would route the
// POST past both host checks.
func TestDiscordClientRefusesRedirects(t *testing.T) {
	if discordClient.CheckRedirect == nil {
		t.Fatal("discordClient must refuse redirects")
	}
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1/internal", nil)
	if err := discordClient.CheckRedirect(req, nil); err == nil {
		t.Error("CheckRedirect allowed a redirect; it must return an error")
	}
}
