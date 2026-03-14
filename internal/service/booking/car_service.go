package booking

import (
	"context"
	"errors"

	"github.com/realestayer/v3/internal/models"
	"github.com/realestayer/v3/internal/provider"
	"github.com/realestayer/v3/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// CarService handles car rental booking operations
type CarService struct {
	registry    *provider.Registry
	bookingRepo *repository.BookingRepository
}

// NewCarService creates a new car service
func NewCarService(registry *provider.Registry, bookingRepo *repository.BookingRepository) *CarService {
	return &CarService{
		registry:    registry,
		bookingRepo: bookingRepo,
	}
}

// Search searches for available rental cars
func (s *CarService) Search(ctx context.Context, req models.CarSearchRequest) ([]models.CarOffer, error) {
	return s.registry.SearchCarsAll(ctx, req)
}

// GetOffer retrieves a specific car offer
func (s *CarService) GetOffer(ctx context.Context, providerName, offerID string) (*models.CarOffer, error) {
	carProvider, err := s.registry.GetCarProvider(providerName)
	if err != nil {
		return nil, err
	}

	return carProvider.GetCarOffer(ctx, offerID)
}

// Book creates a car rental booking
func (s *CarService) Book(ctx context.Context, userID string, req models.CarBookingRequest, providerName string) (*models.Booking, error) {
	userObjID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errors.New("invalid user ID")
	}

	carProvider, err := s.registry.GetCarProvider(providerName)
	if err != nil {
		return nil, err
	}

	// Create booking with provider
	confirmation, err := carProvider.BookCar(ctx, req)
	if err != nil {
		return nil, err
	}

	// Get car offer details for booking record
	carOffer, _ := carProvider.GetCarOffer(ctx, req.OfferID)

	booking := &models.Booking{
		UserID:            userObjID,
		Type:              models.BookingTypeCar,
		Provider:          providerName,
		ProviderReference: confirmation.Reference,
		Status:            models.BookingStatusConfirmed,
		Details: models.BookingDetails{
			Car: &models.CarBookingDetails{
				Vendor:       req.Driver.FirstName + " " + req.Driver.LastName,
				Transmission: string(models.TransmissionAuto),
			},
		},
		Price: confirmation.TotalCharged,
	}

	// If we got offer details, use them
	if carOffer != nil {
		booking.Details.Car = &models.CarBookingDetails{
			Category:        string(carOffer.Vehicle.Category),
			Vendor:          carOffer.Vendor,
			VendorCode:      carOffer.VendorCode,
			PickupLocation:  carOffer.Pickup.Name,
			DropoffLocation: carOffer.Dropoff.Name,
			PickupDateTime:  carOffer.Pickup.DateTime,
			DropoffDateTime: carOffer.Dropoff.DateTime,
			Transmission:    string(carOffer.Vehicle.Transmission),
		}
	}

	// Save booking
	if err := s.bookingRepo.Create(ctx, booking); err != nil {
		return nil, err
	}

	return booking, nil
}

// GetRentalStatus retrieves the status of a car rental
func (s *CarService) GetRentalStatus(ctx context.Context, providerName, bookingRef string) (*models.BookingStatus, error) {
	carProvider, err := s.registry.GetCarProvider(providerName)
	if err != nil {
		return nil, err
	}

	return carProvider.GetRentalStatus(ctx, bookingRef)
}
