package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// AIItineraryService calls the Anthropic API to draft a short itinerary for
// a destination. Falls back to ErrAIItineraryNotConfigured when ANTHROPIC_API_KEY
// is not set so the UI can hide the feature.
//
// We ask Claude for strict JSON back (days + blocks), so the UI can render a
// structured list rather than freeform prose. The request uses system prompt
// caching via `cache_control: {"type": "ephemeral"}` on the system block —
// cost optimization for a prompt that rarely changes.
type AIItineraryService struct {
	apiKey string
	model  string
	client *http.Client
}

var ErrAIItineraryNotConfigured = errors.New("ai: ANTHROPIC_API_KEY not set")

// NewAIItineraryService wires the service. model defaults to Claude Opus 5;
// override with ANTHROPIC_MODEL if you want to trade capability for cost.
func NewAIItineraryService(apiKey, model string) *AIItineraryService {
	if model == "" {
		model = "claude-opus-5"
	}
	return &AIItineraryService{
		apiKey: apiKey,
		model:  model,
		client: &http.Client{Timeout: 60 * time.Second},
	}
}

func (s *AIItineraryService) Configured() bool { return s.apiKey != "" }

// ItineraryRequest is the input shape.
type ItineraryRequest struct {
	Destination string    `json:"destination"`
	StartDate   time.Time `json:"start_date"`
	EndDate     time.Time `json:"end_date"`
	Interests   []string  `json:"interests"`
	BudgetUSD   float64   `json:"budget_usd,omitempty"`
	Pace        string    `json:"pace,omitempty"` // relaxed|balanced|packed
}

// Itinerary is the structured output.
type Itinerary struct {
	Summary string         `json:"summary"`
	Tips    []string       `json:"tips,omitempty"`
	Days    []ItineraryDay `json:"days"`
}

type ItineraryDay struct {
	Date   string           `json:"date"`
	Theme  string           `json:"theme,omitempty"`
	Blocks []ItineraryBlock `json:"blocks"`
}

type ItineraryBlock struct {
	Time         string `json:"time"` // e.g. "09:00"
	Title        string `json:"title"`
	Kind         string `json:"kind"` // morning, lunch, afternoon, dinner, evening, transfer
	Neighborhood string `json:"neighborhood,omitempty"`
	Notes        string `json:"notes,omitempty"`
	CostHint     string `json:"cost_hint,omitempty"`
}

// Generate asks Claude for an itinerary and parses the JSON response.
func (s *AIItineraryService) Generate(ctx context.Context, req ItineraryRequest) (*Itinerary, error) {
	if !s.Configured() {
		return nil, ErrAIItineraryNotConfigured
	}
	if req.Destination == "" {
		return nil, errors.New("destination is required")
	}
	if req.EndDate.Before(req.StartDate) {
		return nil, errors.New("end date before start")
	}
	nights := int(req.EndDate.Sub(req.StartDate).Hours()/24) + 1
	if nights < 1 {
		nights = 1
	}
	if nights > 10 {
		nights = 10 // cap to keep tokens bounded
	}
	if req.Pace == "" {
		req.Pace = "balanced"
	}
	interests := strings.Join(req.Interests, ", ")
	if interests == "" {
		interests = "general sightseeing, local food"
	}

	system := `You are a trip planner. Respond ONLY with a JSON object matching this shape, no prose:
{
  "summary": "<2-sentence overview of the plan>",
  "tips":    ["<short tip>", ...],
  "days":    [
    {
      "date":   "YYYY-MM-DD",
      "theme":  "<one-line theme for the day>",
      "blocks": [
        {"time": "HH:MM", "title": "<place or activity>", "kind": "morning|lunch|afternoon|dinner|evening|transfer",
         "neighborhood": "<district>", "notes": "<one line>", "cost_hint": "$|$$|$$$|free|variable"}
      ]
    }
  ]
}
Rules:
- Prefer well-reviewed places; avoid generic tourist traps unless exceptional.
- Group activities by neighborhood to minimize transit.
- Use local timezone / typical mealtimes.
- 4-7 blocks per day depending on pace (relaxed=4, balanced=5-6, packed=7).
- Don't invent specific prices; use the cost_hint buckets.`

	userMsg := fmt.Sprintf(
		"Plan a %d-day trip to %s starting %s. Traveler interests: %s. Pace: %s.%s Return the JSON only.",
		nights, req.Destination, req.StartDate.Format("2006-01-02"), interests, req.Pace,
		budgetSuffix(req.BudgetUSD),
	)

	body := map[string]interface{}{
		// Current models think by default and max_tokens caps thinking +
		// response text together, so leave headroom above the ~3k the JSON
		// itself needs or the reply truncates mid-object.
		"model":      s.model,
		"max_tokens": 8000,
		"system": []map[string]interface{}{
			{
				"type":          "text",
				"text":          system,
				"cache_control": map[string]string{"type": "ephemeral"},
			},
		},
		"messages": []map[string]interface{}{
			{"role": "user", "content": userMsg},
		},
	}
	buf, _ := json.Marshal(body)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", s.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic: status %d", resp.StatusCode)
	}

	var apiResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}

	var raw string
	for _, c := range apiResp.Content {
		if c.Type == "text" {
			raw += c.Text
		}
	}
	raw = stripCodeFence(strings.TrimSpace(raw))
	var out Itinerary
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("itinerary parse failed: %w (raw: %.200s)", err, raw)
	}
	return &out, nil
}

