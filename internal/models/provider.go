package models

import (
	"time"
)

// CabinClass represents flight cabin classes
type CabinClass string

const (
	CabinEconomy        CabinClass = "ECONOMY"
	CabinPremiumEconomy CabinClass = "PREMIUM_ECONOMY"
	CabinBusiness       CabinClass = "BUSINESS"
	CabinFirst          CabinClass = "FIRST"
)

// =========== FLIGHT TYPES ===========

// FlightSearchRequest holds flight search parameters
type FlightSearchRequest struct {
	Origin        string     `json:"origin"`
	Destination   string     `json:"destination"`
	DepartureDate time.Time  `json:"departure_date"`
	ReturnDate    *time.Time `json:"return_date,omitempty"` // nil for one-way
	Adults        int        `json:"adults"`
	Children      int        `json:"children"`
	Infants       int        `json:"infants"`
	CabinClass    CabinClass `json:"cabin_class"`
	Currency      string     `json:"currency"`
	MaxPrice      *float64   `json:"max_price,omitempty"`
	DirectOnly    bool       `json:"direct_only"`
}

// FlightOffer represents a flight search result
type FlightOffer struct {
	ID               string      `json:"id"`
	Provider         string      `json:"provider"`
	Itineraries      []Itinerary `json:"itineraries"`
	Price            Price       `json:"price"`
	ValidUntil       time.Time   `json:"valid_until"`
	SeatsRemaining   int         `json:"seats_remaining,omitempty"`
	InstantTicketing bool        `json:"instant_ticketing"`
}

// Itinerary represents outbound or return journey
type Itinerary struct {
	Duration string          `json:"duration"`
	Segments []FlightSegment `json:"segments"`
}

// FlightBookingRequest holds flight booking data
type FlightBookingRequest struct {
	OfferID     string             `json:"offer_id"`
	Passengers  []PassengerInfo    `json:"passengers"`
	Contact     ContactInfo        `json:"contact"`
	PaymentInfo BookingPaymentInfo `json:"payment"`
}

// PassengerInfo holds passenger details for booking
type PassengerInfo struct {
	FirstName       string    `json:"first_name"`
	LastName        string    `json:"last_name"`
	DateOfBirth     time.Time `json:"date_of_birth"`
	Gender          string    `json:"gender"` // MALE, FEMALE
	Email           string    `json:"email"`
	Phone           string    `json:"phone"`
	PassportNumber  string    `json:"passport_number,omitempty"`
	PassportExpiry  time.Time `json:"passport_expiry,omitempty"`
	PassportCountry string    `json:"passport_country,omitempty"`
}

// ContactInfo holds contact details
type ContactInfo struct {
	Email       string `json:"email"`
	Phone       string `json:"phone"`
	CountryCode string `json:"country_code"`
}

// BookingPaymentInfo holds payment card details
type BookingPaymentInfo struct {
	CardType       string `json:"card_type"` // VI, MC, AX
	CardNumber     string `json:"card_number"`
	ExpiryMonth    string `json:"expiry_month"`
	ExpiryYear     string `json:"expiry_year"`
	CVV            string `json:"cvv"`
	CardholderName string `json:"cardholder_name"`
}

// BookingConfirmation is returned after successful booking
type BookingConfirmation struct {
	Provider        string    `json:"provider"`
	Reference       string    `json:"reference"`
	Status          string    `json:"status"`
	ConfirmedAt     time.Time `json:"confirmed_at"`
	TotalCharged    Price     `json:"total_charged"`
	ConfirmationURL string    `json:"confirmation_url,omitempty"`
	Details         any       `json:"details,omitempty"`
}

// Airport represents an airport for autocomplete
type Airport struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	City    string `json:"city"`
	Country string `json:"country"`
}
