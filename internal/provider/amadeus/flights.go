package amadeus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/realestayer/v4/internal/models"
)

// Flight API response types

type flightOffersResponse struct {
	Data []flightOfferData `json:"data"`
}

type flightOfferData struct {
	ID                     string             `json:"id"`
	Itineraries            []itineraryData    `json:"itineraries"`
	Price                  priceData          `json:"price"`
	ValidatingAirlineCodes []string           `json:"validatingAirlineCodes"`
	NumberOfBookableSeats  int                `json:"numberOfBookableSeats"`
	InstantTicketingRequired bool             `json:"instantTicketingRequired"`
	LastTicketingDate      string             `json:"lastTicketingDate"`
}

type itineraryData struct {
	Duration string        `json:"duration"`
	Segments []segmentData `json:"segments"`
}

type segmentData struct {
	Departure       endpointData `json:"departure"`
	Arrival         endpointData `json:"arrival"`
	CarrierCode     string       `json:"carrierCode"`
	Number          string       `json:"number"`
	Aircraft        aircraft     `json:"aircraft"`
	Duration        string       `json:"duration"`
	OperatingCarrier *struct {
		CarrierCode string `json:"carrierCode"`
	} `json:"operating,omitempty"`
}

type endpointData struct {
	IataCode string `json:"iataCode"`
	Terminal string `json:"terminal,omitempty"`
	At       string `json:"at"`
}

type aircraft struct {
	Code string `json:"code"`
}

type priceData struct {
	Currency   string `json:"currency"`
	Total      string `json:"total"`
	Base       string `json:"base"`
	GrandTotal string `json:"grandTotal"`
}

