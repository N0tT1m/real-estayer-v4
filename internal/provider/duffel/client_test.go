package duffel

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/realestayer/v4/internal/models"
)

// newTestClient points a real Client at an httptest server, exercising the
// full request-build / response-decode path without touching Duffel.
func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return NewClient("test-token", srv.URL)
}

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("bad date %q: %v", s, err)
	}
	return d
}

func TestNewClientDefaultsBaseURL(t *testing.T) {
	if got := NewClient("tok", "").baseURL; got != defaultBaseURL {
		t.Errorf("baseURL = %q, want %q", got, defaultBaseURL)
	}
	if got := NewClient("tok", "https://example.test").baseURL; got != "https://example.test" {
		t.Errorf("explicit baseURL was overridden: %q", got)
	}
}

// An unconfigured client must fail fast rather than issuing an unauthenticated
// request — main.go registers it before the token may exist.
func TestUnconfiguredClientReturnsErrNotConfigured(t *testing.T) {
	c := NewClient("", "https://example.invalid")
	if _, err := c.SearchFlights(context.Background(), models.FlightSearchRequest{}); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("SearchFlights err = %v, want ErrNotConfigured", err)
	}
	if _, err := c.GetFlightOffer(context.Background(), "off_1"); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("GetFlightOffer err = %v, want ErrNotConfigured", err)
	}
}

func TestSearchFlightsBuildsRequest(t *testing.T) {
	var (
		gotMethod, gotPath, gotAuth, gotVersion string
		gotBody                                 offerRequestCreate
	)
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotVersion = r.Header.Get("Duffel-Version")
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		if r.URL.Query().Get("return_offers") != "true" {
			t.Errorf("return_offers query = %q, want true", r.URL.Query().Get("return_offers"))
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"id":"orq_1","offers":[]}}`))
	})

	ret := mustDate(t, "2026-09-20")
	_, err := c.SearchFlights(context.Background(), models.FlightSearchRequest{
		Origin: "LHR", Destination: "JFK",
		DepartureDate: mustDate(t, "2026-09-10"), ReturnDate: &ret,
		Adults: 2, Children: 1, Infants: 1,
		CabinClass: models.CabinClass("BUSINESS"),
	})
	if err != nil {
		t.Fatalf("SearchFlights: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/air/offer_requests" {
		t.Errorf("path = %s", gotPath)
	}
	if gotAuth != "Bearer test-token" {
		t.Errorf("Authorization = %q", gotAuth)
	}
	if gotVersion != apiVersion {
		t.Errorf("Duffel-Version = %q, want %q", gotVersion, apiVersion)
	}
	if gotBody.Data.CabinClass != "business" {
		t.Errorf("cabin_class = %q, want business", gotBody.Data.CabinClass)
	}

	// A return date must produce a second, reversed slice.
	if len(gotBody.Data.Slices) != 2 {
		t.Fatalf("slices = %d, want 2", len(gotBody.Data.Slices))
	}
	out, back := gotBody.Data.Slices[0], gotBody.Data.Slices[1]
	if out.Origin != "LHR" || out.Destination != "JFK" || out.DepartureDate != "2026-09-10" {
		t.Errorf("outbound slice = %+v", out)
	}
	if back.Origin != "JFK" || back.Destination != "LHR" || back.DepartureDate != "2026-09-20" {
		t.Errorf("return slice = %+v", back)
	}

	// 2 adults + 1 child + 1 infant, with an age only on the child.
	if len(gotBody.Data.Passengers) != 4 {
		t.Fatalf("passengers = %d, want 4", len(gotBody.Data.Passengers))
	}
	for _, p := range gotBody.Data.Passengers {
		if p.Type == "child" && p.Age == nil {
			t.Error("child passenger sent without an age; Duffel rejects that")
		}
		if p.Type == "adult" && p.Age != nil {
			t.Error("adult passenger should not carry an age")
		}
	}
}

func TestSearchFlightsOneWaySendsSingleSlice(t *testing.T) {
	var gotBody offerRequestCreate
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotBody)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"offers":[]}}`))
	})
	_, err := c.SearchFlights(context.Background(), models.FlightSearchRequest{
		Origin: "LHR", Destination: "JFK", DepartureDate: mustDate(t, "2026-09-10"),
	})
	if err != nil {
		t.Fatalf("SearchFlights: %v", err)
	}
	if len(gotBody.Data.Slices) != 1 {
		t.Errorf("one-way search sent %d slices, want 1", len(gotBody.Data.Slices))
	}
}

