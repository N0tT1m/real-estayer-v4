package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

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
// The model is never allowed to invent a place, but it is also not confined to
// the places we happen to have stored. Those are different constraints, and
// conflating them is what made an earlier version of this feel like it only
// knew 97 cities: the catalog was used as a whitelist, so a model that
// correctly answered "Banff" for skiing had that answer thrown away. The
// catalog is now a preference, not a boundary. A name that matches it is used
// verbatim; a name that does not is verified against Wikidata and stored as a
// real destination before being returned, and only a name that survives
// neither is dropped.
//
// The invariant that matters is unchanged and is about provenance, not about
// membership: every actionable field — coordinates, country, daily budget — is
// read from a database row or from Wikidata, never from the model. The model
// contributes the name and the prose explaining why the place fits. This
// matters more with a local model than a hosted one: a 27B model will
// cheerfully invent a dive site, a national park, or a season.
type DestinationSuggestService struct {
	ai        *AIItineraryService
	dests     *DestinationService
	discovery *DestinationDiscoveryService
}

// NewDestinationSuggestService wires the finder. discovery may be nil, in
// which case the service degrades to catalog-only matching rather than
// failing — the same fail-soft rule every optional integration follows.
func NewDestinationSuggestService(ai *AIItineraryService, dests *DestinationService, discovery *DestinationDiscoveryService) *DestinationSuggestService {
	return &DestinationSuggestService{ai: ai, dests: dests, discovery: discovery}
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

// ActivityMatch pairs a real destination with the model's reasoning about it.
// Destination is the stored row verbatim.
type ActivityMatch struct {
	Destination models.Destination `json:"destination"`
	Why         string             `json:"why"`
	Activities  []string           `json:"activities,omitempty"`
	Timing      string             `json:"timing,omitempty"`
	// Discovered marks a place that was not in the catalog when this search
	// ran and was resolved through Wikidata to answer it. Worth surfacing:
	// its budget figure is synthesised rather than curated, so it is a
	// weaker number than the one on an established row.
	Discovered bool `json:"discovered,omitempty"`
}

// ActivitySuggestions is the response. Dropped is deliberately visible rather
// than swallowed: with discovery in the path, a name lands there only when
// neither the catalog nor Wikidata could confirm the place exists, which makes
// it a genuine "the model made this up" signal rather than a coverage gap.
type ActivitySuggestions struct {
	Matches []ActivityMatch `json:"matches"`
	Dropped []string        `json:"dropped,omitempty"`
	// CatalogSize is the catalog the model chose from, before any discovery.
	CatalogSize int `json:"catalog_size"`
	// DiscoveredCount is how many matches came from outside it.
	DiscoveredCount int `json:"discovered_count,omitempty"`
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

	out, ungrounded := matchPicks(catalog, wire.Picks, limit)
	s.resolveUngrounded(ctx, out, ungrounded, req, limit)
	return out, nil
}

const suggestSystemPrompt = `You match travellers to destinations they could actually go to.
You will be given a CATALOG of destinations we already cover, and a list of activities.
Respond ONLY with a JSON object, no prose:
{"picks":[{"name":"<destination name>","why":"<1-2 sentences on why this suits the activities>","activities":["<specific thing to do there>"],"timing":"<when to go / season caveat, one line>"}]}
Rules:
- Prefer a CATALOG entry whenever one genuinely suits the request, and copy its name character-for-character.
- The CATALOG is not the whole world. If the best place for these activities is not in it, name that place anyway rather than substituting a worse catalog entry. Answering "Banff" for skiing is better than answering a city that merely has an airport.
- Every name must be a real, well-known place that has an English Wikipedia article. Give the common English name of the place on its own — no country suffix, no descriptive phrases, no invented resorts or venues.
- Name a place at the scale a traveller would go to: a town, city, island, park, or region. Not a single hotel, dive shop, trail, or ski lift.
- If fewer places genuinely suit the activities than requested, return fewer. Do not pad the list.
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
	sb.WriteString("CATALOG — destinations we already cover. Prefer these, but do not be limited to them (one per line, fields separated by |, the NAME is the first field):\n")
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

// matchPicks resolves the model's chosen names against the catalog, returning
// the grounded matches plus the picks it could not place.
//
// Grounding here is only the cheap half — a name already in the catalog needs
// no network call. The picks it hands back are candidates for discovery, not
// yet failures; resolveUngrounded decides which of them are real.
//
// A placeholder slot is appended to Matches for each ungrounded pick so the
// model's ranking survives: discovery is concurrent and would otherwise
// reorder the results by whichever Wikidata lookup finished first.
func matchPicks(catalog []models.Destination, picks []suggestPick, limit int) (*ActivitySuggestions, []ungroundedPick) {
	byName := make(map[string]*models.Destination, len(catalog))
	for i := range catalog {
		byName[normalizeName(catalog[i].Name)] = &catalog[i]
	}

	out := &ActivitySuggestions{CatalogSize: len(catalog)}
	var ungrounded []ungroundedPick
	seen := make(map[string]bool, limit)
	for _, p := range picks {
		name := strings.TrimSpace(p.Name)
		if name == "" {
			continue
		}
		dest, key, ok := resolve(byName, name)
		if !ok {
			key = normalizeName(name)
			if seen[key] {
				continue
			}
			seen[key] = true
			ungrounded = append(ungrounded, ungroundedPick{pick: p, slot: len(out.Matches)})
			out.Matches = append(out.Matches, ActivityMatch{})
			if len(out.Matches) >= limit {
				break
			}
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
	return out, ungrounded
}

// ungroundedPick is a pick awaiting discovery, tagged with the position it
// holds in the result list.
type ungroundedPick struct {
	pick suggestPick
	slot int
}

// discoveryConcurrency bounds the parallel Wikidata/Wikipedia lookups. Each
// resolution is several sequential HTTP calls against public endpoints that
// ask callers to be gentle, and a request only ever has a handful of names to
// resolve, so there is nothing to gain from going wider.
const discoveryConcurrency = 4

// discoveryBudget caps the whole discovery phase. The model call ahead of it
// already spends 10-30s against a local model, and the endpoint runs under a
// 90s write deadline; this keeps a slow Wikidata from eating the difference.
// Whatever has not resolved when it expires is reported as dropped.
const discoveryBudget = 40 * time.Second

// resolveUngrounded fills the placeholder slots left by matchPicks, verifying
// each name against Wikidata and storing it as a destination if it is real.
// Names that do not resolve, or that resolve to somewhere violating a
// constraint the caller set, collapse out of the result and are reported in
// Dropped.
//
// Errors are deliberately not propagated. A Wikidata outage should cost the
// off-catalog half of the answer, not the catalog matches already in hand.
func (s *DestinationSuggestService) resolveUngrounded(ctx context.Context, out *ActivitySuggestions, ungrounded []ungroundedPick, req ActivitySuggestRequest, limit int) {
	if len(ungrounded) == 0 {
		return
	}
	if s.discovery == nil {
		for _, u := range ungrounded {
			out.Dropped = append(out.Dropped, strings.TrimSpace(u.pick.Name))
		}
		compactMatches(out, limit)
		return
	}

	dctx, cancel := context.WithTimeout(ctx, discoveryBudget)
	defer cancel()

	var (
		mu   sync.Mutex
		wg   sync.WaitGroup
		sem  = make(chan struct{}, discoveryConcurrency)
		seen = make(map[string]bool, len(ungrounded))
	)
	for i := range out.Matches {
		if out.Matches[i].Destination.Name != "" {
			seen[normalizeName(out.Matches[i].Destination.Name)] = true
		}
	}

	for _, u := range ungrounded {
		wg.Add(1)
		sem <- struct{}{}
		go func(u ungroundedPick) {
			defer wg.Done()
			defer func() { <-sem }()

			name := strings.TrimSpace(u.pick.Name)
			dest, err := s.discovery.ResolveAndInsertByName(dctx, name)
			if err != nil {
				slog.Debug("suggest: discovery failed", "name", name, "error", err)
			}

			mu.Lock()
			defer mu.Unlock()
			if dest == nil || !suggestionSatisfies(*dest, req) {
				out.Dropped = append(out.Dropped, name)
				return
			}
			// Canonicalisation can land two different picks on the same row
			// ("Banff" and "Banff National Park"), and a discovered row can
			// collide with a catalog match already held.
			key := normalizeName(dest.Name)
			if seen[key] {
				return
			}
			seen[key] = true

			out.Matches[u.slot] = ActivityMatch{
				Destination: *dest,
				Why:         strings.TrimSpace(u.pick.Why),
				Activities:  trimmedNonEmpty(u.pick.Activities),
				Timing:      strings.TrimSpace(u.pick.Timing),
				Discovered:  true,
			}
			out.DiscoveredCount++
		}(u)
	}
	wg.Wait()

	compactMatches(out, limit)
}

// suggestionSatisfies re-checks the constraints that Mongo enforced on the
// catalog against a row that never went through that query. A discovered
// place has a synthesised budget, so this is a sanity floor rather than a
// precise filter — but returning a $300/day city to someone who asked for
// $80/day is worse than returning one fewer result.
func suggestionSatisfies(d models.Destination, req ActivitySuggestRequest) bool {
	if req.MaxBudget > 0 && d.AvgDailyBudget > req.MaxBudget {
		return false
	}
	if r := strings.TrimSpace(req.Region); r != "" && !strings.EqualFold(d.Region, r) {
		return false
	}
	return true
}

// compactMatches drops the placeholder slots left by picks that never
// resolved, preserving the model's ranking among those that did.
func compactMatches(out *ActivitySuggestions, limit int) {
	kept := out.Matches[:0]
	for _, m := range out.Matches {
		if m.Destination.Name == "" {
			continue
		}
		kept = append(kept, m)
	}
	out.Matches = kept
	if len(out.Matches) > limit {
		out.Matches = out.Matches[:limit]
	}
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
