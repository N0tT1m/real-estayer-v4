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
	"go.mongodb.org/mongo-driver/mongo/options"
)

var ErrSavedSearchNotFound = errors.New("saved search not found")

type SavedSearchRepository struct {
	collection *mongo.Collection
}

func NewSavedSearchRepository(db *database.DB) *SavedSearchRepository {
	return &SavedSearchRepository{collection: db.Collection("saved_searches")}
}

func (r *SavedSearchRepository) Create(ctx context.Context, s *models.SavedSearch) error {
	s.ID = primitive.NewObjectID()
	s.CreatedAt = time.Now()
	s.UpdatedAt = s.CreatedAt
	_, err := r.collection.InsertOne(ctx, s)
	return err
}

func (r *SavedSearchRepository) ListByUser(ctx context.Context, userID primitive.ObjectID) ([]models.SavedSearch, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"user_id": userID}, options.Find().SetSort(bson.M{"created_at": -1}))
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()
	var out []models.SavedSearch
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListDue returns searches that should be re-run (never run, or last run
// before `before`). Caller orchestrates batching.
func (r *SavedSearchRepository) ListDue(ctx context.Context, before time.Time, limit int) ([]models.SavedSearch, error) {
	filter := bson.M{
		"$or": []bson.M{
			{"last_run_at": bson.M{"$exists": false}},
			{"last_run_at": nil},
			{"last_run_at": bson.M{"$lt": before}},
		},
	}
	opts := options.Find().SetLimit(int64(limit))
	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()
	var out []models.SavedSearch
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *SavedSearchRepository) Delete(ctx context.Context, userID, id primitive.ObjectID) error {
	res, err := r.collection.DeleteOne(ctx, bson.M{"_id": id, "user_id": userID})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrSavedSearchNotFound
	}
	return nil
}

func (r *SavedSearchRepository) UpdateAfterRun(ctx context.Context, id primitive.ObjectID, seenIDs []string) error {
	now := time.Now()
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{
		"$set": bson.M{
			"last_run_at":   now,
			"last_seen_ids": seenIDs,
			"updated_at":    now,
		},
	})
	return err
}
