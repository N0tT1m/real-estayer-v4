// Package duffel is a thin client over Duffel's Air API. Duffel replaced
// Amadeus Self-Service for our flight path when Amadeus was decommissioned:
// the API is JSON-only, has real data in the test environment, and its
// search endpoint is shaped almost identically to what our internal
// FlightProvider contract expects.
//
// Scope of this client:
//   - SearchFlights (via /air/offer_requests?return_offers=true)
//   - GetFlightOffer (live lookup by offer ID)
//   - PriceFlightOffer (re-fetches offer — Duffel prices are live-at-lookup)
//   - BookFlight (POST /air/orders, test-mode payment by balance)
//   - GetFlightStatus (GET /air/orders/<id>)
//   - SearchAirports (GET /places/suggestions, filtered to airports)
//
// Not implemented yet: seat selection, ancillaries, order cancellation,
// refund flow. Those are follow-on work once the search path is in use.
//
// Auth is a single bearer token exported as Client.accessToken. Duffel
// issues long-lived access tokens from the dashboard — no OAuth dance.
package duffel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/realestayer/v4/internal/models"
)

const (
	defaultBaseURL = "https://api.duffel.com"
	apiVersion     = "v2" // pinned — breaking changes are gated on version header
)

// Client talks to the Duffel Air API.
type Client struct {
	accessToken string
	baseURL     string
	httpClient  *http.Client
}

// NewClient returns a configured client. An empty accessToken produces a
// client that returns ErrNotConfigured for every call — makes it safe to
// register even before the key is provisioned.
func NewClient(accessToken, baseURL string) *Client {
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	return &Client{
		accessToken: accessToken,
		baseURL:     baseURL,
		httpClient:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Name implements provider.FlightProvider.
func (c *Client) Name() string { return "duffel" }

// ErrNotConfigured is returned when the client has no access token.
var ErrNotConfigured = fmt.Errorf("duffel: access token not configured")

// --- Shared request plumbing ---

func (c *Client) do(ctx context.Context, method, path string, body interface{}) (*http.Response, error) {
	if c.accessToken == "" {
		return nil, ErrNotConfigured
	}
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("duffel: marshal body: %w", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.accessToken)
	req.Header.Set("Duffel-Version", apiVersion)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return c.httpClient.Do(req)
}

// apiError is the standard Duffel error envelope.
type apiError struct {
	Errors []struct {
		Title   string `json:"title"`
		Message string `json:"message"`
		Code    string `json:"code"`
	} `json:"errors"`
}

func parseError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	var e apiError
	if jerr := json.Unmarshal(body, &e); jerr == nil && len(e.Errors) > 0 {
		first := e.Errors[0]
		return fmt.Errorf("duffel %d: %s — %s (%s)", resp.StatusCode, first.Title, first.Message, first.Code)
	}
	return fmt.Errorf("duffel %d: %s", resp.StatusCode, string(body))
}

// --- Wire types ---
// Duffel wraps every body in {"data": …}. These types capture just the
// fields we render — the wire format has plenty more we don't use.

type offerRequestCreate struct {
	Data offerRequestInput `json:"data"`
}

type offerRequestInput struct {
	Slices     []sliceInput     `json:"slices"`
	Passengers []passengerInput `json:"passengers"`
	CabinClass string           `json:"cabin_class,omitempty"`
}

type sliceInput struct {
	Origin        string `json:"origin"`
	Destination   string `json:"destination"`
	DepartureDate string `json:"departure_date"`
}

type passengerInput struct {
	Type string `json:"type"` // adult, child, infant_without_seat
	Age  *int   `json:"age,omitempty"`
}

type offerRequestResponse struct {
	Data struct {
		ID     string      `json:"id"`
		Offers []wireOffer `json:"offers"`
	} `json:"data"`
}

type wireOffer struct {
	ID            string      `json:"id"`
	TotalAmount   string      `json:"total_amount"`
	TotalCurrency string      `json:"total_currency"`
	BaseAmount    string      `json:"base_amount"`
	TaxAmount     string      `json:"tax_amount"`
	ExpiresAt     time.Time   `json:"expires_at"`
	Slices        []wireSlice `json:"slices"`
}

type wireSlice struct {
	Duration string        `json:"duration"`
	Segments []wireSegment `json:"segments"`
}

type wireSegment struct {
	ID                           string    `json:"id"`
	DepartingAt                  time.Time `json:"departing_at"`
	ArrivingAt                   time.Time `json:"arriving_at"`
	Duration                     string    `json:"duration"`
	Origin                       wirePlace `json:"origin"`
	Destination                  wirePlace `json:"destination"`
	MarketingCarrier             carrier   `json:"marketing_carrier"`
	OperatingCarrier             *carrier  `json:"operating_carrier,omitempty"`
	Aircraft                     *aircraft `json:"aircraft,omitempty"`
	MarketingCarrierFlightNumber string    `json:"marketing_carrier_flight_number"`
}

type wirePlace struct {
	IATACode string `json:"iata_code"`
	Name     string `json:"name"`
	CityName string `json:"city_name"`
	Terminal string `json:"terminal"`
}

type carrier struct {
	Name     string `json:"name"`
	IATACode string `json:"iata_code"`
}

type aircraft struct {
	Name     string `json:"name"`
	IATACode string `json:"iata_code"`
}

// --- API methods ---

// SearchFlights creates an offer request with return_offers=true so the
// offers come back inline on the first round-trip.
func (c *Client) SearchFlights(ctx context.Context, req models.FlightSearchRequest) ([]models.FlightOffer, error) {
	input := offerRequestInput{
		Slices: []sliceInput{{
			Origin:        req.Origin,
			Destination:   req.Destination,
			DepartureDate: req.DepartureDate.Format("2006-01-02"),
		}},
		Passengers: passengerList(req),
		CabinClass: duffelCabinClass(req.CabinClass),
	}
	if req.ReturnDate != nil {
		input.Slices = append(input.Slices, sliceInput{
			Origin:        req.Destination,
			Destination:   req.Origin,
			DepartureDate: req.ReturnDate.Format("2006-01-02"),
		})
	}

	resp, err := c.do(ctx, http.MethodPost, "/air/offer_requests?return_offers=true", offerRequestCreate{Data: input})
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var parsed offerRequestResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("duffel: decode offer response: %w", err)
	}
	return convertOffers(parsed.Data.Offers, req), nil
}

