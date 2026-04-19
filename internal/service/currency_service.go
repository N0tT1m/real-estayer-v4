package service

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CurrencyService pulls the ECB daily reference rates (EUR-base) and exposes
// a simple `Convert(amount, from, to)` helper. Rates are cached for 12h —
// ECB updates once per business day.
//
// The ECB feed is free, keyless, and has a stable XML schema dating back to
// ~2006. No API key means no secret to manage.
type CurrencyService struct {
	client *http.Client
	mu     sync.RWMutex
	rates  map[string]float64 // ratesPerEUR[code]
	expires time.Time
}

func NewCurrencyService() *CurrencyService {
	return &CurrencyService{
		client: &http.Client{Timeout: 10 * time.Second},
		rates:  map[string]float64{"EUR": 1},
	}
}

// ensureRates lazily fetches ECB rates on first use and every 12h thereafter.
// If the network fails we keep serving the stale cache — better than nothing.
func (s *CurrencyService) ensureRates(ctx context.Context) error {
	s.mu.RLock()
	if time.Now().Before(s.expires) && len(s.rates) > 1 {
		s.mu.RUnlock()
		return nil
	}
	s.mu.RUnlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://www.ecb.europa.eu/stats/eurofxref/eurofxref-daily.xml", nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("ecb: status %d", resp.StatusCode)
	}

	// ECB's daily XML nests three <Cube> levels inside <Envelope>:
	//   outer <Cube> → time-stamped <Cube time="…"> → per-currency <Cube currency="…" rate="…"/>
	var doc struct {
		XMLName xml.Name `xml:"Envelope"`
		Cube    struct {
			Days []struct {
				Rates []struct {
					Currency string `xml:"currency,attr"`
					Rate     string `xml:"rate,attr"`
				} `xml:"Cube"`
			} `xml:"Cube"`
		} `xml:"Cube"`
	}
	if err := xml.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return err
	}
	if len(doc.Cube.Days) == 0 {
		return fmt.Errorf("ecb: empty feed")
	}
	fresh := map[string]float64{"EUR": 1}
	for _, r := range doc.Cube.Days[0].Rates {
		val, err := strconv.ParseFloat(r.Rate, 64)
		if err != nil || val == 0 {
			continue
		}
		fresh[strings.ToUpper(r.Currency)] = val
	}

	s.mu.Lock()
	s.rates = fresh
	s.expires = time.Now().Add(12 * time.Hour)
	s.mu.Unlock()
	return nil
}

// Convert returns amount in `from` currency converted into `to`. Unknown
// currencies return an error. Rates are ECB reference rates — not for
// trading; fine for trip budgeting.
func (s *CurrencyService) Convert(ctx context.Context, amount float64, from, to string) (float64, error) {
	from = strings.ToUpper(strings.TrimSpace(from))
	to = strings.ToUpper(strings.TrimSpace(to))
	if from == to || from == "" || to == "" {
		return amount, nil
	}
	if err := s.ensureRates(ctx); err != nil && len(s.rates) <= 1 {
		return 0, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	fromRate, ok := s.rates[from]
	if !ok {
		return 0, fmt.Errorf("unknown currency %q", from)
	}
	toRate, ok := s.rates[to]
	if !ok {
		return 0, fmt.Errorf("unknown currency %q", to)
	}
	// ECB rates are per-1-EUR, so convert through EUR.
	eur := amount / fromRate
	return eur * toRate, nil
}

// SupportedCurrencies returns the set of codes the cached feed currently
// knows about, sorted. Useful for populating a dropdown.
func (s *CurrencyService) SupportedCurrencies(ctx context.Context) []string {
	_ = s.ensureRates(ctx)
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.rates))
	for k := range s.rates {
		out = append(out, k)
	}
	// Lightweight sort so callers see stable order.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}
