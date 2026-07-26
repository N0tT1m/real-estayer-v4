package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// CountryService hydrates a destination with travel-ready context: visa-ish
// hints (calling codes, currency, languages, flag) from REST Countries, plus
// a static fact table for plug type, voltage, tipping culture, emergency
// numbers, and a handful of key phrases. Public holidays are fetched lazily
// from Nager.Date when needed.
//
// Both upstream APIs are free and keyless. We cache everything in-process for
// 24 h — country facts almost never change between releases.
type CountryService struct {
	client       *http.Client
	mu           sync.RWMutex
	basics       map[string]*CountryBasics // keyed by ISO-3166-1 alpha-2, uppercase
	holidayCache map[string]holidayCacheEntry
}

type holidayCacheEntry struct {
	value     []PublicHoliday
	expiresAt time.Time
}

// CountryBasics bundles the fields pulled from REST Countries + our static
// extras into one serializable blob the UI can render directly.
type CountryBasics struct {
	Code         string   `json:"code"`
	Name         string   `json:"name"`
	OfficialName string   `json:"official_name,omitempty"`
	Capital      string   `json:"capital,omitempty"`
	Region       string   `json:"region,omitempty"`
	Subregion    string   `json:"subregion,omitempty"`
	Continent    string   `json:"continent,omitempty"`
	Languages    []string `json:"languages,omitempty"`
	Currencies   []string `json:"currencies,omitempty"`
	CallingCodes []string `json:"calling_codes,omitempty"`
	Timezones    []string `json:"timezones,omitempty"`
	FlagEmoji    string   `json:"flag_emoji,omitempty"`
	FlagImageURL string   `json:"flag_image_url,omitempty"`
	MapURL       string   `json:"map_url,omitempty"`
	DrivesOn     string   `json:"drives_on,omitempty"`

	// Static extras — curated below.
	PlugTypes     []string          `json:"plug_types,omitempty"`
	Voltage       string            `json:"voltage,omitempty"`
	Frequency     string            `json:"frequency,omitempty"`
	Tipping       string            `json:"tipping,omitempty"`
	Emergency     map[string]string `json:"emergency,omitempty"`
	KeyPhrases    map[string]string `json:"key_phrases,omitempty"`
	WaterSafe     string            `json:"water_safe,omitempty"`
	CardsAccepted string            `json:"cards_accepted,omitempty"`
}

// PublicHoliday is a single national holiday returned by Nager.Date.
type PublicHoliday struct {
	Date        string   `json:"date"`
	LocalName   string   `json:"local_name"`
	Name        string   `json:"name"`
	CountryCode string   `json:"country_code"`
	Global      bool     `json:"global"`
	Counties    []string `json:"counties,omitempty"`
	Types       []string `json:"types,omitempty"`
}

func NewCountryService() *CountryService {
	return &CountryService{
		client:       &http.Client{Timeout: 8 * time.Second},
		basics:       map[string]*CountryBasics{},
		holidayCache: map[string]holidayCacheEntry{},
	}
}

// Basics returns merged REST-Countries + static data for the given alpha-2
// code. Returns nil (and no error) if the code is unknown and the upstream
// service doesn't know it either — the UI should hide the fact panel rather
// than surface a scary error.
func (s *CountryService) Basics(ctx context.Context, code string) (*CountryBasics, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return nil, nil
	}
	s.mu.RLock()
	if v, ok := s.basics[code]; ok {
		s.mu.RUnlock()
		return v, nil
	}
	s.mu.RUnlock()

	out, err := s.fetchBasics(ctx, code)
	if err != nil {
		// Fall back to the static table so we at least return plug/voltage.
		if extras, ok := staticCountryExtras[code]; ok {
			out = &CountryBasics{Code: code, Name: code}
			applyStaticExtras(out, extras)
			s.put(code, out)
			return out, nil
		}
		return nil, err
	}
	if extras, ok := staticCountryExtras[code]; ok {
		applyStaticExtras(out, extras)
	}
	s.put(code, out)
	return out, nil
}

