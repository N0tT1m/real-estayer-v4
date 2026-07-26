package service

import (
	"context"
	"strings"
	"time"
)

// VisaService answers "as a citizen of X, do I need a visa to enter Y?"
// There is no great free API for this (Sherpa and iVisa gate behind partner
// contracts). We ship a curated starter table for the most common routes and
// expose the service behind a clean interface so a paid provider can be
// slotted in later without touching the UI.
//
// Source of truth for the static data is each destination country's publicly
// published policy as of late 2024. Use at your own risk; always double-
// check before booking.
type VisaService struct{}

func NewVisaService() *VisaService { return &VisaService{} }

// Kind categorises the strongest form of visa-free access available.
type Kind string

const (
	KindVisaFree      Kind = "visa_free"     // passport stamp only
	KindETA           Kind = "eta"           // electronic travel authorization pre-arrival
	KindVisaOnArrival Kind = "voa"           // visa issued on landing
	KindVisaRequired  Kind = "visa_required" // embassy/consulate required
	KindNotAvailable  Kind = "not_available" // no data
)

// Requirement is the view-model.
type Requirement struct {
	Citizenship string `json:"citizenship"`
	Destination string `json:"destination"`
	Kind        Kind   `json:"kind"`
	Label       string `json:"label"`
	MaxStayDays int    `json:"max_stay_days,omitempty"`
	Notes       string `json:"notes,omitempty"`
	OfficialURL string `json:"official_url,omitempty"`
	UpdatedAt   string `json:"updated_at"`
}

// Check returns the best-known requirement. Codes are ISO-3166-1 alpha-2,
// case-insensitive.
func (s *VisaService) Check(_ context.Context, citizenship, destination string) *Requirement {
	c := strings.ToUpper(strings.TrimSpace(citizenship))
	d := strings.ToUpper(strings.TrimSpace(destination))
	updatedAt := time.Now().Format("2006-01-02")
	if c == "" || d == "" {
		return &Requirement{Kind: KindNotAvailable, Label: "Please set your citizenship in your profile.", UpdatedAt: updatedAt}
	}
	if c == d {
		return &Requirement{Citizenship: c, Destination: d, Kind: KindVisaFree, Label: "You're a citizen — no visa needed.", UpdatedAt: updatedAt}
	}
	if r, ok := visaMatrix[key(c, d)]; ok {
		out := r
		out.Citizenship, out.Destination, out.UpdatedAt = c, d, updatedAt
		return &out
	}
	// Default to "not available" — do not assume visa-free when we don't
	// have data. Encourages users to verify.
	return &Requirement{
		Citizenship: c, Destination: d,
		Kind:        KindNotAvailable,
		Label:       "No saved policy — check the destination's official site.",
		OfficialURL: "https://" + strings.ToLower(d) + ".visahq.com/",
		UpdatedAt:   updatedAt,
	}
}

func key(citizenship, destination string) string {
	return citizenship + "→" + destination
}

