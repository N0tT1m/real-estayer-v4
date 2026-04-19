package provider

import (
	"context"

	"github.com/realestayer/v4/internal/models"
)

// FlightProvider defines the interface for flight booking providers
type FlightProvider interface {
	// Name returns the provider identifier
	Name() string

	// SearchFlights searches for available flights
	SearchFlights(ctx context.Context, req models.FlightSearchRequest) ([]models.FlightOffer, error)

	// GetFlightOffer retrieves a specific flight offer by ID
	GetFlightOffer(ctx context.Context, offerID string) (*models.FlightOffer, error)

	// PriceFlightOffer confirms current pricing for an offer
	PriceFlightOffer(ctx context.Context, offerID string) (*models.FlightOffer, error)

	// BookFlight creates a flight booking
	BookFlight(ctx context.Context, req models.FlightBookingRequest) (*models.BookingConfirmation, error)

	// GetFlightStatus retrieves booking status
	GetFlightStatus(ctx context.Context, bookingRef string) (*models.BookingStatus, error)

	// SearchAirports searches for airports by keyword
	SearchAirports(ctx context.Context, keyword string) ([]models.Airport, error)
}

// HotelProvider defines the interface for hotel booking providers
type HotelProvider interface {
	// Name returns the provider identifier
	Name() string

	// SearchHotels searches for available hotels
	SearchHotels(ctx context.Context, req models.HotelSearchRequest) ([]models.HotelOffer, error)

	// GetHotelDetails retrieves detailed hotel information
	GetHotelDetails(ctx context.Context, hotelID string) (*models.HotelInfo, error)

	// GetRoomAvailability gets available rooms for a hotel
	GetRoomAvailability(ctx context.Context, hotelID string, checkIn, checkOut string, guests int) ([]models.RoomOffer, error)

	// BookHotel creates a hotel booking
	BookHotel(ctx context.Context, req models.HotelBookingRequest) (*models.BookingConfirmation, error)

	// SearchCities searches for cities by keyword
	SearchCities(ctx context.Context, keyword string) ([]models.City, error)
}

// CarProvider defines the interface for car rental providers
type CarProvider interface {
	// Name returns the provider identifier
	Name() string

	// SearchCars searches for available rental cars
	SearchCars(ctx context.Context, req models.CarSearchRequest) ([]models.CarOffer, error)

	// GetCarOffer retrieves a specific car offer by ID
	GetCarOffer(ctx context.Context, offerID string) (*models.CarOffer, error)

	// BookCar creates a car rental booking
	BookCar(ctx context.Context, req models.CarBookingRequest) (*models.BookingConfirmation, error)

	// GetRentalStatus retrieves rental booking status
	GetRentalStatus(ctx context.Context, bookingRef string) (*models.BookingStatus, error)
}

// UnifiedProvider combines all provider interfaces
type UnifiedProvider interface {
	FlightProvider
	HotelProvider
	CarProvider
}