func (s *CountryService) put(code string, b *CountryBasics) {
	s.mu.Lock()
	s.basics[code] = b
	s.mu.Unlock()
}

func (s *CountryService) fetchBasics(ctx context.Context, code string) (*CountryBasics, error) {
	// Ask REST Countries only for the fields we render.
	fields := "name,cca2,capital,region,subregion,continents,languages,currencies,idd,timezones,flag,flags,maps,car"
	url := fmt.Sprintf("https://restcountries.com/v3.1/alpha/%s?fields=%s", code, fields)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("restcountries: status %d", resp.StatusCode)
	}

	var raw struct {
		Name struct {
			Common   string `json:"common"`
			Official string `json:"official"`
		} `json:"name"`
		CCA2       string            `json:"cca2"`
		Capital    []string          `json:"capital"`
		Region     string            `json:"region"`
		Subregion  string            `json:"subregion"`
		Continents []string          `json:"continents"`
		Languages  map[string]string `json:"languages"`
		Currencies map[string]struct {
			Name   string `json:"name"`
			Symbol string `json:"symbol"`
		} `json:"currencies"`
		Idd struct {
			Root     string   `json:"root"`
			Suffixes []string `json:"suffixes"`
		} `json:"idd"`
		Timezones []string `json:"timezones"`
		Flag      string   `json:"flag"`
		Flags     struct {
			SVG string `json:"svg"`
			PNG string `json:"png"`
		} `json:"flags"`
		Maps struct {
			GoogleMaps     string `json:"googleMaps"`
			OpenStreetMaps string `json:"openStreetMaps"`
		} `json:"maps"`
		Car struct {
			Side string `json:"side"`
		} `json:"car"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	out := &CountryBasics{
		Code:         raw.CCA2,
		Name:         raw.Name.Common,
		OfficialName: raw.Name.Official,
		Region:       raw.Region,
		Subregion:    raw.Subregion,
		Timezones:    raw.Timezones,
		FlagEmoji:    raw.Flag,
		FlagImageURL: raw.Flags.SVG,
		MapURL:       raw.Maps.OpenStreetMaps,
		DrivesOn:     raw.Car.Side,
	}
	if len(raw.Capital) > 0 {
		out.Capital = raw.Capital[0]
	}
	if len(raw.Continents) > 0 {
		out.Continent = raw.Continents[0]
	}
	for _, lang := range raw.Languages {
		out.Languages = append(out.Languages, lang)
	}
	for code, cur := range raw.Currencies {
		out.Currencies = append(out.Currencies, fmt.Sprintf("%s (%s %s)", cur.Name, code, cur.Symbol))
	}
	for _, suffix := range raw.Idd.Suffixes {
		out.CallingCodes = append(out.CallingCodes, raw.Idd.Root+suffix)
	}
	return out, nil
}

// Holidays fetches public holidays for the given year from Nager.Date.
// Cached 24 h per (code,year).
func (s *CountryService) Holidays(ctx context.Context, code string, year int) ([]PublicHoliday, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" || year == 0 {
		return nil, nil
	}
	key := fmt.Sprintf("%s:%d", code, year)
	s.mu.RLock()
	if v, ok := s.holidayCache[key]; ok && time.Now().Before(v.expiresAt) {
		s.mu.RUnlock()
		return v.value, nil
	}
	s.mu.RUnlock()

	url := fmt.Sprintf("https://date.nager.at/api/v3/PublicHolidays/%d/%s", year, code)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		s.mu.Lock()
		s.holidayCache[key] = holidayCacheEntry{value: nil, expiresAt: time.Now().Add(24 * time.Hour)}
		s.mu.Unlock()
		return nil, nil
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nager.date: status %d", resp.StatusCode)
	}
	var raw []struct {
		Date        string   `json:"date"`
		LocalName   string   `json:"localName"`
		Name        string   `json:"name"`
		CountryCode string   `json:"countryCode"`
		Global      bool     `json:"global"`
		Counties    []string `json:"counties"`
		Types       []string `json:"types"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}
	out := make([]PublicHoliday, 0, len(raw))
	for _, r := range raw {
		out = append(out, PublicHoliday(r))
	}
	s.mu.Lock()
	s.holidayCache[key] = holidayCacheEntry{value: out, expiresAt: time.Now().Add(24 * time.Hour)}
	s.mu.Unlock()
	return out, nil
}

