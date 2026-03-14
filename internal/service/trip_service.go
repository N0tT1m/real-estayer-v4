package service

import (
	"context"
	"errors"

	"github.com/realestayer/v3/internal/models"
	"github.com/realestayer/v3/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

var ErrTripAccessDenied = errors.New("access to trip denied")

// TripService handles trip-related business logic
type TripService struct {
	tripRepo *repository.TripRepository
}

// NewTripService creates a new trip service
func NewTripService(tripRepo *repository.TripRepository) *TripService {
	return &TripService{
		tripRepo: tripRepo,
	}
}

// Create creates a new trip
func (s *TripService) Create(ctx context.Context, userID string, req models.CreateTripRequest) (*models.Trip, error) {
	objID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errors.New("invalid user ID")
	}

	trip := &models.Trip{
		UserID:       objID,
		Name:         req.Name,
		Description:  req.Description,
		Status:       models.TripStatusPlanning,
		StartDate:    req.StartDate,
		EndDate:      req.EndDate,
		Destinations: req.Destinations,
		Items:        []models.TripItem{},
		TotalBudget: models.TripBudget{
			Estimated: req.Budget,
			Currency:  req.Currency,
		},
	}

	if trip.TotalBudget.Currency == "" {
		trip.TotalBudget.Currency = "USD"
	}

	if err := s.tripRepo.Create(ctx, trip); err != nil {
		return nil, err
	}

	return trip, nil
}

// GetByID retrieves a trip by ID (with access check)
func (s *TripService) GetByID(ctx context.Context, userID, tripID string) (*models.Trip, error) {
	objID, err := primitive.ObjectIDFromHex(tripID)
	if err != nil {
		return nil, repository.ErrTripNotFound
	}

	trip, err := s.tripRepo.FindByID(ctx, objID)
	if err != nil {
		return nil, err
	}

	// Check access
	if !s.hasAccess(userID, trip) {
		return nil, ErrTripAccessDenied
	}

	return trip, nil
}

// GetUserTrips returns all trips for a user
func (s *TripService) GetUserTrips(ctx context.Context, userID string, page, limit int) ([]models.Trip, int64, error) {
	objID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, 0, errors.New("invalid user ID")
	}

	return s.tripRepo.FindByUserID(ctx, objID, page, limit)
}

// Update updates a trip
func (s *TripService) Update(ctx context.Context, userID, tripID string, req models.UpdateTripRequest) (*models.Trip, error) {
	trip, err := s.GetByID(ctx, userID, tripID)
	if err != nil {
		return nil, err
	}

	// Only owner can update
	if trip.UserID.Hex() != userID {
		return nil, ErrTripAccessDenied
	}

	if req.Name != "" {
		trip.Name = req.Name
	}
	if req.Description != "" {
		trip.Description = req.Description
	}
	if req.Status != "" {
		trip.Status = req.Status
	}
	if req.StartDate != nil {
		trip.StartDate = *req.StartDate
	}
	if req.EndDate != nil {
		trip.EndDate = *req.EndDate
	}
	if req.Destinations != nil {
		trip.Destinations = req.Destinations
	}
	if req.Budget != nil {
		trip.TotalBudget.Estimated = *req.Budget
	}

	if err := s.tripRepo.Update(ctx, trip); err != nil {
		return nil, err
	}

	return trip, nil
}

// Delete removes a trip
func (s *TripService) Delete(ctx context.Context, userID, tripID string) error {
	trip, err := s.GetByID(ctx, userID, tripID)
	if err != nil {
		return err
	}

	// Only owner can delete
	if trip.UserID.Hex() != userID {
		return ErrTripAccessDenied
	}

	return s.tripRepo.Delete(ctx, trip.ID)
}

// AddItem adds an item to a trip
func (s *TripService) AddItem(ctx context.Context, userID, tripID string, req models.AddTripItemRequest) (*models.Trip, error) {
	trip, err := s.GetByID(ctx, userID, tripID)
	if err != nil {
		return nil, err
	}

	item := models.TripItem{
		Type:        req.Type,
		ReferenceID: req.ReferenceID,
		Provider:    req.Provider,
		Title:       req.Title,
		Status:      "planned",
		Details:     req.Details,
		Price:       req.Price,
		StartTime:   req.StartTime,
		EndTime:     req.EndTime,
		Notes:       req.Notes,
	}

	if err := s.tripRepo.AddItem(ctx, trip.ID, item); err != nil {
		return nil, err
	}

	// Return updated trip
	return s.tripRepo.FindByID(ctx, trip.ID)
}

// RemoveItem removes an item from a trip
func (s *TripService) RemoveItem(ctx context.Context, userID, tripID, itemID string) error {
	trip, err := s.GetByID(ctx, userID, tripID)
	if err != nil {
		return err
	}

	itemObjID, err := primitive.ObjectIDFromHex(itemID)
	if err != nil {
		return errors.New("invalid item ID")
	}

	return s.tripRepo.RemoveItem(ctx, trip.ID, itemObjID)
}

// ShareWithUser shares a trip with another user
func (s *TripService) ShareWithUser(ctx context.Context, ownerID, tripID, shareWithUserID string) error {
	trip, err := s.GetByID(ctx, ownerID, tripID)
	if err != nil {
		return err
	}

	// Only owner can share
	if trip.UserID.Hex() != ownerID {
		return ErrTripAccessDenied
	}

	shareWithObjID, err := primitive.ObjectIDFromHex(shareWithUserID)
	if err != nil {
		return errors.New("invalid user ID")
	}

	return s.tripRepo.ShareWithUser(ctx, trip.ID, shareWithObjID)
}

// GetUpcoming returns upcoming trips for a user
func (s *TripService) GetUpcoming(ctx context.Context, userID string, limit int) ([]models.Trip, error) {
	objID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errors.New("invalid user ID")
	}

	return s.tripRepo.GetUpcoming(ctx, objID, limit)
}

// hasAccess checks if a user has access to a trip
func (s *TripService) hasAccess(userID string, trip *models.Trip) bool {
	if trip.UserID.Hex() == userID {
		return true
	}

	for _, sharedID := range trip.SharedWith {
		if sharedID.Hex() == userID {
			return true
		}
	}

	return false
}
