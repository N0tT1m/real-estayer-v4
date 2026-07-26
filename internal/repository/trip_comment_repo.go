package repository

import (
	"context"
	"time"

	"github.com/realestayer/v4/internal/database"
	"github.com/realestayer/v4/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// TripCommentRepository persists comments threaded by trip, optionally scoped
// to a single trip item.
type TripCommentRepository struct {
	collection *mongo.Collection
}

func NewTripCommentRepository(db *database.DB) *TripCommentRepository {
	return &TripCommentRepository{collection: db.Collection("trip_comments")}
}

func (r *TripCommentRepository) Create(ctx context.Context, c *models.TripComment) error {
	c.ID = primitive.NewObjectID()
	c.CreatedAt = time.Now()
	_, err := r.collection.InsertOne(ctx, c)
	return err
}

func (r *TripCommentRepository) ListForTrip(ctx context.Context, tripID primitive.ObjectID) ([]models.TripComment, error) {
	opts := options.Find().SetSort(bson.M{"created_at": 1})
	cursor, err := r.collection.Find(ctx, bson.M{"trip_id": tripID}, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()
	var out []models.TripComment
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *TripCommentRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (r *TripCommentRepository) DeleteForTrip(ctx context.Context, tripID primitive.ObjectID) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"trip_id": tripID})
	return err
}