// visaMatrix is the starter set. Populate only the most common traveller
// flows; anything missing returns "not available" rather than a dangerous
// default. Entries are keyed "CITIZEN→DESTINATION".
var visaMatrix = map[string]Requirement{
	// US citizens
	"US→CA": {Kind: KindVisaFree, Label: "Visa-free for stays up to 180 days.", MaxStayDays: 180},
	"US→MX": {Kind: KindVisaFree, Label: "FMM tourist permit on arrival; up to 180 days.", MaxStayDays: 180},
	"US→GB": {Kind: KindVisaFree, Label: "Visa-free for stays up to 6 months.", MaxStayDays: 180},
	"US→FR": {Kind: KindVisaFree, Label: "Schengen: 90 days in any 180-day window.", MaxStayDays: 90, Notes: "ETIAS pre-authorisation expected to be required from 2025."},
	"US→IT": {Kind: KindVisaFree, Label: "Schengen: 90 days in any 180-day window.", MaxStayDays: 90},
	"US→ES": {Kind: KindVisaFree, Label: "Schengen: 90 days in any 180-day window.", MaxStayDays: 90},
	"US→DE": {Kind: KindVisaFree, Label: "Schengen: 90 days in any 180-day window.", MaxStayDays: 90},
	"US→PT": {Kind: KindVisaFree, Label: "Schengen: 90 days in any 180-day window.", MaxStayDays: 90},
	"US→JP": {Kind: KindVisaFree, Label: "Visa-free for stays up to 90 days.", MaxStayDays: 90},
	"US→AU": {Kind: KindETA, Label: "ETA required (AUD $20).", MaxStayDays: 90, OfficialURL: "https://immi.homeaffairs.gov.au/visas/getting-a-visa/visa-finder/visit/evisitor-651"},
	"US→NZ": {Kind: KindETA, Label: "NZeTA required (NZD $17–$23).", MaxStayDays: 90, OfficialURL: "https://www.immigration.govt.nz/new-zealand-visas/apply-for-a-visa/about-visa/nzeta"},
	"US→CN": {Kind: KindVisaRequired, Label: "Tourist visa (L) required in advance."},
	"US→IN": {Kind: KindETA, Label: "e-Visa online, $25–$100 depending on duration.", OfficialURL: "https://indianvisaonline.gov.in/"},
	"US→TH": {Kind: KindVisaFree, Label: "Visa-free for stays up to 60 days.", MaxStayDays: 60},
	"US→BR": {Kind: KindVisaFree, Label: "Visa-free for stays up to 90 days (as of 2024).", MaxStayDays: 90},
	"US→AE": {Kind: KindVisaOnArrival, Label: "Visa on arrival, 30 days.", MaxStayDays: 30},
	"US→ZA": {Kind: KindVisaFree, Label: "Visa-free for stays up to 90 days.", MaxStayDays: 90},

	// UK citizens
	"GB→US": {Kind: KindETA, Label: "ESTA required ($21), valid 2 years.", MaxStayDays: 90, OfficialURL: "https://esta.cbp.dhs.gov/"},
	"GB→FR": {Kind: KindVisaFree, Label: "Schengen: 90 days in any 180-day window.", MaxStayDays: 90},
	"GB→IT": {Kind: KindVisaFree, Label: "Schengen: 90 days in any 180-day window.", MaxStayDays: 90},
	"GB→ES": {Kind: KindVisaFree, Label: "Schengen: 90 days in any 180-day window.", MaxStayDays: 90},
	"GB→DE": {Kind: KindVisaFree, Label: "Schengen: 90 days in any 180-day window.", MaxStayDays: 90},
	"GB→PT": {Kind: KindVisaFree, Label: "Schengen: 90 days in any 180-day window.", MaxStayDays: 90},
	"GB→JP": {Kind: KindVisaFree, Label: "Visa-free for stays up to 90 days.", MaxStayDays: 90},
	"GB→AU": {Kind: KindETA, Label: "ETA required.", MaxStayDays: 90},
	"GB→CA": {Kind: KindETA, Label: "eTA required (CAD $7).", OfficialURL: "https://www.canada.ca/en/immigration-refugees-citizenship/services/visit-canada/eta.html"},
	"GB→IN": {Kind: KindETA, Label: "e-Visa online."},
	"GB→CN": {Kind: KindVisaRequired, Label: "Tourist visa required."},
	"GB→TH": {Kind: KindVisaFree, Label: "Visa-free for stays up to 60 days.", MaxStayDays: 60},
	"GB→AE": {Kind: KindVisaFree, Label: "Visa-free for stays up to 30 days.", MaxStayDays: 30},

	// Canadian citizens
	"CA→US": {Kind: KindVisaFree, Label: "Visa-free land/air for stays up to 180 days.", MaxStayDays: 180},
	"CA→GB": {Kind: KindVisaFree, Label: "Visa-free for stays up to 6 months.", MaxStayDays: 180},
	"CA→FR": {Kind: KindVisaFree, Label: "Schengen: 90 days in any 180-day window.", MaxStayDays: 90},
	"CA→JP": {Kind: KindVisaFree, Label: "Visa-free for stays up to 90 days.", MaxStayDays: 90},
	"CA→AU": {Kind: KindETA, Label: "ETA required.", MaxStayDays: 90},

	// EU (FR/DE/IT/ES/PT) citizens heading out
	"FR→US": {Kind: KindETA, Label: "ESTA required ($21).", MaxStayDays: 90},
	"DE→US": {Kind: KindETA, Label: "ESTA required ($21).", MaxStayDays: 90},
	"IT→US": {Kind: KindETA, Label: "ESTA required ($21).", MaxStayDays: 90},
	"ES→US": {Kind: KindETA, Label: "ESTA required ($21).", MaxStayDays: 90},
	"PT→US": {Kind: KindETA, Label: "ESTA required ($21).", MaxStayDays: 90},
	"FR→JP": {Kind: KindVisaFree, Label: "Visa-free 90 days.", MaxStayDays: 90},
	"DE→JP": {Kind: KindVisaFree, Label: "Visa-free 90 days.", MaxStayDays: 90},
	"FR→CN": {Kind: KindVisaFree, Label: "Visa-free 15 days (2024 policy).", MaxStayDays: 15, Notes: "Policy extends periodically — verify before flying."},
	"DE→CN": {Kind: KindVisaFree, Label: "Visa-free 15 days (2024 policy).", MaxStayDays: 15, Notes: "Policy extends periodically — verify before flying."},

	// Australian citizens
	"AU→US": {Kind: KindETA, Label: "ESTA required ($21).", MaxStayDays: 90},
	"AU→GB": {Kind: KindVisaFree, Label: "Visa-free for stays up to 6 months.", MaxStayDays: 180},
	"AU→JP": {Kind: KindVisaFree, Label: "Visa-free for stays up to 90 days.", MaxStayDays: 90},
	"AU→NZ": {Kind: KindVisaFree, Label: "Visa-free as under the Trans-Tasman arrangement.", MaxStayDays: 0, Notes: "No hard limit; residency granted on arrival."},
	"AU→TH": {Kind: KindVisaFree, Label: "Visa-free 60 days.", MaxStayDays: 60},

	// Japanese citizens
	"JP→US": {Kind: KindETA, Label: "ESTA required ($21).", MaxStayDays: 90},
	"JP→FR": {Kind: KindVisaFree, Label: "Schengen: 90 days.", MaxStayDays: 90},
	"JP→GB": {Kind: KindVisaFree, Label: "Visa-free up to 6 months.", MaxStayDays: 180},

	// Indian citizens
	"IN→US": {Kind: KindVisaRequired, Label: "B1/B2 visa required; appointment backlogs can be long."},
	"IN→GB": {Kind: KindVisaRequired, Label: "Standard Visitor Visa required."},
	"IN→CN": {Kind: KindVisaRequired, Label: "Tourist visa (L) required."},
	"IN→JP": {Kind: KindVisaRequired, Label: "Tourist visa required."},
	"IN→AE": {Kind: KindVisaOnArrival, Label: "Visa on arrival, 14 days.", MaxStayDays: 14},
	"IN→TH": {Kind: KindVisaFree, Label: "Visa-free 30 days (as of 2024).", MaxStayDays: 30, Notes: "Temporary scheme — verify before booking."},
}
