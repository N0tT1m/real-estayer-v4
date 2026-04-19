package repository

import (
	"context"
	"errors"
	"time"

	"github.com/realestayer/v4/internal/database"
	"github.com/realestayer/v4/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

var ErrReviewNotFound = errors.New("review not found")

type TripReviewRepository struct {
	collection *mongo.Collection
}

func NewTripReviewRepository(db *database.DB) *TripReviewRepository {
	return &TripReviewRepository{collection: db.Collection("trip_reviews")}
}

// Upsert inserts or updates a review keyed by (user, trip, item) so each user
// has at most one review per item.
func (r *TripReviewRepository) Upsert(ctx context.Context, rev *models.ItemReview) error {
	filter := bson.M{"user_id": rev.UserID, "trip_id": rev.TripID, "item_id": rev.ItemID}
	update := bson.M{
		"$set": bson.M{
			"rating": rev.Rating,
			"body":   rev.Body,
		},
		"$setOnInsert": bson.M{
			"_id":        primitive.NewObjectID(),
			"user_id":    rev.UserID,
			"trip_id":    rev.TripID,
			"item_id":    rev.ItemID,
			"created_at": time.Now(),
		},
	}
	_, err := r.collection.UpdateOne(ctx, filter, update, mongoUpsert())
	return err
}

// ListForTrip returns all reviews across items within a trip.
func (r *TripReviewRepository) ListForTrip(ctx context.Context, tripID primitive.ObjectID) ([]models.ItemReview, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"trip_id": tripID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var out []models.ItemReview
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *TripReviewRepository) DeleteForTrip(ctx context.Context, tripID primitive.ObjectID) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"trip_id": tripID})
	return err
}