// SearchFlights searches for available flights
func (c *Client) SearchFlights(ctx context.Context, req models.FlightSearchRequest) ([]models.FlightOffer, error) {
	params := url.Values{}
	params.Set("originLocationCode", req.Origin)
	params.Set("destinationLocationCode", req.Destination)
	params.Set("departureDate", req.DepartureDate.Format("2006-01-02"))
	params.Set("adults", strconv.Itoa(req.Adults))

	if req.ReturnDate != nil {
		params.Set("returnDate", req.ReturnDate.Format("2006-01-02"))
	}
	if req.Children > 0 {
		params.Set("children", strconv.Itoa(req.Children))
	}
	if req.Infants > 0 {
		params.Set("infants", strconv.Itoa(req.Infants))
	}
	if req.CabinClass != "" {
		params.Set("travelClass", string(req.CabinClass))
	}
	if req.Currency != "" {
		params.Set("currencyCode", req.Currency)
	}
	if req.MaxPrice != nil {
		params.Set("maxPrice", strconv.FormatFloat(*req.MaxPrice, 'f', 2, 64))
	}
	if req.DirectOnly {
		params.Set("nonStop", "true")
	}
	params.Set("max", "50") // limit results

	resp, err := c.get(ctx, "/v2/shopping/flight-offers?"+params.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var result flightOffersResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return c.convertFlightOffers(result.Data), nil
}

// GetFlightOffer retrieves a specific flight offer from the in-memory search
// cache. Amadeus does not expose a get-by-id endpoint — the offer must have
// been seen by a recent SearchFlights call. Returns an error if the entry is
// missing or has expired.
func (c *Client) GetFlightOffer(ctx context.Context, offerID string) (*models.FlightOffer, error) {
	raw, ok := c.lookupOffer(offerID)
	if !ok {
		return nil, fmt.Errorf("flight offer not found or expired: %s", offerID)
	}
	offers := c.convertFlightOffers([]flightOfferData{raw})
	if len(offers) == 0 {
		return nil, fmt.Errorf("flight offer decode failed: %s", offerID)
	}
	return &offers[0], nil
}

// PriceFlightOffer confirms current pricing for an offer by calling Amadeus's
// pricing endpoint with the cached raw offer payload. If the cache entry is
// missing we can't price it — the client has to redo the search.
func (c *Client) PriceFlightOffer(ctx context.Context, offerID string) (*models.FlightOffer, error) {
	raw, ok := c.lookupOffer(offerID)
	if !ok {
		return nil, fmt.Errorf("flight offer not found or expired: %s", offerID)
	}

	body := map[string]interface{}{
		"data": map[string]interface{}{
			"type":         "flight-offers-pricing",
			"flightOffers": []flightOfferData{raw},
		},
	}
	resp, err := c.post(ctx, "/v1/shopping/flight-offers/pricing", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, parseError(resp)
	}

	var priced struct {
		Data struct {
			FlightOffers []flightOfferData `json:"flightOffers"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&priced); err != nil {
		return nil, fmt.Errorf("failed to decode pricing response: %w", err)
	}
	if len(priced.Data.FlightOffers) == 0 {
		return nil, fmt.Errorf("pricing returned no offers for %s", offerID)
	}
	// Refresh the cache with the priced version so the booking step sees
	// the same data Amadeus just confirmed.
	c.rememberOffer(priced.Data.FlightOffers[0])
	offers := c.convertFlightOffers(priced.Data.FlightOffers[:1])
	return &offers[0], nil
}

// BookFlight creates a flight booking
func (c *Client) BookFlight(ctx context.Context, req models.FlightBookingRequest) (*models.BookingConfirmation, error) {
	// Build the flight order request
	orderReq := buildFlightOrderRequest(req)

	resp, err := c.post(ctx, "/v1/booking/flight-orders", orderReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var result struct {
		Data struct {
			ID           string `json:"id"`
			AssociatedRecords []struct {
				Reference string `json:"reference"`
			} `json:"associatedRecords"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode booking response: %w", err)
	}

	reference := result.Data.ID
	if len(result.Data.AssociatedRecords) > 0 {
		reference = result.Data.AssociatedRecords[0].Reference
	}

	return &models.BookingConfirmation{
		Provider:    "amadeus",
		Reference:   reference,
		Status:      "confirmed",
		ConfirmedAt: time.Now(),
	}, nil
}

// GetFlightStatus retrieves booking status
func (c *Client) GetFlightStatus(ctx context.Context, bookingRef string) (*models.BookingStatus, error) {
	resp, err := c.get(ctx, "/v1/booking/flight-orders/"+bookingRef)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("booking not found: %s", bookingRef)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	status := models.BookingStatusConfirmed
	return &status, nil
}

// SearchAirports searches for airports by keyword
func (c *Client) SearchAirports(ctx context.Context, keyword string) ([]models.Airport, error) {
	params := url.Values{}
	params.Set("subType", "AIRPORT")
	params.Set("keyword", keyword)
	params.Set("page[limit]", "10")

	resp, err := c.get(ctx, "/v1/reference-data/locations?"+params.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var result struct {
		Data []struct {
			IataCode string `json:"iataCode"`
			Name     string `json:"name"`
			Address  struct {
				CityName    string `json:"cityName"`
				CountryName string `json:"countryName"`
			} `json:"address"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	airports := make([]models.Airport, len(result.Data))
	for i, a := range result.Data {
		airports[i] = models.Airport{
			Code:    a.IataCode,
			Name:    a.Name,
			City:    a.Address.CityName,
			Country: a.Address.CountryName,
		}
	}

	return airports, nil
}

// convertFlightOffers converts Amadeus response to our model and remembers
// each offer in the client cache so GetFlightOffer / PriceFlightOffer can
// find it later.
func (c *Client) convertFlightOffers(data []flightOfferData) []models.FlightOffer {
	offers := make([]models.FlightOffer, len(data))

	for i, d := range data {
		c.rememberOffer(d)

		offers[i] = models.FlightOffer{
			ID:               d.ID,
			Provider:         "amadeus",
			Itineraries:      convertItineraries(d.Itineraries),
			Price:            convertPrice(d.Price),
			SeatsRemaining:   d.NumberOfBookableSeats,
			InstantTicketing: d.InstantTicketingRequired,
		}

		if d.LastTicketingDate != "" {
			if t, err := time.Parse("2006-01-02", d.LastTicketingDate); err == nil {
				offers[i].ValidUntil = t
			}
		}
	}

	return offers
}

func convertItineraries(data []itineraryData) []models.Itinerary {
	itineraries := make([]models.Itinerary, len(data))

	for i, d := range data {
		itineraries[i] = models.Itinerary{
			Duration: d.Duration,
			Segments: convertSegments(d.Segments),
		}
	}

	return itineraries
}

func convertSegments(data []segmentData) []models.FlightSegment {
	segments := make([]models.FlightSegment, len(data))

	for i, d := range data {
		depTime, _ := time.Parse(time.RFC3339, d.Departure.At)
		arrTime, _ := time.Parse(time.RFC3339, d.Arrival.At)

		segments[i] = models.FlightSegment{
			Departure: models.FlightEndpoint{
				Airport:  d.Departure.IataCode,
				Terminal: d.Departure.Terminal,
				DateTime: depTime,
			},
			Arrival: models.FlightEndpoint{
				Airport:  d.Arrival.IataCode,
				Terminal: d.Arrival.Terminal,
				DateTime: arrTime,
			},
			Carrier: models.Carrier{
				Code: d.CarrierCode,
				Name: getAirlineName(d.CarrierCode),
			},
			FlightNumber: d.CarrierCode + d.Number,
			Duration:     d.Duration,
		}
	}

	return segments
}

func convertPrice(data priceData) models.Price {
	total, _ := strconv.ParseFloat(data.GrandTotal, 64)
	base, _ := strconv.ParseFloat(data.Base, 64)

	return models.Price{
		Base:     base,
		Taxes:    total - base,
		Total:    total,
		Currency: data.Currency,
	}
}

func buildFlightOrderRequest(req models.FlightBookingRequest) map[string]interface{} {
	travelers := make([]map[string]interface{}, len(req.Passengers))
	for i, p := range req.Passengers {
		travelers[i] = map[string]interface{}{
			"id":          strconv.Itoa(i + 1),
			"dateOfBirth": p.DateOfBirth.Format("2006-01-02"),
			"name": map[string]string{
				"firstName": p.FirstName,
				"lastName":  p.LastName,
			},
			"gender": p.Gender,
			"contact": map[string]interface{}{
				"emailAddress": p.Email,
				"phones": []map[string]string{
					{
						"deviceType":         "MOBILE",
						"countryCallingCode": req.Contact.CountryCode,
						"number":             p.Phone,
					},
				},
			},
		}

		if p.PassportNumber != "" {
			travelers[i]["documents"] = []map[string]interface{}{
				{
					"documentType":     "PASSPORT",
					"number":           p.PassportNumber,
					"expiryDate":       p.PassportExpiry.Format("2006-01-02"),
					"issuanceCountry":  p.PassportCountry,
					"nationality":      p.PassportCountry,
					"holder":           true,
				},
			}
		}
	}

	return map[string]interface{}{
		"data": map[string]interface{}{
			"type":       "flight-order",
			"travelers":  travelers,
			"remarks": map[string]interface{}{
				"general": []map[string]string{
					{
						"subType": "GENERAL_MISCELLANEOUS",
						"text":    "ONLINE BOOKING VIA REAL-ESTAYER",
					},
				},
			},
			"ticketingAgreement": map[string]string{
				"option": "DELAY_TO_CANCEL",
				"delay":  "6D",
			},
		},
	}
}

// Airline code to name mapping
var airlineNames = map[string]string{
	"AA": "American Airlines",
	"UA": "United Airlines",
	"DL": "Delta Air Lines",
	"WN": "Southwest Airlines",
	"AS": "Alaska Airlines",
	"B6": "JetBlue Airways",
	"NK": "Spirit Airlines",
	"F9": "Frontier Airlines",
	"LH": "Lufthansa",
	"BA": "British Airways",
	"AF": "Air France",
	"KL": "KLM Royal Dutch Airlines",
	"IB": "Iberia",
	"EK": "Emirates",
	"QR": "Qatar Airways",
	"SQ": "Singapore Airlines",
	"CX": "Cathay Pacific",
	"AC": "Air Canada",
	"QF": "Qantas",
}

func getAirlineName(code string) string {
	if name, ok := airlineNames[code]; ok {
		return name
	}
	return code
}
