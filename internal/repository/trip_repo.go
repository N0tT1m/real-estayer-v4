package repository

import (
	"context"
	"errors"
	"time"

	"github.com/realestayer/v3/internal/database"
	"github.com/realestayer/v3/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var ErrTripNotFound = errors.New("trip not found")

// TripRepository handles trip data operations
type TripRepository struct {
	collection *mongo.Collection
}

// NewTripRepository creates a new trip repository
func NewTripRepository(db *database.DB) *TripRepository {
	return &TripRepository{
		collection: db.Collection("trips"),
	}
}

// Create inserts a new trip
func (r *TripRepository) Create(ctx context.Context, trip *models.Trip) error {
	trip.ID = primitive.NewObjectID()
	trip.CreatedAt = time.Now()
	trip.UpdatedAt = time.Now()

	if trip.Status == "" {
		trip.Status = models.TripStatusPlanning
	}
	if trip.Items == nil {
		trip.Items = []models.TripItem{}
	}
	if trip.Destinations == nil {
		trip.Destinations = []models.TripDestination{}
	}

	_, err := r.collection.InsertOne(ctx, trip)
	return err
}

// FindByID finds a trip by ID
func (r *TripRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*models.Trip, error) {
	var trip models.Trip
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&trip)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrTripNotFound
	}
	return &trip, err
}

// FindByUserID finds all trips for a user
func (r *TripRepository) FindByUserID(ctx context.Context, userID primitive.ObjectID, page, limit int) ([]models.Trip, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	// User can see their own trips or trips shared with them
	filter := bson.M{
		"$or": []bson.M{
			{"user_id": userID},
			{"shared_with": userID},
		},
	}

	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	skip := (page - 1) * limit
	opts := options.Find().
		SetSkip(int64(skip)).
		SetLimit(int64(limit)).
		SetSort(bson.M{"start_date": -1})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer cursor.Close(ctx)

	var trips []models.Trip
	if err := cursor.All(ctx, &trips); err != nil {
		return nil, 0, err
	}

	return trips, total, nil
}

// Update updates a trip
func (r *TripRepository) Update(ctx context.Context, trip *models.Trip) error {
	trip.UpdatedAt = time.Now()
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"_id": trip.ID},
		bson.M{"$set": trip},
	)
	return err
}

// Delete removes a trip
func (r *TripRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

// AddItem adds an item to a trip
func (r *TripRepository) AddItem(ctx context.Context, tripID primitive.ObjectID, item models.TripItem) error {
	item.ID = primitive.NewObjectID()
	if item.Status == "" {
		item.Status = "planned"
	}

	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"_id": tripID},
		bson.M{
			"$push":  bson.M{"items": item},
			"$set":   bson.M{"updated_at": time.Now()},
		},
	)
	return err
}

// RemoveItem removes an item from a trip
func (r *TripRepository) RemoveItem(ctx context.Context, tripID, itemID primitive.ObjectID) error {
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"_id": tripID},
		bson.M{
			"$pull": bson.M{"items": bson.M{"id": itemID}},
			"$set":  bson.M{"updated_at": time.Now()},
		},
	)
	return err
}

// UpdateItemStatus updates the status of a trip item
func (r *TripRepository) UpdateItemStatus(ctx context.Context, tripID, itemID primitive.ObjectID, status string) error {
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"_id": tripID, "items.id": itemID},
		bson.M{
			"$set": bson.M{
				"items.$.status": status,
				"updated_at":     time.Now(),
			},
		},
	)
	return err
}

// ShareWithUser adds a user to the shared list
func (r *TripRepository) ShareWithUser(ctx context.Context, tripID, userID primitive.ObjectID) error {
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"_id": tripID},
		bson.M{
			"$addToSet": bson.M{"shared_with": userID},
			"$set":      bson.M{"updated_at": time.Now()},
		},
	)
	return err
}

// UnshareWithUser removes a user from the shared list
func (r *TripRepository) UnshareWithUser(ctx context.Context, tripID, userID primitive.ObjectID) error {
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"_id": tripID},
		bson.M{
			"$pull": bson.M{"shared_with": userID},
			"$set":  bson.M{"updated_at": time.Now()},
		},
	)
	return err
}

// GetUpcoming returns upcoming trips for a user
func (r *TripRepository) GetUpcoming(ctx context.Context, userID primitive.ObjectID, limit int) ([]models.Trip, error) {
	filter := bson.M{
		"$or": []bson.M{
			{"user_id": userID},
			{"shared_with": userID},
		},
		"start_date": bson.M{"$gte": time.Now()},
		"status":     bson.M{"$ne": models.TripStatusCancelled},
	}

	opts := options.Find().
		SetLimit(int64(limit)).
		SetSort(bson.M{"start_date": 1})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var trips []models.Trip
	if err := cursor.All(ctx, &trips); err != nil {
		return nil, err
	}

	return trips, nil
}
