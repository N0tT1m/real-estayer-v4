package repository

import (
	"context"
	"errors"
	"time"

	"github.com/realestayer/v4/internal/database"
	"github.com/realestayer/v4/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var ErrCollectionNotFound = errors.New("collection not found")

// CollectionRepository persists editorial destination collections ("12 hours
// in Lisbon"). Self-contained so a single admin can seed the whole library.
type CollectionRepository struct {
	collection *mongo.Collection
}

func NewCollectionRepository(db *database.DB) *CollectionRepository {
	return &CollectionRepository{collection: db.Collection("collections")}
}

// UpsertBySlug replaces a collection matched by slug. Used by the seed file
// so re-running deploy-time seeds is safe.
func (r *CollectionRepository) UpsertBySlug(ctx context.Context, c *models.Collection) error {
	now := time.Now()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = now
	}
	c.UpdatedAt = now
	_, err := r.collection.UpdateOne(ctx,
		bson.M{"slug": c.Slug},
		bson.M{"$set": c},
		mongoUpsert(),
	)
	return err
}

func (r *CollectionRepository) FindBySlug(ctx context.Context, slug string) (*models.Collection, error) {
	var out models.Collection
	err := r.collection.FindOne(ctx, bson.M{"slug": slug}).Decode(&out)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrCollectionNotFound
	}
	return &out, err
}

// ListForDestination returns collections scoped to a given destination name
// (case-insensitive exact match) plus any collection flagged as featured.
func (r *CollectionRepository) ListForDestination(ctx context.Context, destination string) ([]models.Collection, error) {
	filter := bson.M{"$or": []bson.M{
		{"destination": bson.M{"$regex": "^" + destination + "$", "$options": "i"}},
		{"featured": true},
	}}
	opts := options.Find().SetSort(bson.M{"featured": -1, "updated_at": -1}).SetLimit(12)
	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()
	var out []models.Collection
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ListFeatured returns the home-page / explore curated rail.
func (r *CollectionRepository) ListFeatured(ctx context.Context, limit int) ([]models.Collection, error) {
	if limit <= 0 {
		limit = 8
	}
	opts := options.Find().SetSort(bson.M{"updated_at": -1}).SetLimit(int64(limit))
	cursor, err := r.collection.Find(ctx, bson.M{"featured": true}, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()
	var out []models.Collection
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}
