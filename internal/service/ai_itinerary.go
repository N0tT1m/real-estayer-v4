package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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
	apiKey  string
	model   string
	baseURL string // "" => Anthropic's hosted API
	client  *http.Client
}

var ErrAIItineraryNotConfigured = errors.New("ai: set ANTHROPIC_API_KEY, or AI_BASE_URL for a local OpenAI-compatible endpoint")

// anthropicBaseURL is the hosted API. A non-empty AI_BASE_URL replaces it and
// switches the wire format to OpenAI-compatible chat completions.
const anthropicBaseURL = "https://api.anthropic.com"

// NewAIItineraryService wires the service.
//
// Two backends are supported:
//
//   - Anthropic hosted: leave baseURL empty and set an API key. model defaults
//     to Claude Opus 5.
//   - Any OpenAI-compatible endpoint (ollama, vLLM, a local gateway): set
//     baseURL to something ending in /v1. The API key is optional — local
//     servers usually want none — so configuration is keyed on baseURL there.
//
// The two speak different wire formats; see chatText.
func NewAIItineraryService(apiKey, model, baseURL string) *AIItineraryService {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if model == "" {
		if baseURL == "" {
			model = "claude-opus-5"
		}
		// For a local endpoint there is no sensible default model name — the
		// operator must name one their server actually serves.
	}
	return &AIItineraryService{
		apiKey:  apiKey,
		model:   model,
		baseURL: baseURL,
		client:  &http.Client{Timeout: 120 * time.Second}, // local models can be slow
	}
}

// usesOpenAIFormat reports whether requests go to an OpenAI-compatible server.
func (s *AIItineraryService) usesOpenAIFormat() bool { return s.baseURL != "" }

func (s *AIItineraryService) Configured() bool {
	if s.usesOpenAIFormat() {
		// Local servers commonly need no key; a model name is what's required.
		return s.model != ""
	}
	return s.apiKey != ""
}

// chatText sends a system prompt plus one user message and returns the reply
// text. It hides the difference between Anthropic's /v1/messages shape
// (system as a block list, reply in content[].text) and the OpenAI chat
// completions shape (system as a message, reply in choices[0].message.content).
func (s *AIItineraryService) chatText(ctx context.Context, system, userMsg string, maxTokens int) (string, error) {
	if !s.Configured() {
		return "", ErrAIItineraryNotConfigured
	}

	var (
		url  string
		body map[string]any
	)
	if s.usesOpenAIFormat() {
		url = s.baseURL + "/chat/completions"
		body = map[string]any{
			"model":      s.model,
			"max_tokens": maxTokens,
			"messages": []map[string]any{
				{"role": "system", "content": system},
				{"role": "user", "content": userMsg},
			},
		}
	} else {
		url = anthropicBaseURL + "/v1/messages"
		body = map[string]any{
			"model":      s.model,
			"max_tokens": maxTokens,
			"system": []map[string]any{{
				"type":          "text",
				"text":          system,
				"cache_control": map[string]string{"type": "ephemeral"},
			}},
			"messages": []map[string]any{{"role": "user", "content": userMsg}},
		}
	}

	buf, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if s.usesOpenAIFormat() {
		if s.apiKey != "" {
			req.Header.Set("Authorization", "Bearer "+s.apiKey)
		}
	} else {
		req.Header.Set("x-api-key", s.apiKey)
		req.Header.Set("anthropic-version", "2023-06-01")
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("ai request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ai: status %d", resp.StatusCode)
	}
	return decodeChatReply(resp.Body, s.usesOpenAIFormat())
}

// decodeChatReply pulls the assistant text out of whichever envelope came back.
func decodeChatReply(r io.Reader, openAIFormat bool) (string, error) {
	if openAIFormat {
		var out struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.NewDecoder(r).Decode(&out); err != nil {
			return "", err
		}
		if len(out.Choices) == 0 {
			return "", errors.New("ai: response contained no choices")
		}
		return out.Choices[0].Message.Content, nil
	}

	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.NewDecoder(r).Decode(&out); err != nil {
		return "", err
	}
	// Skip non-text blocks (thinking, tool use) rather than assuming index 0.
	var sb strings.Builder
	for _, c := range out.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String(), nil
}

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

	raw, err := s.chatText(ctx, system, userMsg, 8000)
	if err != nil {
		return nil, err
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

	raw, err := s.chatText(ctx, system, userMsg, 8000)
	if err != nil {
		return nil, err
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