// singleOfferJSON is one offer body, shared by the booking tests which need
// GetFlightOffer to succeed before the order is placed.
const singleOfferJSON = `{
  "id":"off_123",
  "total_amount":"431.50","total_currency":"GBP",
  "base_amount":"300.25","tax_amount":"131.25",
  "expires_at":"2026-09-01T12:00:00Z",
  "slices":[]
}`

const offerPayload = `{"data":{"id":"orq_1","offers":[{
  "id":"off_123",
  "total_amount":"431.50","total_currency":"GBP",
  "base_amount":"300.25","tax_amount":"131.25",
  "expires_at":"2026-09-01T12:00:00Z",
  "slices":[{"duration":"PT7H55M","segments":[{
    "id":"seg_1",
    "departing_at":"2026-09-10T09:00:00Z",
    "arriving_at":"2026-09-10T16:55:00Z",
    "duration":"PT7H55M",
    "origin":{"iata_code":"LHR","name":"Heathrow","terminal":"5"},
    "destination":{"iata_code":"JFK","name":"Kennedy","terminal":"4"},
    "marketing_carrier":{"name":"British Airways","iata_code":"BA"},
    "marketing_carrier_flight_number":"117"
  }]}]
}]}}`

func TestSearchFlightsMapsOfferToModel(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(offerPayload))
	})
	offers, err := c.SearchFlights(context.Background(), models.FlightSearchRequest{
		Origin: "LHR", Destination: "JFK", DepartureDate: mustDate(t, "2026-09-10"),
	})
	if err != nil {
		t.Fatalf("SearchFlights: %v", err)
	}
	if len(offers) != 1 {
		t.Fatalf("offers = %d, want 1", len(offers))
	}
	o := offers[0]
	if o.ID != "off_123" || o.Provider != "duffel" {
		t.Errorf("id/provider = %q/%q", o.ID, o.Provider)
	}
	// Duffel sends money as strings; they must survive the float conversion.
	if o.Price.Total != 431.50 || o.Price.Base != 300.25 || o.Price.Taxes != 131.25 {
		t.Errorf("price = %+v", o.Price)
	}
	if o.Price.Currency != "GBP" {
		t.Errorf("currency = %q", o.Price.Currency)
	}
	if len(o.Itineraries) != 1 || len(o.Itineraries[0].Segments) != 1 {
		t.Fatalf("itineraries = %+v", o.Itineraries)
	}
	seg := o.Itineraries[0].Segments[0]
	// Flight number is carrier code + number, not the bare number.
	if seg.FlightNumber != "BA117" {
		t.Errorf("flight number = %q, want BA117", seg.FlightNumber)
	}
	if seg.Departure.Airport != "LHR" || seg.Departure.Terminal != "5" {
		t.Errorf("departure = %+v", seg.Departure)
	}
	if seg.Arrival.Airport != "JFK" || seg.Arrival.Terminal != "4" {
		t.Errorf("arrival = %+v", seg.Arrival)
	}
	if !seg.Departure.DateTime.Equal(time.Date(2026, 9, 10, 9, 0, 0, 0, time.UTC)) {
		t.Errorf("departure time = %v", seg.Departure.DateTime)
	}
}

