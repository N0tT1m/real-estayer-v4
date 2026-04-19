package service

import (
	"context"
	"errors"

	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// WatchlistService handles watchlist-related business logic
type WatchlistService struct {
	watchlistRepo *repository.WatchlistRepository
	listingRepo   *repository.ListingRepository
}

// NewWatchlistService creates a new watchlist service
func NewWatchlistService(watchlistRepo *repository.WatchlistRepository) *WatchlistService {
	return &WatchlistService{
		watchlistRepo: watchlistRepo,
	}
}

// Add adds a listing to the user's watchlist
func (s *WatchlistService) Add(ctx context.Context, userID string, req models.AddToWatchlistRequest) (*models.WatchlistItem, error) {
	userObjID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errors.New("invalid user ID")
	}

	listingObjID, err := primitive.ObjectIDFromHex(req.ListingID)
	if err != nil {
		return nil, errors.New("invalid listing ID")
	}

	item := &models.WatchlistItem{
		UserID:       userObjID,
		ListingID:    listingObjID,
		TargetPrice:  req.TargetPrice,
		Notes:        req.Notes,
		PriceHistory: []models.PricePoint{},
		IsActive:     true,
	}

	if err := s.watchlistRepo.Create(ctx, item); err != nil {
		if errors.Is(err, repository.ErrWatchlistItemExists) {
			return nil, errors.New("listing already in watchlist")
		}
		return nil, err
	}

	return item, nil
}

// Remove removes a listing from the user's watchlist
func (s *WatchlistService) Remove(ctx context.Context, userID, itemID string) error {
	objID, err := primitive.ObjectIDFromHex(itemID)
	if err != nil {
		return errors.New("invalid watchlist item ID")
	}

	// Verify ownership
	item, err := s.watchlistRepo.FindByID(ctx, objID)
	if err != nil {
		return err
	}

	if item.UserID.Hex() != userID {
		return errors.New("access denied")
	}

	return s.watchlistRepo.Delete(ctx, objID)
}

// GetUserWatchlist returns all watchlist items for a user
func (s *WatchlistService) GetUserWatchlist(ctx context.Context, userID string) ([]models.WatchlistItem, error) {
	objID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errors.New("invalid user ID")
	}

	return s.watchlistRepo.FindByUserID(ctx, objID)
}

// GetWithListings returns watchlist items with their associated listings
func (s *WatchlistService) GetWithListings(ctx context.Context, userID string) ([]models.WatchlistItemWithListing, error) {
	objID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errors.New("invalid user ID")
	}

	return s.watchlistRepo.FindWithListings(ctx, objID)
}

// Update updates a watchlist item
func (s *WatchlistService) Update(ctx context.Context, userID, itemID string, req models.UpdateWatchlistRequest) (*models.WatchlistItem, error) {
	objID, err := primitive.ObjectIDFromHex(itemID)
	if err != nil {
		return nil, errors.New("invalid watchlist item ID")
	}

	item, err := s.watchlistRepo.FindByID(ctx, objID)
	if err != nil {
		return nil, err
	}

	if item.UserID.Hex() != userID {
		return nil, errors.New("access denied")
	}

	if req.TargetPrice != nil {
		item.TargetPrice = *req.TargetPrice
	}
	if req.Notes != nil {
		item.Notes = *req.Notes
	}
	if req.IsActive != nil {
		item.IsActive = *req.IsActive
	}

	if err := s.watchlistRepo.Update(ctx, item); err != nil {
		return nil, err
	}

	return item, nil
}

// GetForUser returns a watchlist item by ID scoped to a user. Returns an
// access-denied error if the item belongs to someone else.
func (s *WatchlistService) GetForUser(ctx context.Context, userID string, itemID primitive.ObjectID) (*models.WatchlistItem, error) {
	item, err := s.watchlistRepo.FindByID(ctx, itemID)
	if err != nil {
		return nil, err
	}
	if item.UserID.Hex() != userID {
		return nil, errors.New("access denied")
	}
	return item, nil
}

// GetStats returns watchlist statistics for a user
func (s *WatchlistService) GetStats(ctx context.Context, userID string) (*models.WatchlistStats, error) {
	objID, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errors.New("invalid user ID")
	}

	return s.watchlistRepo.GetStats(ctx, objID)
}

// CheckPrices checks prices for all active watchlist items (background job)
func (s *WatchlistService) CheckPrices(ctx context.Context) error {
	items, err := s.watchlistRepo.GetAllActive(ctx)
	if err != nil {
		return err
	}

	for _, item := range items {
		// In a real implementation, you would:
		// 1. Fetch current price from scraper or listing
		// 2. Compare with target price
		// 3. Send alert if price dropped below target
		// 4. Update price history
		_ = item // placeholder
	}

	return nil
}
