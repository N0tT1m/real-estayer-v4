package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/realestayer/v4/internal/models"
)

// ErrNoDestinationCatalog means the destinations collection had nothing to
// offer for the given constraints. The seed runs at boot, so an empty catalog
// on an unconstrained request usually means the seed has not finished yet.
var ErrNoDestinationCatalog = errors.New("destination suggest: no destinations match those constraints")

// ErrInvalidSuggestRequest wraps every caller-input problem so the handler can
// answer 400 without matching on error strings. Details come from the wrapping
// message.
var ErrInvalidSuggestRequest = errors.New("invalid suggestion request")

// DestinationSuggestService answers "I want to do X — where should I go?",
// which is the inverse of AIItineraryService: that one needs the destination
// up front and plans activities inside it.
//
// The model is never allowed to invent a place. It is handed the destination
// catalog and asked to choose from it, and anything it names that is not in
// the catalog is dropped rather than returned. Every actionable field —
// coordinates, airport code, daily budget — is read from the database row;
// the model contributes only the prose explaining why the place fits. This
// matters more with a local model than a hosted one: a 27B model will
// cheerfully invent a dive site, a national park, or a season.
type DestinationSuggestService struct {
	ai    *AIItineraryService
	dests *DestinationService
}

func NewDestinationSuggestService(ai *AIItineraryService, dests *DestinationService) *DestinationSuggestService {
	return &DestinationSuggestService{ai: ai, dests: dests}
}

// Configured reports whether both halves are available. Without the catalog
// there is nothing to ground against, so this is not a model-only check.
func (s *DestinationSuggestService) Configured() bool {
	return s.ai != nil && s.ai.Configured() && s.dests != nil
}

const (
	suggestDefaultLimit = 5
	suggestMaxLimit     = 10
	// catalogCap bounds how much of the catalog goes into the prompt. The
	// seed ships 97 cities, so this is headroom for admin-added ones rather
	// than a limit anyone is expected to hit.
	catalogCap = 400
)

// ActivitySuggestRequest is the input: what you want to do, plus optional
// constraints to narrow the catalog before the model ever sees it.
type ActivitySuggestRequest struct {
	Activities []string `json:"activities"`
	Month      int      `json:"month,omitempty"`            // 1-12, advisory only
	MaxBudget  float64  `json:"max_daily_budget,omitempty"` // USD/day
	Region     string   `json:"region,omitempty"`
	Limit      int      `json:"limit,omitempty"`
}

// ActivityMatch pairs a real catalog destination with the model's reasoning
// about it. Destination is the stored row verbatim.
type ActivityMatch struct {
	Destination models.Destination `json:"destination"`
	Why         string             `json:"why"`
	Activities  []string           `json:"activities,omitempty"`
	Timing      string             `json:"timing,omitempty"`
}

// ActivitySuggestions is the response. Dropped is deliberately visible rather
// than swallowed: it is the honest signal that the model reached for something
// outside the catalog, and it doubles as a shortlist of cities worth adding
// through the admin discovery flow.
type ActivitySuggestions struct {
	Matches     []ActivityMatch `json:"matches"`
	Dropped     []string        `json:"dropped,omitempty"`
	CatalogSize int             `json:"catalog_size"`
}

// Suggest picks destinations from the catalog that suit the requested
// activities.
func (s *DestinationSuggestService) Suggest(ctx context.Context, req ActivitySuggestRequest) (*ActivitySuggestions, error) {
	if !s.Configured() {
		return nil, ErrAIItineraryNotConfigured
	}
	activities := trimmedNonEmpty(req.Activities)
	if len(activities) == 0 {
		return nil, fmt.Errorf("%w: at least one activity is required", ErrInvalidSuggestRequest)
	}
	if req.Month < 0 || req.Month > 12 {
		return nil, fmt.Errorf("%w: month must be between 1 and 12", ErrInvalidSuggestRequest)
	}
	if req.MaxBudget < 0 {
		return nil, fmt.Errorf("%w: max_daily_budget cannot be negative", ErrInvalidSuggestRequest)
	}
	limit := req.Limit
	if limit <= 0 {
		limit = suggestDefaultLimit
	}
	if limit > suggestMaxLimit {
		limit = suggestMaxLimit
	}

	// Narrow in Mongo wherever the stored data actually supports it, so the
	// model only ever sees candidates that already satisfy the hard
	// constraints. Month is deliberately NOT filtered here: best_months is
	// never populated by the seed, so filtering on it would match nothing at
	// all. It goes to the model as advisory context instead, and the caller
	// gets it back as prose in Timing rather than as a guarantee.
	catalog, _, err := s.dests.SearchDestinations(ctx, models.DestinationFilter{
		Region:    strings.TrimSpace(req.Region),
		MaxBudget: req.MaxBudget,
		SortBy:    "popularity",
		Limit:     catalogCap,
	})
	if err != nil {
		return nil, fmt.Errorf("load destination catalog: %w", err)
	}
	if len(catalog) == 0 {
		return nil, ErrNoDestinationCatalog
	}

	raw, err := s.ai.chatText(ctx, suggestSystemPrompt, buildSuggestPrompt(catalog, activities, req, limit), 2000)
	if err != nil {
		return nil, err
	}

	var wire struct {
		Picks []suggestPick `json:"picks"`
	}
	cleaned := stripCodeFence(strings.TrimSpace(raw))
	if err := json.Unmarshal([]byte(cleaned), &wire); err != nil {
		return nil, fmt.Errorf("suggest parse failed: %w (raw: %.200s)", err, cleaned)
	}

	return matchPicks(catalog, wire.Picks, limit), nil
}