// HolidaysInRange returns holidays that fall inside [start,end]. Spans
// multiple calendar years if needed.
func (s *CountryService) HolidaysInRange(ctx context.Context, code string, start, end time.Time) ([]PublicHoliday, error) {
	if end.Before(start) {
		return nil, nil
	}
	out := []PublicHoliday{}
	for y := start.Year(); y <= end.Year(); y++ {
		hs, err := s.Holidays(ctx, code, y)
		if err != nil {
			return nil, err
		}
		for _, h := range hs {
			t, err := time.Parse("2006-01-02", h.Date)
			if err != nil {
				continue
			}
			if !t.Before(start) && !t.After(end) {
				out = append(out, h)
			}
		}
	}
	return out, nil
}

// --- Static extras table ---
// Intentionally small; expand as needed. Data compiled from publicly-
// available sources (government travel advisories, Wikipedia, etc.) and
// represents common conventions rather than legal requirements.

type staticExtras struct {
	PlugTypes []string
	Voltage   string
	Frequency string
	Tipping   string
	Emergency map[string]string
	Phrases   map[string]string
	WaterSafe string
	Cards     string
}

var staticCountryExtras = map[string]staticExtras{
	"US": {
		PlugTypes: []string{"A", "B"}, Voltage: "120V", Frequency: "60Hz",
		Tipping:   "Expected: 18–22% at restaurants; $1–2/bag for porters; $2–5/night for housekeeping.",
		Emergency: map[string]string{"all": "911"},
		Phrases:   map[string]string{"hello": "Hello", "thank you": "Thank you", "please": "Please"},
		WaterSafe: "Tap water is safe virtually everywhere.",
		Cards:     "Cards accepted almost universally; carry a $20 bill as backup.",
	},
	"GB": {
		PlugTypes: []string{"G"}, Voltage: "230V", Frequency: "50Hz",
		Tipping:   "Optional. 10–12.5% at sit-down restaurants if service charge isn't included.",
		Emergency: map[string]string{"all": "999", "non-urgent": "101"},
		Phrases:   map[string]string{"hello": "Hello", "thank you": "Thank you", "please": "Please"},
		WaterSafe: "Tap water is safe.",
		Cards:     "Contactless and chip+PIN everywhere. Cash increasingly rare.",
	},
	"FR": {
		PlugTypes: []string{"C", "E"}, Voltage: "230V", Frequency: "50Hz",
		Tipping:   "Service compris — rounding up or leaving a euro or two is generous.",
		Emergency: map[string]string{"all": "112", "police": "17", "medical": "15", "fire": "18"},
		Phrases:   map[string]string{"hello": "Bonjour", "thank you": "Merci", "please": "S'il vous plaît", "excuse me": "Pardon"},
		WaterSafe: "Tap water is safe.",
		Cards:     "Cards widely accepted; small cafés may require €10+ minimum.",
	},
	"IT": {
		PlugTypes: []string{"C", "F", "L"}, Voltage: "230V", Frequency: "50Hz",
		Tipping:   "Not expected; round up the bill. A 'coperto' is often added.",
		Emergency: map[string]string{"all": "112"},
		Phrases:   map[string]string{"hello": "Ciao", "thank you": "Grazie", "please": "Per favore"},
		WaterSafe: "Tap water is safe; 'acqua non potabile' means not drinkable.",
		Cards:     "Cards accepted; small shops and taxis sometimes cash-only.",
	},
	"ES": {
		PlugTypes: []string{"C", "F"}, Voltage: "230V", Frequency: "50Hz",
		Tipping:   "Rounding up is enough; 5–10% for exceptional service.",
		Emergency: map[string]string{"all": "112"},
		Phrases:   map[string]string{"hello": "Hola", "thank you": "Gracias", "please": "Por favor"},
		WaterSafe: "Tap water is safe almost everywhere.",
		Cards:     "Cards accepted; keep €20 in small bills for tapas bars.",
	},
	"PT": {
		PlugTypes: []string{"C", "F"}, Voltage: "230V", Frequency: "50Hz",
		Tipping:   "Not expected; round up or leave 5–10% for great service.",
		Emergency: map[string]string{"all": "112"},
		Phrases:   map[string]string{"hello": "Olá", "thank you": "Obrigado/Obrigada", "please": "Por favor"},
		WaterSafe: "Tap water is safe.",
		Cards:     "Cards widely accepted via the Multibanco network.",
	},
	"DE": {
		PlugTypes: []string{"C", "F"}, Voltage: "230V", Frequency: "50Hz",
		Tipping:   "Round up or add 5–10%; say the total including tip as you pay.",
		Emergency: map[string]string{"all": "112", "police": "110"},
		Phrases:   map[string]string{"hello": "Hallo", "thank you": "Danke", "please": "Bitte"},
		WaterSafe: "Tap water is safe.",
		Cards:     "Many places still cash-preferred; carry €50+.",
	},
	"NL": {
		PlugTypes: []string{"C", "F"}, Voltage: "230V", Frequency: "50Hz",
		Tipping:   "Not expected; round up the bill.",
		Emergency: map[string]string{"all": "112"},
		Phrases:   map[string]string{"hello": "Hallo", "thank you": "Dank je", "please": "Alsjeblieft"},
		WaterSafe: "Tap water is safe.",
		Cards:     "Maestro/PIN strongly preferred; some places don't take credit cards.",
	},
	"JP": {
		PlugTypes: []string{"A", "B"}, Voltage: "100V", Frequency: "50/60Hz",
		Tipping:   "Not customary; often refused. Excellent service is the default.",
		Emergency: map[string]string{"police": "110", "fire/medical": "119"},
		Phrases:   map[string]string{"hello": "Konnichiwa", "thank you": "Arigatou gozaimasu", "excuse me": "Sumimasen"},
		WaterSafe: "Tap water is safe.",
		Cards:     "Cash still widely used; keep ¥10,000 on hand.",
	},
	"CN": {
		PlugTypes: []string{"A", "C", "I"}, Voltage: "220V", Frequency: "50Hz",
		Tipping:   "Not expected; often refused.",
		Emergency: map[string]string{"police": "110", "fire": "119", "medical": "120"},
		Phrases:   map[string]string{"hello": "Nǐ hǎo", "thank you": "Xièxiè"},
		WaterSafe: "Bottled water recommended.",
		Cards:     "WeChat Pay and Alipay dominate; Western cards limited.",
	},
	"TH": {
		PlugTypes: []string{"A", "B", "C"}, Voltage: "220V", Frequency: "50Hz",
		Tipping:   "Not required; 10% at nicer restaurants is generous.",
		Emergency: map[string]string{"tourist police": "1155", "police": "191", "medical": "1669"},
		Phrases:   map[string]string{"hello": "Sawatdi khrap/kha", "thank you": "Khop khun"},
		WaterSafe: "Bottled water recommended.",
		Cards:     "Cards accepted in cities; street vendors cash-only.",
	},
	"AU": {
		PlugTypes: []string{"I"}, Voltage: "230V", Frequency: "50Hz",
		Tipping:   "Not expected; 10% for exceptional service.",
		Emergency: map[string]string{"all": "000"},
		Phrases:   map[string]string{"hello": "Hello / G'day", "thank you": "Thanks"},
		WaterSafe: "Tap water is safe.",
		Cards:     "Tap-and-pay everywhere; $1.50 surcharge sometimes.",
	},
	"NZ": {
		PlugTypes: []string{"I"}, Voltage: "230V", Frequency: "50Hz",
		Tipping:   "Not expected.",
		Emergency: map[string]string{"all": "111"},
		Phrases:   map[string]string{"hello": "Kia ora", "thank you": "Thanks"},
		WaterSafe: "Tap water is safe.",
		Cards:     "Cards accepted universally.",
	},
	"MX": {
		PlugTypes: []string{"A", "B"}, Voltage: "127V", Frequency: "60Hz",
		Tipping:   "10–15% at restaurants; $20 MXN per bag for porters.",
		Emergency: map[string]string{"all": "911"},
		Phrases:   map[string]string{"hello": "Hola", "thank you": "Gracias", "please": "Por favor"},
		WaterSafe: "Bottled water recommended.",
		Cards:     "Cash widely preferred; cards accepted in tourist areas.",
	},
	"CA": {
		PlugTypes: []string{"A", "B"}, Voltage: "120V", Frequency: "60Hz",
		Tipping:   "Expected: 15–20% at restaurants; round up taxis.",
		Emergency: map[string]string{"all": "911"},
		Phrases:   map[string]string{"hello": "Hello / Bonjour", "thank you": "Thank you / Merci"},
		WaterSafe: "Tap water is safe.",
		Cards:     "Tap-to-pay everywhere.",
	},
	"BR": {
		PlugTypes: []string{"C", "N"}, Voltage: "127/220V", Frequency: "60Hz",
		Tipping:   "10% often included; otherwise add one.",
		Emergency: map[string]string{"police": "190", "medical": "192", "fire": "193"},
		Phrases:   map[string]string{"hello": "Olá", "thank you": "Obrigado/Obrigada"},
		WaterSafe: "Bottled water recommended.",
		Cards:     "Pix is dominant; cards widely accepted.",
	},
	"IN": {
		PlugTypes: []string{"C", "D", "M"}, Voltage: "230V", Frequency: "50Hz",
		Tipping:   "10% at restaurants; ₹50–100 for hotel porters.",
		Emergency: map[string]string{"all": "112"},
		Phrases:   map[string]string{"hello": "Namaste", "thank you": "Dhanyavaad"},
		WaterSafe: "Bottled/filtered water recommended.",
		Cards:     "UPI everywhere; foreign cards accepted at mid-tier and up.",
	},
	"AE": {
		PlugTypes: []string{"G"}, Voltage: "220V", Frequency: "50Hz",
		Tipping:   "10% often added; otherwise AED 10–20 per bill.",
		Emergency: map[string]string{"police": "999", "ambulance": "998", "fire": "997"},
		Phrases:   map[string]string{"hello": "Marhaba", "thank you": "Shukran"},
		WaterSafe: "Tap water is technically safe; bottled is the norm.",
		Cards:     "Cards accepted everywhere.",
	},
	"ZA": {
		PlugTypes: []string{"M", "N"}, Voltage: "230V", Frequency: "50Hz",
		Tipping:   "10–15% at restaurants; R10 per bag for porters.",
		Emergency: map[string]string{"all": "112", "police": "10111", "medical": "10177"},
		Phrases:   map[string]string{"hello": "Hello / Sawubona", "thank you": "Thank you / Ngiyabonga"},
		WaterSafe: "Tap water is safe in major cities.",
		Cards:     "Cards accepted; keep cash for rural areas.",
	},
}

func applyStaticExtras(out *CountryBasics, x staticExtras) {
	out.PlugTypes = x.PlugTypes
	out.Voltage = x.Voltage
	out.Frequency = x.Frequency
	out.Tipping = x.Tipping
	out.Emergency = x.Emergency
	out.KeyPhrases = x.Phrases
	out.WaterSafe = x.WaterSafe
	out.CardsAccepted = x.Cards
}
