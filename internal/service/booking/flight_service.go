package booking

import (
	"context"
	"errors"

	"github.com/realestayer/v3/internal/models"
	"github.com/realestayer/v3/internal/provider"
	"github.com/realestayer/v3/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// FlightService handles flight booking operations
type FlightService struct {
	registry    *provider.Registry
	bookingRepo *repository.BookingRepository
}

// NewFlightService creates a new flight service
func NewFlightService(registry *provider.Registry, bookingRepo *repository.BookingRepository) *FlightService {
	return &FlightService{
		registry:    registry,
		bookingRepo: bookingRepo,
	}
}

// Search searches for available flights
func (s *FlightService) Search(ctx context.Context, req models.FlightSearchRequest) ([]models.FlightOffer, error) {
	// Search across all providers
	return s.registry.SearchFlightsAll(ctx, req)
}

// GetOffer retrieves a specific flight offer
func (s *FlightService) GetOffer(ctx context.Context, providerName, offerID string) (*models.FlightOffer, error) {
	flightProvider, err := s.registry.GetFlightProvider(providerName)
	if err != nil {
		return nil, err
	}

	return flightProvider.GetFlightOffer(ctx, offerID)
}

// Book creates a flight booking
func (s *FlightService) Book(ctx context.Context, userID string, req models.FlightBookingRequest, providerName string) (*models.Booking, error) {
	userObjID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errors.New("invalid user ID")
	}

	flightProvider, err := s.registry.GetFlightProvider(providerName)
	if err != nil {
		return nil, err
	}

	// Create booking with provider
	confirmation, err := flightProvider.BookFlight(ctx, req)
	if err != nil {
		return nil, err
	}

	// Build flight segments from passengers info
	// In a real implementation, you'd get this from the offer
	booking := &models.Booking{
		UserID:            userObjID,
		Type:              models.BookingTypeFlight,
		Provider:          providerName,
		ProviderReference: confirmation.Reference,
		Status:            models.BookingStatusConfirmed,
		Details: models.BookingDetails{
			Flights: []models.FlightSegment{}, // Would be populated from offer
		},
		Price: confirmation.TotalCharged,
	}

	// Convert passengers
	passengers := make([]models.Passenger, len(req.Passengers))
	for i, p := range req.Passengers {
		passengers[i] = models.Passenger{
			FirstName:      p.FirstName,
			LastName:       p.LastName,
			DateOfBirth:    p.DateOfBirth,
			PassportNumber: p.PassportNumber,
			PassportExpiry: p.PassportExpiry,
			Email:          p.Email,
			Phone:          p.Phone,
		}
	}
	booking.Passengers = passengers

	// Save booking
	if err := s.bookingRepo.Create(ctx, booking); err != nil {
		return nil, err
	}

	return booking, nil
}

// SearchAirports searches for airports
func (s *FlightService) SearchAirports(ctx context.Context, keyword string) ([]models.Airport, error) {
	flightProvider, err := s.registry.GetFlightProvider("")
	if err != nil {
		return nil, err
	}

	return flightProvider.SearchAirports(ctx, keyword)
}