// GetFlightOffer fetches a single offer by ID — used during booking flow
// to re-confirm that the offer still exists + see the current price.
func (c *Client) GetFlightOffer(ctx context.Context, offerID string) (*models.FlightOffer, error) {
	resp, err := c.do(ctx, http.MethodGet, "/air/offers/"+url.PathEscape(offerID), nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("duffel: offer not found or expired: %s", offerID)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var body struct {
		Data wireOffer `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	offers := convertOffers([]wireOffer{body.Data}, models.FlightSearchRequest{})
	if len(offers) == 0 {
		return nil, fmt.Errorf("duffel: empty offer payload for %s", offerID)
	}
	return &offers[0], nil
}

// PriceFlightOffer is implemented as a fresh GetFlightOffer: Duffel returns
// live pricing on every read, so there's no separate pricing endpoint.
func (c *Client) PriceFlightOffer(ctx context.Context, offerID string) (*models.FlightOffer, error) {
	return c.GetFlightOffer(ctx, offerID)
}

// --- Booking ---

type orderCreate struct {
	Data orderInput `json:"data"`
}

type orderInput struct {
	Type           string         `json:"type"` // "instant"
	SelectedOffers []string       `json:"selected_offers"`
	Passengers     []passengerOut `json:"passengers"`
	Payments       []paymentInput `json:"payments"`
}

type passengerOut struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	GivenName   string `json:"given_name"`
	FamilyName  string `json:"family_name"`
	BornOn      string `json:"born_on"` // YYYY-MM-DD
	Gender      string `json:"gender"`  // "m" / "f"
	Email       string `json:"email"`
	PhoneNumber string `json:"phone_number"`
}

type paymentInput struct {
	Type     string `json:"type"` // "balance" in test, "arc_bsp_cash" in prod
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

type orderResponse struct {
	Data struct {
		ID               string `json:"id"`
		BookingReference string `json:"booking_reference"`
		TotalAmount      string `json:"total_amount"`
		TotalCurrency    string `json:"total_currency"`
	} `json:"data"`
}

// BookFlight creates an order against a previously-returned offer. In test
// mode payment uses Duffel's sandbox balance; in prod you'd swap the
// payment type to `arc_bsp_cash` or a card token.
func (c *Client) BookFlight(ctx context.Context, req models.FlightBookingRequest) (*models.BookingConfirmation, error) {
	// Duffel needs the offer's total amount on the payment line; we fetch
	// it here so the caller doesn't have to plumb it through.
	offer, err := c.GetFlightOffer(ctx, req.OfferID)
	if err != nil {
		return nil, fmt.Errorf("duffel: re-fetch offer before booking: %w", err)
	}

	passengers := make([]passengerOut, len(req.Passengers))
	for i, p := range req.Passengers {
		passengers[i] = passengerOut{
			ID:          fmt.Sprintf("pas_%d", i),
			Title:       "mr", // not surfaced in our model; defaulting
			GivenName:   p.FirstName,
			FamilyName:  p.LastName,
			BornOn:      p.DateOfBirth.Format("2006-01-02"),
			Gender:      normalizeGender(p.Gender),
			Email:       p.Email,
			PhoneNumber: p.Phone,
		}
	}

	amount := strconv.FormatFloat(offer.Price.Total, 'f', 2, 64)
	payload := orderCreate{Data: orderInput{
		Type:           "instant",
		SelectedOffers: []string{req.OfferID},
		Passengers:     passengers,
		Payments: []paymentInput{{
			Type:     "balance",
			Amount:   amount,
			Currency: offer.Price.Currency,
		}},
	}}

	resp, err := c.do(ctx, http.MethodPost, "/air/orders", payload)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var body orderResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	ref := body.Data.BookingReference
	if ref == "" {
		ref = body.Data.ID
	}
	return &models.BookingConfirmation{
		Provider:    "duffel",
		Reference:   ref,
		Status:      "confirmed",
		ConfirmedAt: time.Now(),
	}, nil
}

// GetFlightStatus returns a coarse status for an order. Duffel has richer
// lifecycle events (cancelled, on_hold, etc.); we collapse them to the
// three values the rest of the app understands.
func (c *Client) GetFlightStatus(ctx context.Context, bookingRef string) (*models.BookingStatus, error) {
	resp, err := c.do(ctx, http.MethodGet, "/air/orders/"+url.PathEscape(bookingRef), nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("duffel: booking not found: %s", bookingRef)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var body struct {
		Data struct {
			Cancelled bool `json:"cancelled_at,omitempty"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	status := models.BookingStatusConfirmed
	if body.Data.Cancelled {
		status = models.BookingStatusCancelled
	}
	return &status, nil
}

// --- Airport search ---

type placeSuggestion struct {
	Type            string `json:"type"` // "airport" | "city"
	IATACode        string `json:"iata_code"`
	Name            string `json:"name"`
	CityName        string `json:"city_name"`
	IATACountryCode string `json:"iata_country_code"`
}

// SearchAirports returns airport suggestions matching the keyword. Duffel
// returns both airports and city-level entries; we filter to airports
// because the rest of the app expects IATA-code-bearing results.
func (c *Client) SearchAirports(ctx context.Context, keyword string) ([]models.Airport, error) {
	q := url.Values{}
	q.Set("query", keyword)
	resp, err := c.do(ctx, http.MethodGet, "/places/suggestions?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}
	var body struct {
		Data []placeSuggestion `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]models.Airport, 0, len(body.Data))
	for _, s := range body.Data {
		if s.Type != "airport" || s.IATACode == "" {
			continue
		}
		out = append(out, models.Airport{
			Code:    s.IATACode,
			Name:    s.Name,
			City:    s.CityName,
			Country: s.IATACountryCode,
		})
	}
	return out, nil
}

// --- Helpers ---

func passengerList(req models.FlightSearchRequest) []passengerInput {
	n := req.Adults
	if n < 1 {
		n = 1
	}
	out := make([]passengerInput, 0, n+req.Children+req.Infants)
	for i := 0; i < n; i++ {
		out = append(out, passengerInput{Type: "adult"})
	}
	for i := 0; i < req.Children; i++ {
		// Duffel wants an age for children; default to 8 when not supplied.
		age := 8
		out = append(out, passengerInput{Type: "child", Age: &age})
	}
	for i := 0; i < req.Infants; i++ {
		out = append(out, passengerInput{Type: "infant_without_seat"})
	}
	return out
}

func duffelCabinClass(c models.CabinClass) string {
	switch strings.ToUpper(string(c)) {
	case "ECONOMY", "":
		return "economy"
	case "PREMIUM_ECONOMY":
		return "premium_economy"
	case "BUSINESS":
		return "business"
	case "FIRST":
		return "first"
	}
	return "economy"
}

func normalizeGender(g string) string {
	switch strings.ToLower(g) {
	case "male", "m":
		return "m"
	case "female", "f":
		return "f"
	}
	return "m" // Duffel requires one of m/f/x; default so the call doesn't 400
}

// convertOffers maps Duffel's wire format to our internal FlightOffer type.
// We preserve the Duffel offer ID verbatim so GetFlightOffer / BookFlight
// can round-trip against it later.
func convertOffers(offers []wireOffer, _ models.FlightSearchRequest) []models.FlightOffer {
	out := make([]models.FlightOffer, len(offers))
	for i, o := range offers {
		total, _ := strconv.ParseFloat(o.TotalAmount, 64)
		base, _ := strconv.ParseFloat(o.BaseAmount, 64)
		tax, _ := strconv.ParseFloat(o.TaxAmount, 64)

		itineraries := make([]models.Itinerary, len(o.Slices))
		for si, s := range o.Slices {
			segs := make([]models.FlightSegment, len(s.Segments))
			for j, seg := range s.Segments {
				segs[j] = models.FlightSegment{
					Departure: models.FlightEndpoint{
						Airport:  seg.Origin.IATACode,
						Terminal: seg.Origin.Terminal,
						DateTime: seg.DepartingAt,
					},
					Arrival: models.FlightEndpoint{
						Airport:  seg.Destination.IATACode,
						Terminal: seg.Destination.Terminal,
						DateTime: seg.ArrivingAt,
					},
					Carrier: models.Carrier{
						Code: seg.MarketingCarrier.IATACode,
						Name: seg.MarketingCarrier.Name,
					},
					FlightNumber: seg.MarketingCarrier.IATACode + seg.MarketingCarrierFlightNumber,
					Duration:     seg.Duration,
				}
			}
			itineraries[si] = models.Itinerary{Duration: s.Duration, Segments: segs}
		}

		out[i] = models.FlightOffer{
			ID:          o.ID,
			Provider:    "duffel",
			Itineraries: itineraries,
			Price: models.Price{
				Base:     base,
				Taxes:    tax,
				Total:    total,
				Currency: o.TotalCurrency,
			},
			ValidUntil: o.ExpiresAt,
		}
	}
	return out
}
