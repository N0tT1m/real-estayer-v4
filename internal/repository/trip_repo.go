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
	defer func() { _ = cursor.Close(ctx) }()

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
			"$push": bson.M{"items": item},
			"$set":  bson.M{"updated_at": time.Now()},
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

// AddCollaborator appends a collaborator entry (idempotent on user_id).
func (r *TripRepository) AddCollaborator(ctx context.Context, tripID primitive.ObjectID, c models.TripCollaborator) error {
	c.AddedAt = time.Now()
	// Remove any existing entry for that user first so role changes take effect.
	_, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": tripID},
		bson.M{"$pull": bson.M{"collaborators": bson.M{"user_id": c.UserID}}},
	)
	if err != nil {
		return err
	}
	_, err = r.collection.UpdateOne(ctx,
		bson.M{"_id": tripID},
		bson.M{
			"$push": bson.M{"collaborators": c},
			"$set":  bson.M{"updated_at": time.Now()},
		},
	)
	return err
}

// RemoveCollaborator drops a collaborator by user ID.
func (r *TripRepository) RemoveCollaborator(ctx context.Context, tripID, userID primitive.ObjectID) error {
	_, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": tripID},
		bson.M{
			"$pull": bson.M{"collaborators": bson.M{"user_id": userID}},
			"$set":  bson.M{"updated_at": time.Now()},
		},
	)
	return err
}

// ReorderItems replaces the items array with the provided slice, preserving
// item IDs. Caller is responsible for ensuring the slice contains the same
// set of items (in whatever new order).
func (r *TripRepository) ReorderItems(ctx context.Context, tripID primitive.ObjectID, items []models.TripItem) error {
	_, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": tripID},
		bson.M{
			"$set": bson.M{
				"items":      items,
				"updated_at": time.Now(),
			},
		},
	)
	return err
}

// SetPackingList replaces the packing list wholesale — simpler than doing
// array surgery over websockets.
func (r *TripRepository) SetPackingList(ctx context.Context, tripID primitive.ObjectID, items []models.PackingItem) error {
	_, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": tripID},
		bson.M{"$set": bson.M{"packing_list": items, "updated_at": time.Now()}},
	)
	return err
}

// SetChecklist replaces the checklist wholesale.
func (r *TripRepository) SetChecklist(ctx context.Context, tripID primitive.ObjectID, items []models.ChecklistItem) error {
	_, err := r.collection.UpdateOne(ctx,
		bson.M{"_id": tripID},
		bson.M{"$set": bson.M{"checklist": items, "updated_at": time.Now()}},
	)
	return err
}

// FindBySlug returns a trip by its public share slug, or ErrTripNotFound.
func (r *TripRepository) FindBySlug(ctx context.Context, slug string) (*models.Trip, error) {
	if slug == "" {
		return nil, ErrTripNotFound
	}
	var trip models.Trip
	err := r.collection.FindOne(ctx, bson.M{"share_slug": slug}).Decode(&trip)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrTripNotFound
	}
	return &trip, err
}

// SetShareSlug writes or clears the slug. Callers generate the slug themselves.
func (r *TripRepository) SetShareSlug(ctx context.Context, tripID primitive.ObjectID, slug string) error {
	update := bson.M{"$set": bson.M{"updated_at": time.Now()}}
	if slug == "" {
		update["$unset"] = bson.M{"share_slug": ""}
	} else {
		update["$set"].(bson.M)["share_slug"] = slug
	}
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": tripID}, update)
	return err
}

// FindUpcomingAll returns all future trips across every user, ordered by
// start date ascending. Used by the notification worker.
func (r *TripRepository) FindUpcomingAll(ctx context.Context, from time.Time, limit int) ([]models.Trip, error) {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	filter := bson.M{
		"start_date": bson.M{"$gte": from.Add(-24 * time.Hour)}, // include today
		"status":     bson.M{"$ne": models.TripStatusCancelled},
	}
	opts := options.Find().SetSort(bson.M{"start_date": 1}).SetLimit(int64(limit))
	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()
	var out []models.Trip
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
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
	defer func() { _ = cursor.Close(ctx) }()

	var trips []models.Trip
	if err := cursor.All(ctx, &trips); err != nil {
		return nil, err
	}

	return trips, nil
}
