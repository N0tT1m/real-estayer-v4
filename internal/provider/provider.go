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
