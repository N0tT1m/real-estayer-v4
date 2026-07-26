package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/realestayer/v4/internal/models"
)

// EmailParserService turns a forwarded / pasted booking confirmation into one
// or more structured TripItems. Two paths:
//
//  1. Rule-based parsers for the common templates (Airbnb, Booking.com,
//     generic airlines, generic hotels, car rentals). Fast, zero cost,
//     deterministic.
//  2. Claude fallback if no rule matches. Expensive but catches the long
//     tail.
//
// We never mutate the trip — the returned items are "draft" suggestions for
// the user to confirm in the UI.
type EmailParserService struct {
	ai *AIItineraryService // for the Claude fallback path
}

func NewEmailParserService(ai *AIItineraryService) *EmailParserService {
	return &EmailParserService{ai: ai}
}

// ParsedImport is what the handler returns: a list of proposed trip items and
// an optional low-level confidence reason so the UI can surface "we're not
// sure, please double-check".
type ParsedImport struct {
	Source string                      `json:"source"`
	Items  []models.AddTripItemRequest `json:"items"`
	Notes  string                      `json:"notes,omitempty"`
}

// Parse runs the rule-based parsers first; when none match and an Anthropic
// key is configured, it falls back to Claude. Never panics on malformed input.
func (s *EmailParserService) Parse(ctx context.Context, emailText string) (*ParsedImport, error) {
	text := strings.TrimSpace(emailText)
	if text == "" {
		return nil, errors.New("empty email")
	}
	lower := strings.ToLower(text)

	switch {
	case strings.Contains(lower, "airbnb"):
		if out := parseAirbnb(text); out != nil {
			return &ParsedImport{Source: "airbnb", Items: out}, nil
		}
	case strings.Contains(lower, "booking.com"):
		if out := parseBooking(text); out != nil {
			return &ParsedImport{Source: "booking.com", Items: out}, nil
		}
	case strings.Contains(lower, "your flight") || strings.Contains(lower, "e-ticket") || strings.Contains(lower, "boarding pass"):
		if out := parseGenericFlight(text); out != nil {
			return &ParsedImport{Source: "flight", Items: out}, nil
		}
	case strings.Contains(lower, "car rental") || strings.Contains(lower, "hertz") || strings.Contains(lower, "enterprise") || strings.Contains(lower, "sixt"):
		if out := parseGenericCar(text); out != nil {
			return &ParsedImport{Source: "car", Items: out}, nil
		}
	}

	// Fallback: Claude.
	if s.ai != nil && s.ai.Configured() {
		items, err := s.claudeFallback(ctx, text)
		if err != nil {
			return nil, err
		}
		return &ParsedImport{Source: "ai", Items: items, Notes: "Parsed with AI — please double-check the details."}, nil
	}
	return nil, errors.New("no rule matched and no AI fallback configured")
}

// --- Rule-based parsers ---

// Airbnb confirmation has: "You're going to ..." + check-in/out lines +
// "Hosted by ...". We look for the city in the subject/header and the ISO-ish
// dates that follow "Check-in" / "Check-out".
func parseAirbnb(text string) []models.AddTripItemRequest {
	title := "Airbnb stay"
	if m := regexp.MustCompile(`(?i)(?:going to|trip to)\s+([A-Z][A-Za-z .,-]+)`).FindStringSubmatch(text); len(m) > 1 {
		title = "Airbnb: " + strings.TrimSpace(m[1])
	}
	in := findDate(text, `(?i)check[- ]?in[:\s]+(\w+\s+\d{1,2},\s*\d{4})`)
	out := findDate(text, `(?i)check[- ]?out[:\s]+(\w+\s+\d{1,2},\s*\d{4})`)
	if in == nil || out == nil {
		return nil
	}
	price := findPrice(text)
	item := models.AddTripItemRequest{
		Type:      models.TripItemTypeListing,
		Provider:  "airbnb",
		Title:     title,
		StartTime: in,
		EndTime:   out,
	}
	if price > 0 {
		item.Price = &models.TripItemPrice{Amount: price, Currency: findCurrency(text)}
	}
	if conf := findConfirmation(text); conf != "" {
		item.ReferenceID = conf
	}
	return []models.AddTripItemRequest{item}
}

func parseBooking(text string) []models.AddTripItemRequest {
	title := "Hotel booking"
	if m := regexp.MustCompile(`(?i)booked[: ]\s*([A-Z][A-Za-z0-9 &'.,-]+)`).FindStringSubmatch(text); len(m) > 1 {
		title = strings.TrimSpace(m[1])
	}
	in := findDate(text, `(?i)check[- ]?in[:\s]+([A-Za-z]+\s+\d{1,2},?\s*\d{4})`)
	out := findDate(text, `(?i)check[- ]?out[:\s]+([A-Za-z]+\s+\d{1,2},?\s*\d{4})`)
	if in == nil || out == nil {
		return nil
	}
	price := findPrice(text)
	item := models.AddTripItemRequest{
		Type:      models.TripItemTypeHotel,
		Provider:  "booking.com",
		Title:     title,
		StartTime: in,
		EndTime:   out,
	}
	if price > 0 {
		item.Price = &models.TripItemPrice{Amount: price, Currency: findCurrency(text)}
	}
	if conf := findConfirmation(text); conf != "" {
		item.ReferenceID = conf
	}
	return []models.AddTripItemRequest{item}
}