func budgetSuffix(b float64) string {
	if b <= 0 {
		return ""
	}
	return fmt.Sprintf(" Total budget approximately $%.0f USD.", b)
}

// RefinementTurn is one back-and-forth: the prior itinerary + a user
// instruction like "add more museums" or "swap Tuesday for something quieter".
type RefinementTurn struct {
	Prior    *Itinerary `json:"prior"`
	Feedback string     `json:"feedback"`
}

// Refine runs the itinerary through another pass with the user's feedback.
// Claude is handed the prior JSON and asked to return a new JSON of the same
// shape. Caller should display the diff.
func (s *AIItineraryService) Refine(ctx context.Context, turn RefinementTurn) (*Itinerary, error) {
	if !s.Configured() {
		return nil, ErrAIItineraryNotConfigured
	}
	if turn.Prior == nil {
		return nil, errors.New("prior itinerary required")
	}
	if strings.TrimSpace(turn.Feedback) == "" {
		return nil, errors.New("feedback required")
	}

	system := `You revise travel itineraries. The user will send a JSON itinerary (see schema below) and feedback. Return ONLY the revised itinerary as JSON in the same schema — no prose, no markdown.

Schema:
{"summary":"...", "tips":[...], "days":[{"date":"YYYY-MM-DD","theme":"...","blocks":[{"time":"HH:MM","title":"...","kind":"morning|lunch|afternoon|dinner|evening|transfer","neighborhood":"...","notes":"...","cost_hint":"$|$$|$$$|free|variable"}]}]}

Rules:
- Preserve the overall date range unless the feedback asks to change it.
- Keep items the user likely wants to keep; change only what's called out.
- Reuse concrete places from the prior where sensible; improve others.
- Never invent specific prices.`

	priorJSON, _ := json.Marshal(turn.Prior)
	userMsg := fmt.Sprintf(
		"Here's the current itinerary:\n```json\n%s\n```\n\nFeedback: %s\n\nReturn the revised itinerary JSON only.",
		string(priorJSON), turn.Feedback,
	)

	body := map[string]interface{}{
		// See Generate: budget covers thinking tokens as well as the JSON.
		"model":      s.model,
		"max_tokens": 8000,
		"system": []map[string]interface{}{{
			"type": "text", "text": system,
			"cache_control": map[string]string{"type": "ephemeral"},
		}},
		"messages": []map[string]interface{}{{"role": "user", "content": userMsg}},
	}
	buf, _ := json.Marshal(body)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://api.anthropic.com/v1/messages", bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", s.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("anthropic: status %d", resp.StatusCode)
	}
	var apiResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return nil, err
	}
	raw := ""
	for _, c := range apiResp.Content {
		if c.Type == "text" {
			raw += c.Text
		}
	}
	raw = stripCodeFence(strings.TrimSpace(raw))
	var out Itinerary
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("parse refine response: %w (raw: %.200s)", err, raw)
	}
	return &out, nil
}

// stripCodeFence removes ```json ... ``` wrappers if the model added them
// despite the system-prompt instruction.
func stripCodeFence(s string) string {
	if strings.HasPrefix(s, "```") {
		if i := strings.Index(s, "\n"); i >= 0 {
			s = s[i+1:]
		}
		if j := strings.LastIndex(s, "```"); j >= 0 {
			s = s[:j]
		}
	}
	return strings.TrimSpace(s)
}
