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

// CarCategory represents car rental categories
type CarCategory string

const (
	CarCategoryEconomy    CarCategory = "ECONOMY"
	CarCategoryCompact    CarCategory = "COMPACT"
	CarCategoryMidsize    CarCategory = "MIDSIZE"
	CarCategoryFullsize   CarCategory = "FULLSIZE"
	CarCategorySUV        CarCategory = "SUV"
	CarCategoryLuxury     CarCategory = "LUXURY"
	CarCategoryVan        CarCategory = "VAN"
	CarCategoryConvertible CarCategory = "CONVERTIBLE"
)

// TransmissionType represents car transmission types
type TransmissionType string

const (
	TransmissionAuto   TransmissionType = "AUTOMATIC"
	TransmissionManual TransmissionType = "MANUAL"
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
	ID              string          `json:"id"`
	Provider        string          `json:"provider"`
	Itineraries     []Itinerary     `json:"itineraries"`
	Price           Price           `json:"price"`
	ValidUntil      time.Time       `json:"valid_until"`
	SeatsRemaining  int             `json:"seats_remaining,omitempty"`
	InstantTicketing bool           `json:"instant_ticketing"`
}

// Itinerary represents outbound or return journey
type Itinerary struct {
	Duration string          `json:"duration"`
	Segments []FlightSegment `json:"segments"`
}

// FlightBookingRequest holds flight booking data
type FlightBookingRequest struct {
	OfferID     string            `json:"offer_id"`
	Passengers  []PassengerInfo   `json:"passengers"`
	Contact     ContactInfo       `json:"contact"`
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
	Provider         string    `json:"provider"`
	Reference        string    `json:"reference"`
	Status           string    `json:"status"`
	ConfirmedAt      time.Time `json:"confirmed_at"`
	TotalCharged     Price     `json:"total_charged"`
	ConfirmationURL  string    `json:"confirmation_url,omitempty"`
	Details          any       `json:"details,omitempty"`
}

// =========== HOTEL TYPES ===========

// HotelSearchRequest holds hotel search parameters
type HotelSearchRequest struct {
	CityCode    string   `json:"city_code"`
	CheckIn     time.Time `json:"check_in"`
	CheckOut    time.Time `json:"check_out"`
	Adults      int       `json:"adults"`
	Rooms       int       `json:"rooms"`
	StarRatings []int     `json:"star_ratings,omitempty"`
	ChainCodes  []string  `json:"chain_codes,omitempty"`
	Amenities   []string  `json:"amenities,omitempty"`
	MaxPrice    *float64  `json:"max_price,omitempty"`
	Currency    string    `json:"currency"`
	Radius      int       `json:"radius,omitempty"` // km
}

// HotelOffer represents a hotel search result
type HotelOffer struct {
	ID          string       `json:"id"`
	Provider    string       `json:"provider"`
	Hotel       HotelInfo    `json:"hotel"`
	Offers      []RoomOffer  `json:"offers"`
	Available   bool         `json:"available"`
}

// HotelInfo holds basic hotel information
type HotelInfo struct {
	HotelID     string       `json:"hotel_id"`
	Name        string       `json:"name"`
	ChainCode   string       `json:"chain_code,omitempty"`
	Rating      int          `json:"rating"`
	Address     Address      `json:"address"`
	Coordinates *Coordinates `json:"coordinates,omitempty"`
	Amenities   []string     `json:"amenities,omitempty"`
	ImageURL    string       `json:"image_url,omitempty"`
	Description string       `json:"description,omitempty"`
}

// Address holds physical address
type Address struct {
	Lines      []string `json:"lines"`
	City       string   `json:"city"`
	PostalCode string   `json:"postal_code"`
	Country    string   `json:"country"`
}

// RoomOffer represents a room availability
type RoomOffer struct {
	ID               string    `json:"id"`
	RoomType         string    `json:"room_type"`
	Description      string    `json:"description"`
	BedType          string    `json:"bed_type"`
	Guests           int       `json:"guests"`
	Price            Price     `json:"price"`
	CancellationPolicy string  `json:"cancellation_policy"`
	PaymentType      string    `json:"payment_type"` // prepay, pay_at_property
}