func TestSearchFlightsSurfacesAPIError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"errors":[{"title":"Invalid origin","message":"XXX is not an airport","code":"invalid_origin"}]}`))
	})
	_, err := c.SearchFlights(context.Background(), models.FlightSearchRequest{Origin: "XXX"})
	if err == nil {
		t.Fatal("expected an error for a 422 response")
	}
	// The Duffel error envelope should be unwrapped into the message, not
	// swallowed into a bare status code.
	for _, want := range []string{"422", "Invalid origin", "XXX is not an airport", "invalid_origin"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestSearchFlightsNonJSONErrorBodyStillReported(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream exploded"))
	})
	_, err := c.SearchFlights(context.Background(), models.FlightSearchRequest{})
	if err == nil || !strings.Contains(err.Error(), "upstream exploded") {
		t.Errorf("err = %v, want the raw body surfaced", err)
	}
}

func TestGetFlightOfferNotFoundIsDistinct(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	_, err := c.GetFlightOffer(context.Background(), "off_gone")
	if err == nil {
		t.Fatal("expected an error for 404")
	}
	// Expired offers are routine in booking flows and need a recognisable message.
	if !strings.Contains(err.Error(), "expired") || !strings.Contains(err.Error(), "off_gone") {
		t.Errorf("err = %v, want an expired/not-found message naming the offer", err)
	}
}

func TestGetFlightOfferEscapesIDInPath(t *testing.T) {
	var gotPath string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		w.WriteHeader(http.StatusNotFound)
	})
	_, _ = c.GetFlightOffer(context.Background(), "off/../../etc")
	if strings.Contains(gotPath, "../") {
		t.Errorf("path traversal reached the wire: %q", gotPath)
	}
}

func TestPriceFlightOfferDelegatesToGet(t *testing.T) {
	var hits int
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		w.WriteHeader(http.StatusNotFound)
	})
	_, _ = c.PriceFlightOffer(context.Background(), "off_1")
	if hits != 1 {
		t.Errorf("upstream hits = %d, want 1", hits)
	}
}

func TestPassengerList(t *testing.T) {
	tests := []struct {
		name                            string
		req                             models.FlightSearchRequest
		adults, children, infants, tota int
	}{
		{"defaults to one adult", models.FlightSearchRequest{}, 1, 0, 0, 1},
		{"zero adults still books one", models.FlightSearchRequest{Adults: 0}, 1, 0, 0, 1},
		{"negative adults clamped", models.FlightSearchRequest{Adults: -3}, 1, 0, 0, 1},
		{"family", models.FlightSearchRequest{Adults: 2, Children: 2, Infants: 1}, 2, 2, 1, 5},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := passengerList(tc.req)
			if len(got) != tc.tota {
				t.Fatalf("len = %d, want %d", len(got), tc.tota)
			}
			var a, c, i int
			for _, p := range got {
				switch p.Type {
				case "adult":
					a++
				case "child":
					c++
				case "infant_without_seat":
					i++
				default:
					t.Errorf("unexpected passenger type %q", p.Type)
				}
			}
			if a != tc.adults || c != tc.children || i != tc.infants {
				t.Errorf("adults/children/infants = %d/%d/%d, want %d/%d/%d",
					a, c, i, tc.adults, tc.children, tc.infants)
			}
		})
	}
}

func TestDuffelCabinClass(t *testing.T) {
	tests := map[string]string{
		"":                 "economy",
		"ECONOMY":          "economy",
		"economy":          "economy",
		"PREMIUM_ECONOMY":  "premium_economy",
		"BUSINESS":         "business",
		"business":         "business",
		"FIRST":            "first",
		"NOT_A_REAL_CABIN": "economy", // unknown input must not reach Duffel verbatim
	}
	for in, want := range tests {
		if got := duffelCabinClass(models.CabinClass(in)); got != want {
			t.Errorf("duffelCabinClass(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestContextCancellationIsPropagated(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		// Stall past the caller's deadline, but always return so the test
		// server can shut down (Close waits on in-flight handlers).
		select {
		case <-r.Context().Done():
		case <-time.After(2 * time.Second):
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.SearchFlights(ctx, models.FlightSearchRequest{Origin: "LHR"})
	if err == nil {
		t.Fatal("expected a context deadline error")
	}
}

// --- Booking, status and airport search ---

// BookFlight re-fetches the offer to get the payment amount, so the happy
// path is a two-request flow: GET the offer, then POST the order.
func TestBookFlightReFetchesOfferAndPostsOrder(t *testing.T) {
	var (
		seenPaths []string
		gotOrder  orderCreate
	)
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		seenPaths = append(seenPaths, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"data":` + singleOfferJSON + `}`))
			return
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &gotOrder)
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"id":"ord_1","booking_reference":"ABC123"}}`))
	})

	conf, err := c.BookFlight(context.Background(), models.FlightBookingRequest{
		OfferID: "off_123",
		Passengers: []models.PassengerInfo{
			{FirstName: "Ada", LastName: "Lovelace", Gender: "female",
				DateOfBirth: time.Date(1990, 5, 1, 0, 0, 0, 0, time.UTC), Email: "ada@example.com"},
		},
	})
	if err != nil {
		t.Fatalf("BookFlight: %v", err)
	}

	if len(seenPaths) != 2 || seenPaths[1] != "POST /air/orders" {
		t.Fatalf("request sequence = %v", seenPaths)
	}
	// Payment must carry the offer's own total and currency, not zero.
	if len(gotOrder.Data.Payments) != 1 {
		t.Fatalf("payments = %+v", gotOrder.Data.Payments)
	}
	if gotOrder.Data.Payments[0].Amount != "431.50" {
		t.Errorf("payment amount = %q, want the offer total 431.50", gotOrder.Data.Payments[0].Amount)
	}
	if gotOrder.Data.Payments[0].Currency != "GBP" {
		t.Errorf("payment currency = %q", gotOrder.Data.Payments[0].Currency)
	}
	if len(gotOrder.Data.SelectedOffers) != 1 || gotOrder.Data.SelectedOffers[0] != "off_123" {
		t.Errorf("selected_offers = %v", gotOrder.Data.SelectedOffers)
	}
	if len(gotOrder.Data.Passengers) != 1 || gotOrder.Data.Passengers[0].BornOn != "1990-05-01" {
		t.Errorf("passenger = %+v", gotOrder.Data.Passengers)
	}
	// booking_reference is what a traveller quotes; prefer it over the order ID.
	if conf.Reference != "ABC123" {
		t.Errorf("reference = %q, want the booking reference", conf.Reference)
	}
	if conf.Provider != "duffel" {
		t.Errorf("provider = %q", conf.Provider)
	}
}

func TestBookFlightFallsBackToOrderIDWhenNoReference(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"data":` + singleOfferJSON + `}`))
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"id":"ord_9"}}`))
	})
	conf, err := c.BookFlight(context.Background(), models.FlightBookingRequest{
		OfferID:    "off_123",
		Passengers: []models.PassengerInfo{{FirstName: "A", LastName: "B"}},
	})
	if err != nil {
		t.Fatalf("BookFlight: %v", err)
	}
	if conf.Reference != "ord_9" {
		t.Errorf("reference = %q, want the order ID fallback", conf.Reference)
	}
}

// If the offer can't be re-fetched we must not attempt the order at all —
// booking without a confirmed price is worse than failing.
func TestBookFlightAbortsWhenOfferGone(t *testing.T) {
	var posts int
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
		}
		w.WriteHeader(http.StatusNotFound)
	})
	_, err := c.BookFlight(context.Background(), models.FlightBookingRequest{
		OfferID:    "off_gone",
		Passengers: []models.PassengerInfo{{FirstName: "A", LastName: "B"}},
	})
	if err == nil {
		t.Fatal("expected an error when the offer is unavailable")
	}
	if posts != 0 {
		t.Errorf("order was POSTed %d times despite the offer lookup failing", posts)
	}
	if !strings.Contains(err.Error(), "re-fetch offer") {
		t.Errorf("err = %v, want it to name the re-fetch step", err)
	}
}

func TestBookFlightSurfacesOrderError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"data":` + singleOfferJSON + `}`))
			return
		}
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte(`{"errors":[{"title":"Offer expired","message":"no longer bookable","code":"offer_expired"}]}`))
	})
	_, err := c.BookFlight(context.Background(), models.FlightBookingRequest{
		OfferID:    "off_123",
		Passengers: []models.PassengerInfo{{FirstName: "A", LastName: "B"}},
	})
	if err == nil {
		t.Fatal("expected an error")
	}
	for _, want := range []string{"Offer expired", "offer_expired"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing %q", err, want)
		}
	}
}

