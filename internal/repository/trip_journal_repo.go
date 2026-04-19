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

type TripJournalRepository struct {
	collection *mongo.Collection
}

func NewTripJournalRepository(db *database.DB) *TripJournalRepository {
	return &TripJournalRepository{collection: db.Collection("trip_journal")}
}

func (r *TripJournalRepository) Create(ctx context.Context, e *models.JournalEntry) error {
	e.ID = primitive.NewObjectID()
	e.CreatedAt = time.Now()
	if e.EntryDate.IsZero() {
		e.EntryDate = e.CreatedAt
	}
	_, err := r.collection.InsertOne(ctx, e)
	return err
}

func (r *TripJournalRepository) ListForTrip(ctx context.Context, tripID primitive.ObjectID) ([]models.JournalEntry, error) {
	opts := options.Find().SetSort(bson.M{"entry_date": -1})
	cursor, err := r.collection.Find(ctx, bson.M{"trip_id": tripID}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	var out []models.JournalEntry
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *TripJournalRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (r *TripJournalRepository) DeleteForTrip(ctx context.Context, tripID primitive.ObjectID) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"trip_id": tripID})
	return err
}