// HotelBookingRequest holds hotel booking data
type HotelBookingRequest struct {
	OfferID     string            `json:"offer_id"`
	HotelID     string            `json:"hotel_id"`
	Guests      []GuestInfo       `json:"guests"`
	Contact     ContactInfo       `json:"contact"`
	PaymentInfo BookingPaymentInfo `json:"payment"`
	SpecialRequests string        `json:"special_requests,omitempty"`
}

// GuestInfo holds guest details
type GuestInfo struct {
	Title     string `json:"title"` // MR, MS, MRS
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Phone     string `json:"phone"`
}

// =========== CAR RENTAL TYPES ===========

// CarSearchRequest holds car rental search parameters
type CarSearchRequest struct {
	PickupLocation   string           `json:"pickup_location"`  // airport code or address
	DropoffLocation  string           `json:"dropoff_location"` // can differ from pickup
	PickupDateTime   time.Time        `json:"pickup_datetime"`
	DropoffDateTime  time.Time        `json:"dropoff_datetime"`
	DriverAge        int              `json:"driver_age"`
	Category         CarCategory      `json:"category,omitempty"`
	TransmissionType TransmissionType `json:"transmission_type,omitempty"`
	Currency         string           `json:"currency"`
}

// CarOffer represents a car rental search result
type CarOffer struct {
	ID           string   `json:"id"`
	Provider     string   `json:"provider"`
	Vendor       string   `json:"vendor"`      // e.g., "Hertz", "Enterprise"
	VendorCode   string   `json:"vendor_code"` // e.g., "ZE", "ET"
	Vehicle      Vehicle  `json:"vehicle"`
	Price        Price    `json:"price"`
	RateType     string   `json:"rate_type"` // daily, weekly
	Pickup       Location `json:"pickup"`
	Dropoff      Location `json:"dropoff"`
	Policies     CarPolicies `json:"policies"`
}

// Vehicle represents car details
type Vehicle struct {
	Category     CarCategory      `json:"category"`
	Type         string           `json:"type"` // e.g., "Compact SUV"
	Make         string           `json:"make,omitempty"`
	Model        string           `json:"model,omitempty"`
	Doors        int              `json:"doors"`
	Seats        int              `json:"seats"`
	Bags         int              `json:"bags"` // luggage capacity
	Transmission TransmissionType `json:"transmission"`
	AirCon       bool             `json:"air_con"`
	Fuel         string           `json:"fuel"` // gasoline, diesel, electric, hybrid
	ImageURL     string           `json:"image_url,omitempty"`
}

// Location represents pickup/dropoff location
type Location struct {
	Code      string       `json:"code"` // airport code or location ID
	Name      string       `json:"name"`
	Address   string       `json:"address,omitempty"`
	Coordinates *Coordinates `json:"coordinates,omitempty"`
	DateTime  time.Time    `json:"datetime"`
}

// CarPolicies holds rental policies
type CarPolicies struct {
	Mileage          string `json:"mileage"` // unlimited, 200/day
	FuelPolicy       string `json:"fuel_policy"` // full_to_full, prepaid
	InsuranceIncluded bool  `json:"insurance_included"`
	Cancellation     string `json:"cancellation"`
	MinAge           int    `json:"min_age"`
	MaxAge           int    `json:"max_age,omitempty"`
}

// CarBookingRequest holds car rental booking data
type CarBookingRequest struct {
	OfferID     string            `json:"offer_id"`
	Driver      DriverInfo        `json:"driver"`
	Contact     ContactInfo       `json:"contact"`
	PaymentInfo BookingPaymentInfo `json:"payment"`
	Extras      []string          `json:"extras,omitempty"` // GPS, child_seat, etc.
}

// DriverInfo holds driver details
type DriverInfo struct {
	Title          string    `json:"title"`
	FirstName      string    `json:"first_name"`
	LastName       string    `json:"last_name"`
	Email          string    `json:"email"`
	Phone          string    `json:"phone"`
	DateOfBirth    time.Time `json:"date_of_birth"`
	LicenseNumber  string    `json:"license_number"`
	LicenseCountry string    `json:"license_country"`
}

// =========== LOCATION TYPES ===========

// Airport represents an airport for autocomplete
type Airport struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	City    string `json:"city"`
	Country string `json:"country"`
}

// City represents a city for autocomplete
type City struct {
	Code    string `json:"code"`
	Name    string `json:"name"`
	Country string `json:"country"`
}
