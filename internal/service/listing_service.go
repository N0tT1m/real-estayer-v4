package service

import (
	"context"

	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ListingService handles listing-related business logic
type ListingService struct {
	listingRepo *repository.ListingRepository
}

// NewListingService creates a new listing service
func NewListingService(listingRepo *repository.ListingRepository) *ListingService {
	return &ListingService{
		listingRepo: listingRepo,
	}
}

// GetByID retrieves a listing by ID
func (s *ListingService) GetByID(ctx context.Context, id string) (*models.Listing, error) {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, repository.ErrListingNotFound
	}
	return s.listingRepo.FindByID(ctx, objID)
}

// Search finds listings based on parameters
func (s *ListingService) Search(ctx context.Context, params models.ListingSearchParams) (*models.ListingSearchResult, error) {
	return s.listingRepo.Search(ctx, params)
}

// GetFeatures returns all available features
func (s *ListingService) GetFeatures(ctx context.Context) ([]string, error) {
	return s.listingRepo.GetFeatures(ctx)
}

// GetRegions returns all available regions
func (s *ListingService) GetRegions(ctx context.Context) ([]string, error) {
	return s.listingRepo.GetRegions(ctx)
}

// GetCountries returns all available countries
func (s *ListingService) GetCountries(ctx context.Context) ([]string, error) {
	return s.listingRepo.GetCountries(ctx)
}

// GetStates returns distinct US regions across listings.
func (s *ListingService) GetStates(ctx context.Context) ([]string, error) {
	return s.listingRepo.GetStates(ctx)
}

// GetProvinces returns distinct Canadian regions across listings.
func (s *ListingService) GetProvinces(ctx context.Context) ([]string, error) {
	return s.listingRepo.GetProvinces(ctx)
}

// GetCities returns distinct city names parsed from listing locations.
func (s *ListingService) GetCities(ctx context.Context) ([]string, error) {
	return s.listingRepo.GetCities(ctx)
}

// GetStats returns listing statistics
func (s *ListingService) GetStats(ctx context.Context) (map[string]interface{}, error) {
	count, err := s.listingRepo.Count(ctx)
	if err != nil {
		return nil, err
	}

	regions, err := s.listingRepo.GetRegions(ctx)
	if err != nil {
		regions = []string{}
	}

	countries, err := s.listingRepo.GetCountries(ctx)
	if err != nil {
		countries = []string{}
	}

	return map[string]interface{}{
		"total":          count,
		"total_listings": count,
		"regions":        len(regions),
		"countries":      len(countries),
	}, nil
}

// Delete removes a listing by ID
func (s *ListingService) Delete(ctx context.Context, id string) error {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return repository.ErrListingNotFound
	}
	return s.listingRepo.Delete(ctx, objID)
}

// FilterForSavedSearch builds search params from a saved-search query.
func (s *ListingService) FilterForSavedSearch(q models.SavedSearchQuery) models.ListingSearchParams {
	return models.ListingSearchParams{
		Location: q.Location,
		Region:   q.Region,
		Country:  q.Country,
		Features: q.Features,
		MaxPrice: q.MaxPrice,
		Limit:    50,
	}
}

// FindMatching returns the first `limit` listings matching the params.
func (s *ListingService) FindMatching(ctx context.Context, params models.ListingSearchParams, limit int) ([]models.Listing, error) {
	if limit > 0 {
		params.Limit = limit
	}
	res, err := s.listingRepo.Search(ctx, params)
	if err != nil {
		return nil, err
	}
	return res.Listings, nil
}