func TestNormalizeGender(t *testing.T) {
	tests := map[string]string{
		"male": "m", "MALE": "m", "m": "m", "M": "m",
		"female": "f", "Female": "f", "f": "f",
		// Duffel rejects anything outside m/f/x, so unknown input must not be
		// passed through verbatim.
		"": "m", "nonbinary": "m", "x": "m",
	}
	for in, want := range tests {
		if got := normalizeGender(in); got != want {
			t.Errorf("normalizeGender(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGetFlightStatus(t *testing.T) {
	t.Run("confirmed", func(t *testing.T) {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":{}}`))
		})
		st, err := c.GetFlightStatus(context.Background(), "ABC123")
		if err != nil {
			t.Fatalf("GetFlightStatus: %v", err)
		}
		if *st != models.BookingStatusConfirmed {
			t.Errorf("status = %v, want confirmed", *st)
		}
	})

	t.Run("cancelled", func(t *testing.T) {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(`{"data":{"cancelled_at":true}}`))
		})
		st, err := c.GetFlightStatus(context.Background(), "ABC123")
		if err != nil {
			t.Fatalf("GetFlightStatus: %v", err)
		}
		if *st != models.BookingStatusCancelled {
			t.Errorf("status = %v, want cancelled", *st)
		}
	})

	t.Run("not found", func(t *testing.T) {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		})
		_, err := c.GetFlightStatus(context.Background(), "NOPE")
		if err == nil || !strings.Contains(err.Error(), "NOPE") {
			t.Errorf("err = %v, want a not-found naming the reference", err)
		}
	})
}

