package service

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// CarAffiliateService returns deep-links into partner car-rental sites with
// the user's search parameters pre-populated. It's the v1 stand-in for a
// real car-rental API (CarTrawler, Priceline, etc.) — we can't book cars
// without a contracted partner, but we can at least hand the user off to a
// pre-filled comparison page and earn affiliate revenue on the click.
//
// Affiliate IDs come from env vars; when one isn't set we still emit the
// deep-link but without the tracking parameter, which degrades to organic
// traffic rather than failing the page.
type CarAffiliateService struct {
	// RentalcarsID is the Booking.com-owned Rentalcars.com affiliate ID.
	// Passed as `aid=`.
	RentalcarsID string
	// PricelineID is the Priceline Partner Network click ID.
	PricelineID string
	// KayakID is the Kayak affiliate "enc_cid" identifier.
	KayakID string
}

func NewCarAffiliateService(rentalcars, priceline, kayak string) *CarAffiliateService {
	return &CarAffiliateService{
		RentalcarsID: rentalcars,
		PricelineID:  priceline,
		KayakID:      kayak,
	}
}

// CarDeepLink is one partner card rendered on /cars when no booking API is
// live. Title + subtitle + URL + tracking metadata only — we don't fetch
// prices so everything on the card is static.
type CarDeepLink struct {
	Partner     string `json:"partner"`
	Label       string `json:"label"`
	Subtitle    string `json:"subtitle"`
	URL         string `json:"url"`
	AffiliateID string `json:"affiliate_id,omitempty"`
}

// CarSearchInput is the subset of models.CarSearchRequest that actually
// influences a deep-link. Duplicated rather than imported so this package
// stays free of circular references with /internal/models.
type CarSearchInput struct {
	PickupLocation  string
	DropoffLocation string // may be empty — we fall back to pickup
	PickupAt        time.Time
	DropoffAt       time.Time
}

// Generate returns partner deep-links in display order. Always safe to
// call — missing affiliate IDs just produce plain URLs.
func (s *CarAffiliateService) Generate(ctx context.Context, in CarSearchInput) []CarDeepLink {
	if in.DropoffLocation == "" {
		in.DropoffLocation = in.PickupLocation
	}
	out := []CarDeepLink{}

	// --- Rentalcars.com (Booking-owned, accepts IATA + ISO dates) ---
	rq := url.Values{}
	rq.Set("puCity", in.PickupLocation)
	rq.Set("doCity", in.DropoffLocation)
	if !in.PickupAt.IsZero() {
		rq.Set("puDate", in.PickupAt.Format("2006-01-02"))
		rq.Set("puTime", in.PickupAt.Format("15:04"))
	}
	if !in.DropoffAt.IsZero() {
		rq.Set("doDate", in.DropoffAt.Format("2006-01-02"))
		rq.Set("doTime", in.DropoffAt.Format("15:04"))
	}
	if s.RentalcarsID != "" {
		rq.Set("aid", s.RentalcarsID)
	}
	out = append(out, CarDeepLink{
		Partner:     "Rentalcars.com",
		Label:       "Compare on Rentalcars.com",
		Subtitle:    "900+ suppliers · free cancellation on most rates",
		URL:         "https://www.rentalcars.com/SearchResults.do?" + rq.Encode(),
		AffiliateID: s.RentalcarsID,
	})

	// --- Priceline (accepts airport codes + MM/DD/YY dates) ---
	pq := url.Values{}
	pq.Set("pickup-airport-id", in.PickupLocation)
	pq.Set("dropoff-airport-id", in.DropoffLocation)
	if !in.PickupAt.IsZero() {
		pq.Set("pickup-date", in.PickupAt.Format("01/02/06"))
		pq.Set("pickup-time", in.PickupAt.Format("15:04"))
	}
	if !in.DropoffAt.IsZero() {
		pq.Set("dropoff-date", in.DropoffAt.Format("01/02/06"))
		pq.Set("dropoff-time", in.DropoffAt.Format("15:04"))
	}
	if s.PricelineID != "" {
		pq.Set("refid", s.PricelineID)
	}
	out = append(out, CarDeepLink{
		Partner:     "Priceline",
		Label:       "Compare on Priceline",
		Subtitle:    "Express Deals on major brands (Hertz, Avis, Enterprise)",
		URL:         "https://www.priceline.com/drive/search?" + pq.Encode(),
		AffiliateID: s.PricelineID,
	})

	// --- Kayak (metasearch — great compare-price button) ---
	// Kayak uses path segments + ISO dates: /cars/LAX/JFK/2026-06-01-10:00/...
	kpath := fmt.Sprintf("https://www.kayak.com/cars/%s/%s",
		url.PathEscape(in.PickupLocation), url.PathEscape(in.DropoffLocation))
	if !in.PickupAt.IsZero() && !in.DropoffAt.IsZero() {
		kpath += fmt.Sprintf("/%s/%s",
			in.PickupAt.Format("2006-01-02-15:04"),
			in.DropoffAt.Format("2006-01-02-15:04"))
	}
	kq := url.Values{}
	if s.KayakID != "" {
		kq.Set("enc_cid", s.KayakID)
	}
	if enc := kq.Encode(); enc != "" {
		kpath += "?" + enc
	}
	out = append(out, CarDeepLink{
		Partner:     "Kayak",
		Label:       "Compare on Kayak",
		Subtitle:    "Metasearch across 500+ suppliers",
		URL:         kpath,
		AffiliateID: s.KayakID,
	})

	return out
}