const suggestSystemPrompt = `You match travellers to destinations they could actually book.
You will be given a numbered CATALOG of destinations and a list of activities.
Respond ONLY with a JSON object, no prose:
{"picks":[{"name":"<exact name from the CATALOG>","why":"<1-2 sentences on why this suits the activities>","activities":["<specific thing to do there>"],"timing":"<when to go / season caveat, one line>"}]}
Rules:
- Choose ONLY from the CATALOG. Copy names character-for-character.
- If fewer catalog entries genuinely suit the activities than requested, return fewer. Do not pad the list.
- Rank best match first.
- "activities" must be concrete and specific to that place, not restatements of the request.
- Never invent venues, operators, or prices. If unsure of a specific, describe the activity generically.
- If a month is given, say in "timing" whether it is a good time and why.`

// buildSuggestPrompt renders the catalog and the ask. The catalog goes in the
// user message rather than the system prompt because it varies per request
// (region and budget filters change it), and the system prompt is the half
// that benefits from Anthropic's cache_control.
func buildSuggestPrompt(catalog []models.Destination, activities []string, req ActivitySuggestRequest, limit int) string {
	var sb strings.Builder
	// Pipe-delimited, name first. An earlier "Name, Country (Region)" layout
	// read as one field to the model, which then returned "Quito, Ecuador" as
	// the name and failed to match the "Quito" row — three of four picks were
	// wrongly dropped. Keep the name in its own column and commas out of the
	// separator.
	sb.WriteString("CATALOG (one per line, fields separated by |, the NAME is the first field):\n")
	for _, d := range catalog {
		fmt.Fprintf(&sb, "- %s", d.Name)
		if d.Country != "" {
			fmt.Fprintf(&sb, " | %s", d.Country)
		}
		if d.Region != "" {
			fmt.Fprintf(&sb, " | %s", d.Region)
		}
		if len(d.Categories) > 0 {
			fmt.Fprintf(&sb, " | %s", strings.Join(d.Categories, "/"))
		}
		if d.AvgDailyBudget > 0 {
			fmt.Fprintf(&sb, " | ~$%.0f/day", d.AvgDailyBudget)
		}
		sb.WriteString("\n")
	}

	fmt.Fprintf(&sb, "\nThe traveller wants to: %s\n", strings.Join(activities, ", "))
	if req.Month >= 1 && req.Month <= 12 {
		fmt.Fprintf(&sb, "Travelling in: %s\n", monthName(req.Month))
	}
	if req.MaxBudget > 0 {
		fmt.Fprintf(&sb, "Budget ceiling: $%.0f per day (every catalog entry already fits this).\n", req.MaxBudget)
	}
	fmt.Fprintf(&sb, "Return at most %d picks, best first. JSON only.", limit)
	return sb.String()
}

// suggestPick is one entry as the model returns it, before grounding.
type suggestPick struct {
	Name       string   `json:"name"`
	Why        string   `json:"why"`
	Activities []string `json:"activities"`
	Timing     string   `json:"timing"`
}

// matchPicks resolves the model's chosen names against the catalog. This is
// the grounding step: a name that is not in the catalog is reported in
// Dropped, never returned as a result.
func matchPicks(catalog []models.Destination, picks []suggestPick, limit int) *ActivitySuggestions {
	byName := make(map[string]*models.Destination, len(catalog))
	for i := range catalog {
		byName[normalizeName(catalog[i].Name)] = &catalog[i]
	}

	out := &ActivitySuggestions{CatalogSize: len(catalog)}
	seen := make(map[string]bool, limit)
	for _, p := range picks {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			continue
		}
		dest, key, ok := resolve(byName, name)
		if !ok {
			out.Dropped = append(out.Dropped, name)
			continue
		}
		if seen[key] {
			continue
		}
		seen[key] = true

		out.Matches = append(out.Matches, ActivityMatch{
			Destination: *dest,
			Why:         strings.TrimSpace(p.Why),
			Activities:  trimmedNonEmpty(p.Activities),
			Timing:      strings.TrimSpace(p.Timing),
		})
		if len(out.Matches) >= limit {
			break
		}
	}
	return out
}

// resolve looks a model-supplied name up in the catalog index, returning the
// row and the key it matched under.
//
// The retry on the pre-comma segment is not cosmetic: models qualify a city
// with its country ("Quito, Ecuador") even when told to copy the name exactly,
// and without this those picks are dropped as if they were invented. It stays
// safe because the exact match is tried first, so "Porto Alegre, Brazil" can
// never be captured by a "Porto" row.
func resolve(byName map[string]*models.Destination, name string) (*models.Destination, string, bool) {
	key := normalizeName(name)
	if dest, ok := byName[key]; ok {
		return dest, key, true
	}
	if base, _, found := strings.Cut(name, ","); found {
		key = normalizeName(base)
		if dest, ok := byName[key]; ok {
			return dest, key, true
		}
	}
	return nil, "", false
}

// normalizeName makes catalog matching forgiving of the casing and spacing
// drift a model introduces, without being so loose that two different cities
// collide.
func normalizeName(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
}

func trimmedNonEmpty(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if t := strings.TrimSpace(s); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func monthName(m int) string {
	if m < 1 || m > 12 {
		return ""
	}
	return [...]string{
		"January", "February", "March", "April", "May", "June",
		"July", "August", "September", "October", "November", "December",
	}[m-1]
}
