package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// BookingType represents the type of booking
type BookingType string

const (
	BookingTypeFlight BookingType = "flight"
	BookingTypeHotel  BookingType = "hotel"
	BookingTypeCar    BookingType = "car"
)

// BookingStatus represents the status of a booking
type BookingStatus string

const (
	BookingStatusPending   BookingStatus = "pending"
	BookingStatusConfirmed BookingStatus = "confirmed"
	BookingStatusCancelled BookingStatus = "cancelled"
	BookingStatusCompleted BookingStatus = "completed"
)

// Booking represents a flight, hotel, or car booking
type Booking struct {
	ID                primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID            primitive.ObjectID `bson:"user_id" json:"user_id"`
	TripID            primitive.ObjectID `bson:"trip_id,omitempty" json:"trip_id,omitempty"`
	Type              BookingType        `bson:"type" json:"type"`
	Provider          string             `bson:"provider" json:"provider"`
	ProviderReference string             `bson:"provider_reference" json:"provider_reference"`
	Status            BookingStatus      `bson:"status" json:"status"`
	Details           BookingDetails     `bson:"details" json:"details"`
	Passengers        []Passenger        `bson:"passengers,omitempty" json:"passengers,omitempty"`
	Price             Price              `bson:"price" json:"price"`
	Payment           *PaymentInfo       `bson:"payment,omitempty" json:"payment,omitempty"`
	ConfirmationSent  bool               `bson:"confirmation_sent" json:"confirmation_sent"`
	CreatedAt         time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt         time.Time          `bson:"updated_at" json:"updated_at"`
}

// BookingDetails holds type-specific booking information
type BookingDetails struct {
	// Flight-specific
	Flights []FlightSegment `bson:"flights,omitempty" json:"flights,omitempty"`

	// Hotel-specific
	Hotel *HotelBookingDetails `bson:"hotel,omitempty" json:"hotel,omitempty"`

	// Car-specific
	Car *CarBookingDetails `bson:"car,omitempty" json:"car,omitempty"`
}

// FlightSegment represents a single flight leg
type FlightSegment struct {
	Departure    FlightEndpoint `bson:"departure" json:"departure"`
	Arrival      FlightEndpoint `bson:"arrival" json:"arrival"`
	Carrier      Carrier        `bson:"carrier" json:"carrier"`
	FlightNumber string         `bson:"flight_number" json:"flight_number"`
	CabinClass   string         `bson:"cabin_class" json:"cabin_class"`
	Duration     string         `bson:"duration,omitempty" json:"duration,omitempty"`
}

// FlightEndpoint represents departure or arrival info
type FlightEndpoint struct {
	Airport  string    `bson:"airport" json:"airport"`
	Terminal string    `bson:"terminal,omitempty" json:"terminal,omitempty"`
	DateTime time.Time `bson:"datetime" json:"datetime"`
}

// Carrier represents an airline
type Carrier struct {
	Code string `bson:"code" json:"code"`
	Name string `bson:"name" json:"name"`
}

// HotelBookingDetails holds hotel-specific booking info
type HotelBookingDetails struct {
	Name     string    `bson:"name" json:"name"`
	Address  string    `bson:"address" json:"address"`
	CheckIn  time.Time `bson:"check_in" json:"check_in"`
	CheckOut time.Time `bson:"check_out" json:"check_out"`
	RoomType string    `bson:"room_type" json:"room_type"`
	Guests   int       `bson:"guests" json:"guests"`
}

// CarBookingDetails holds car rental-specific booking info
type CarBookingDetails struct {
	Category        string    `bson:"category" json:"category"`
	Vendor          string    `bson:"vendor" json:"vendor"`
	VendorCode      string    `bson:"vendor_code" json:"vendor_code"`
	PickupLocation  string    `bson:"pickup_location" json:"pickup_location"`
	DropoffLocation string    `bson:"dropoff_location" json:"dropoff_location"`
	PickupDateTime  time.Time `bson:"pickup_datetime" json:"pickup_datetime"`
	DropoffDateTime time.Time `bson:"dropoff_datetime" json:"dropoff_datetime"`
	Transmission    string    `bson:"transmission" json:"transmission"`
	Features        []string  `bson:"features,omitempty" json:"features,omitempty"`
}

// Passenger represents a traveler
type Passenger struct {
	FirstName      string    `bson:"first_name" json:"first_name"`
	LastName       string    `bson:"last_name" json:"last_name"`
	DateOfBirth    time.Time `bson:"date_of_birth" json:"date_of_birth"`
	PassportNumber string    `bson:"passport_number,omitempty" json:"passport_number,omitempty"`
	PassportExpiry time.Time `bson:"passport_expiry,omitempty" json:"passport_expiry,omitempty"`
	Email          string    `bson:"email,omitempty" json:"email,omitempty"`
	Phone          string    `bson:"phone,omitempty" json:"phone,omitempty"`
}

// Price represents pricing information
type Price struct {
	Base     float64 `bson:"base" json:"base"`
	Taxes    float64 `bson:"taxes" json:"taxes"`
	Fees     float64 `bson:"fees" json:"fees"`
	Total    float64 `bson:"total" json:"total"`
	Currency string  `bson:"currency" json:"currency"`
}

// PaymentInfo holds payment details (masked for security)
type PaymentInfo struct {
	Method    string    `bson:"method" json:"method"`
	LastFour  string    `bson:"last_four" json:"last_four"`
	ChargedAt time.Time `bson:"charged_at" json:"charged_at"`
}