func TestSearchAirportsFiltersToAirports(t *testing.T) {
	var gotQuery string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("query")
		_, _ = w.Write([]byte(`{"data":[
			{"type":"airport","iata_code":"LHR","name":"Heathrow","city_name":"London","iata_country_code":"GB"},
			{"type":"city","iata_code":"LON","name":"London"},
			{"type":"airport","iata_code":"","name":"No code airport"}
		]}`))
	})
	airports, err := c.SearchAirports(context.Background(), "london")
	if err != nil {
		t.Fatalf("SearchAirports: %v", err)
	}
	if gotQuery != "london" {
		t.Errorf("query = %q", gotQuery)
	}
	// Cities and code-less entries are useless for a flight search.
	if len(airports) != 1 {
		t.Fatalf("got %d airports, want only the one with a code: %+v", len(airports), airports)
	}
	a := airports[0]
	if a.Code != "LHR" || a.City != "London" || a.Country != "GB" {
		t.Errorf("airport = %+v", a)
	}
}

func TestSearchAirportsEmptyResult(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"data":[]}`))
	})
	airports, err := c.SearchAirports(context.Background(), "zzz")
	if err != nil {
		t.Fatalf("SearchAirports: %v", err)
	}
	if len(airports) != 0 {
		t.Errorf("got %d airports, want 0", len(airports))
	}
}
