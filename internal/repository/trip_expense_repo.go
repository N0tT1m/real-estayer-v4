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

type TripExpenseRepository struct {
	collection *mongo.Collection
}

func NewTripExpenseRepository(db *database.DB) *TripExpenseRepository {
	return &TripExpenseRepository{collection: db.Collection("trip_expenses")}
}

func (r *TripExpenseRepository) Create(ctx context.Context, e *models.TripExpense) error {
	e.ID = primitive.NewObjectID()
	e.CreatedAt = time.Now()
	if e.SpentAt.IsZero() {
		e.SpentAt = e.CreatedAt
	}
	_, err := r.collection.InsertOne(ctx, e)
	return err
}

func (r *TripExpenseRepository) ListForTrip(ctx context.Context, tripID primitive.ObjectID) ([]models.TripExpense, error) {
	opts := options.Find().SetSort(bson.M{"spent_at": -1})
	cursor, err := r.collection.Find(ctx, bson.M{"trip_id": tripID}, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()
	var out []models.TripExpense
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *TripExpenseRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (r *TripExpenseRepository) DeleteForTrip(ctx context.Context, tripID primitive.ObjectID) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"trip_id": tripID})
	return err
}
