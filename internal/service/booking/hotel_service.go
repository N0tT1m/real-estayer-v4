package booking

import (
	"context"
	"errors"

	"github.com/realestayer/v3/internal/models"
	"github.com/realestayer/v3/internal/provider"
	"github.com/realestayer/v3/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// HotelService handles hotel booking operations
type HotelService struct {
	registry    *provider.Registry
	bookingRepo *repository.BookingRepository
}

// NewHotelService creates a new hotel service
func NewHotelService(registry *provider.Registry, bookingRepo *repository.BookingRepository) *HotelService {
	return &HotelService{
		registry:    registry,
		bookingRepo: bookingRepo,
	}
}

// Search searches for available hotels
func (s *HotelService) Search(ctx context.Context, req models.HotelSearchRequest) ([]models.HotelOffer, error) {
	return s.registry.SearchHotelsAll(ctx, req)
}

// GetDetails retrieves detailed hotel information
func (s *HotelService) GetDetails(ctx context.Context, providerName, hotelID string) (*models.HotelInfo, error) {
	hotelProvider, err := s.registry.GetHotelProvider(providerName)
	if err != nil {
		return nil, err
	}

	return hotelProvider.GetHotelDetails(ctx, hotelID)
}

// GetRoomAvailability gets available rooms for a hotel
func (s *HotelService) GetRoomAvailability(ctx context.Context, providerName, hotelID, checkIn, checkOut string, guests int) ([]models.RoomOffer, error) {
	hotelProvider, err := s.registry.GetHotelProvider(providerName)
	if err != nil {
		return nil, err
	}

	return hotelProvider.GetRoomAvailability(ctx, hotelID, checkIn, checkOut, guests)
}

// Book creates a hotel booking
func (s *HotelService) Book(ctx context.Context, userID string, req models.HotelBookingRequest, providerName string) (*models.Booking, error) {
	userObjID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errors.New("invalid user ID")
	}

	hotelProvider, err := s.registry.GetHotelProvider(providerName)
	if err != nil {
		return nil, err
	}

	// Create booking with provider
	confirmation, err := hotelProvider.BookHotel(ctx, req)
	if err != nil {
		return nil, err
	}

	// Get hotel details for booking record
	hotelInfo, _ := hotelProvider.GetHotelDetails(ctx, req.HotelID)

	booking := &models.Booking{
		UserID:            userObjID,
		Type:              models.BookingTypeHotel,
		Provider:          providerName,
		ProviderReference: confirmation.Reference,
		Status:            models.BookingStatusConfirmed,
		Details: models.BookingDetails{
			Hotel: &models.HotelBookingDetails{
				Name:   hotelInfo.Name,
				Guests: len(req.Guests),
			},
		},
		Price: confirmation.TotalCharged,
	}

	// Save booking
	if err := s.bookingRepo.Create(ctx, booking); err != nil {
		return nil, err
	}

	return booking, nil
}

// SearchCities searches for cities
func (s *HotelService) SearchCities(ctx context.Context, keyword string) ([]models.City, error) {
	hotelProvider, err := s.registry.GetHotelProvider("")
	if err != nil {
		return nil, err
	}

	return hotelProvider.SearchCities(ctx, keyword)
}