// parseGenericFlight looks for an IATA flight-number and two airport codes.
// Many airlines format these consistently: "UA 123" + "SFO → JFK" + date/time.
func parseGenericFlight(text string) []models.AddTripItemRequest {
	// Airlines format flight numbers either as "UA283" or "UA 283"; normalise.
	flightRE := regexp.MustCompile(`\b([A-Z]{2})\s?(\d{1,4})\b`)
	routeRE := regexp.MustCompile(`\b([A-Z]{3})\s*(?:→|->|to|-)\s*([A-Z]{3})\b`)
	flightMatch := flightRE.FindStringSubmatch(text)
	route := routeRE.FindStringSubmatch(text)
	if len(flightMatch) < 3 || len(route) < 3 {
		return nil
	}
	flight := flightMatch[1] + flightMatch[2]
	depAt := findDateTime(text, `(?i)depart(?:ure|s)?[:\s]+(\w+\s+\d{1,2},?\s*\d{4}[,\s]+\d{1,2}:\d{2}\s*[AP]M)`)
	title := fmt.Sprintf("%s: %s → %s", flight, route[1], route[2])
	item := models.AddTripItemRequest{
		Type:      models.TripItemTypeFlight,
		Title:     title,
		StartTime: depAt,
		Details: map[string]any{
			"flight_number": flight,
			"from_iata":     route[1],
			"to_iata":       route[2],
		},
	}
	if price := findPrice(text); price > 0 {
		item.Price = &models.TripItemPrice{Amount: price, Currency: findCurrency(text)}
	}
	if conf := findConfirmation(text); conf != "" {
		item.ReferenceID = conf
	}
	return []models.AddTripItemRequest{item}
}

func parseGenericCar(text string) []models.AddTripItemRequest {
	start := findDateTime(text, `(?i)pick[- ]?up[:\s]+(\w+\s+\d{1,2},?\s*\d{4}[,\s]+\d{1,2}:\d{2}\s*[AP]M)`)
	end := findDateTime(text, `(?i)(?:drop[- ]?off|return)[:\s]+(\w+\s+\d{1,2},?\s*\d{4}[,\s]+\d{1,2}:\d{2}\s*[AP]M)`)
	if start == nil && end == nil {
		return nil
	}
	provider := ""
	for _, brand := range []string{"Hertz", "Enterprise", "Sixt", "Avis", "Budget", "Alamo", "National"} {
		if strings.Contains(text, brand) {
			provider = strings.ToLower(brand)
			break
		}
	}
	title := "Car rental"
	if provider != "" {
		title = titleWords(provider) + " rental"
	}
	item := models.AddTripItemRequest{
		Type:      models.TripItemTypeCar,
		Provider:  provider,
		Title:     title,
		StartTime: start,
		EndTime:   end,
	}
	if price := findPrice(text); price > 0 {
		item.Price = &models.TripItemPrice{Amount: price, Currency: findCurrency(text)}
	}
	if conf := findConfirmation(text); conf != "" {
		item.ReferenceID = conf
	}
	return []models.AddTripItemRequest{item}
}

// --- Claude fallback ---

