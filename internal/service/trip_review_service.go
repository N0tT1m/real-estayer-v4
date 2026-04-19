package service

import (
	"context"
	"errors"
	"strings"

	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type TripReviewService struct {
	trips   *TripService
	reviews *repository.TripReviewRepository
}

func NewTripReviewService(trips *TripService, reviews *repository.TripReviewRepository) *TripReviewService {
	return &TripReviewService{trips: trips, reviews: reviews}
}

// Upsert lets a user rate/review one item on a trip they have access to.
func (s *TripReviewService) Upsert(ctx context.Context, userID, tripID, itemID string, rating int, body string) (*models.ItemReview, error) {
	if rating < 1 || rating > 5 {
		return nil, errors.New("rating must be 1-5")
	}
	trip, err := s.trips.GetByID(ctx, userID, tripID)
	if err != nil {
		return nil, err
	}
	iid, err := primitive.ObjectIDFromHex(itemID)
	if err != nil {
		return nil, errors.New("invalid item id")
	}
	found := false
	for _, it := range trip.Items {
		if it.ID == iid {
			found = true
			break
		}
	}
	if !found {
		return nil, errors.New("item not found on this trip")
	}
	uid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errors.New("invalid user id")
	}
	rev := &models.ItemReview{
		TripID: trip.ID,
		ItemID: iid,
		UserID: uid,
		Rating: rating,
		Body:   strings.TrimSpace(body),
	}
	if err := s.reviews.Upsert(ctx, rev); err != nil {
		return nil, err
	}
	return rev, nil
}

// List returns every review on a trip, keyed by ItemID. Handy for a rollup
// view without a second round-trip per item.
func (s *TripReviewService) List(ctx context.Context, userID, tripID string) (map[primitive.ObjectID][]models.ItemReview, error) {
	trip, err := s.trips.GetByID(ctx, userID, tripID)
	if err != nil {
		return nil, err
	}
	all, err := s.reviews.ListForTrip(ctx, trip.ID)
	if err != nil {
		return nil, err
	}
	out := map[primitive.ObjectID][]models.ItemReview{}
	for _, r := range all {
		out[r.ItemID] = append(out[r.ItemID], r)
	}
	return out, nil
}
