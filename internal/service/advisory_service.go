package service

import (
	"context"
	"strings"
	"time"
)

// AdvisoryService surfaces short, plain-language travel guidance for a country.
// Real government advisories are behind tangled feeds (State Dept JSON,
// UK FCDO HTML, etc.) and go stale quickly. Rather than bake in a brittle
// scraper, we ship a curated fallback table and expose a structured shape so
// a richer fetch can be slotted in later.
type AdvisoryService struct{}

func NewAdvisoryService() *AdvisoryService { return &AdvisoryService{} }

// Advisory is the view-model.
type Advisory struct {
	Level        int    `json:"level"`        // 1 exercise normal precautions … 4 do not travel
	LevelLabel   string `json:"level_label"`
	Summary      string `json:"summary"`
	Source       string `json:"source"`       // "curated" | "state.gov" | "fcdo"
	SourceURL    string `json:"source_url,omitempty"`
	UpdatedAt    string `json:"updated_at"`
}

// ForCountry returns the best-known advisory. Always non-nil: unknown
// countries get a level-1 "no specific advisory" response so the UI can show
// the panel consistently.
func (s *AdvisoryService) ForCountry(ctx context.Context, code string) *Advisory {
	code = strings.ToUpper(strings.TrimSpace(code))
	if a, ok := curatedAdvisories[code]; ok {
		a.UpdatedAt = time.Now().Format("2006-01-02")
		return &a
	}
	return &Advisory{
		Level:      1,
		LevelLabel: "Exercise normal precautions",
		Summary:    "No specific advisory. Follow standard precautions, keep travel documents secure, and register with your embassy for long stays.",
		Source:     "curated",
		UpdatedAt:  time.Now().Format("2006-01-02"),
	}
}

// curatedAdvisories is a lightweight static table — a sensible default rather
// than an authoritative source. Operators should either keep it current in
// code review or wire the State Dept / FCDO feeds in.
var curatedAdvisories = map[string]Advisory{
	"US": {Level: 1, LevelLabel: "Exercise normal precautions", Summary: "Watch for local crime in major cities; standard precautions apply.", Source: "curated"},
	"GB": {Level: 1, LevelLabel: "Exercise normal precautions", Summary: "Threat from terrorism is moderate; vigilance in crowded places.", Source: "curated"},
	"FR": {Level: 2, LevelLabel: "Exercise increased caution", Summary: "Heightened vigilance in Paris and other large cities after recent events.", Source: "curated"},
	"IT": {Level: 2, LevelLabel: "Exercise increased caution", Summary: "Pickpocketing common in tourist areas; watch for scams near major sights.", Source: "curated"},
	"ES": {Level: 2, LevelLabel: "Exercise increased caution", Summary: "Pickpocketing is common in Barcelona, Madrid, and on public transit.", Source: "curated"},
	"MX": {Level: 3, LevelLabel: "Reconsider travel to some areas", Summary: "Several states have active travel restrictions; tourist zones in Yucatan and Baja California Sur remain generally safe.", Source: "curated"},
	"BR": {Level: 2, LevelLabel: "Exercise increased caution", Summary: "Crime, including violent crime, can occur in large cities; avoid unlit areas after dark.", Source: "curated"},
	"JP": {Level: 1, LevelLabel: "Exercise normal precautions", Summary: "Very low crime; the main risks are natural disasters and transit strikes.", Source: "curated"},
	"TH": {Level: 2, LevelLabel: "Exercise increased caution", Summary: "Political demonstrations occur occasionally; avoid border regions with Myanmar.", Source: "curated"},
	"AE": {Level: 2, LevelLabel: "Exercise increased caution", Summary: "Penalties for drug and alcohol offenses are severe. Respect local customs.", Source: "curated"},
	"IN": {Level: 2, LevelLabel: "Exercise increased caution", Summary: "Scams target tourists in Delhi/Agra; avoid border areas with Pakistan.", Source: "curated"},
	"CN": {Level: 3, LevelLabel: "Reconsider travel", Summary: "Arbitrary enforcement of local laws; ensure visa paperwork is in perfect order.", Source: "curated"},
	"ZA": {Level: 2, LevelLabel: "Exercise increased caution", Summary: "Crime is widespread outside tourist hubs; self-drive with caution.", Source: "curated"},
}