// claudeFallback asks Claude to extract a structured list of trip items. The
// prompt mirrors our AddTripItemRequest JSON so we can unmarshal directly.
func (s *EmailParserService) claudeFallback(ctx context.Context, text string) ([]models.AddTripItemRequest, error) {
	const systemPrompt = `You extract travel bookings from confirmation emails into JSON.
Respond ONLY with a JSON object of the form {"items":[ ... ]} where each item has fields:
  type: one of "flight" | "hotel" | "car" | "listing" | "activity"
  title: short human-readable name
  provider: brand name (airbnb, united, hertz, ...)
  reference_id: booking or confirmation code (omit if unknown)
  start_time: RFC3339 timestamp (with offset) or ISO date
  end_time: RFC3339 timestamp (or omit for same-day activities)
  price: {"amount": <number>, "currency": "USD"} (omit if unknown)
  details: { ...any extra structured fields you can extract }
If the email isn't a travel booking, respond with {"items":[]}.`

	body := map[string]any{
		// Headroom for thinking tokens, which share this budget on current models.
		"model":      s.ai.model,
		"max_tokens": 4000,
		"system": []map[string]any{{
			"type": "text", "text": systemPrompt,
			"cache_control": map[string]string{"type": "ephemeral"},
		}},
		"messages": []map[string]any{{
			"role": "user", "content": text,
		}},
	}
	buf, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", s.ai.apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	resp, err := s.ai.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic: status %d", resp.StatusCode)
	}

	var api struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&api); err != nil {
		return nil, err
	}
	raw := ""
	for _, c := range api.Content {
		if c.Type == "text" {
			raw += c.Text
		}
	}
	raw = stripCodeFence(strings.TrimSpace(raw))

	// Claude writes dates as strings; we parse them into *time.Time here so
	// the Mongo store gets proper timestamps.
	var wire struct {
		Items []struct {
			Type        string                `json:"type"`
			Title       string                `json:"title"`
			Provider    string                `json:"provider"`
			ReferenceID string                `json:"reference_id"`
			StartTime   string                `json:"start_time"`
			EndTime     string                `json:"end_time"`
			Details     map[string]any        `json:"details"`
			Price       *models.TripItemPrice `json:"price"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		return nil, fmt.Errorf("parse AI response: %w (raw: %.200s)", err, raw)
	}
	out := make([]models.AddTripItemRequest, 0, len(wire.Items))
	for _, it := range wire.Items {
		item := models.AddTripItemRequest{
			Type:        models.TripItemType(it.Type),
			Title:       it.Title,
			Provider:    it.Provider,
			ReferenceID: it.ReferenceID,
			Details:     it.Details,
			Price:       it.Price,
		}
		if t, err := parseFlexibleTime(it.StartTime); err == nil && !t.IsZero() {
			item.StartTime = &t
		}
		if t, err := parseFlexibleTime(it.EndTime); err == nil && !t.IsZero() {
			item.EndTime = &t
		}
		out = append(out, item)
	}
	return out, nil
}

// --- Shared helpers ---

func findDate(text, pattern string) *time.Time {
	m := regexp.MustCompile(pattern).FindStringSubmatch(text)
	if len(m) < 2 {
		return nil
	}
	return tryParseDates(m[1])
}

func findDateTime(text, pattern string) *time.Time {
	m := regexp.MustCompile(pattern).FindStringSubmatch(text)
	if len(m) < 2 {
		return nil
	}
	return tryParseDates(m[1])
}

var dateFormats = []string{
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05",
	"2006-01-02 15:04",
	"2006-01-02",
	"Jan 2, 2006 3:04 PM",
	"January 2, 2006 3:04 PM",
	"Jan 2, 2006",
	"January 2, 2006",
	"Jan 2 2006",
	"2 Jan 2006",
}

func tryParseDates(s string) *time.Time {
	s = strings.TrimSpace(strings.ReplaceAll(s, ",", ""))
	for _, f := range dateFormats {
		// Strip the stray comma that some patterns leave behind.
		normal := strings.ReplaceAll(f, ",", "")
		if t, err := time.Parse(normal, s); err == nil {
			return &t
		}
	}
	return nil
}

func parseFlexibleTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	for _, f := range dateFormats {
		if t, err := time.Parse(strings.ReplaceAll(f, ",", ""), strings.ReplaceAll(s, ",", "")); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unparseable time: %s", s)
}

func findPrice(text string) float64 {
	m := regexp.MustCompile(`(?:total|price|amount)[^$€£¥]*([$€£¥])\s*([\d,]+\.?\d*)`).FindStringSubmatch(strings.ToLower(text))
	if len(m) < 3 {
		return 0
	}
	var f float64
	_, _ = fmt.Sscanf(strings.ReplaceAll(m[2], ",", ""), "%f", &f)
	return f
}

func findCurrency(text string) string {
	if strings.Contains(text, "€") || strings.Contains(strings.ToLower(text), " eur") {
		return "EUR"
	}
	if strings.Contains(text, "£") || strings.Contains(strings.ToLower(text), " gbp") {
		return "GBP"
	}
	if strings.Contains(text, "¥") || strings.Contains(strings.ToLower(text), " jpy") {
		return "JPY"
	}
	return "USD"
}

func findConfirmation(text string) string {
	// Looks for "confirmation code: ABC123" or "code: ABC123" or "#ABC123".
	patterns := []string{
		`(?i)confirmation\s*(?:code|number)?[:\s#]+([A-Z0-9]{5,})`,
		`(?i)reservation\s*(?:code|number)?[:\s#]+([A-Z0-9]{5,})`,
		`(?i)booking\s*(?:ref|reference|number)?[:\s#]+([A-Z0-9]{5,})`,
	}
	for _, p := range patterns {
		if m := regexp.MustCompile(p).FindStringSubmatch(text); len(m) > 1 {
			return strings.TrimSpace(m[1])
		}
	}
	return ""
}
