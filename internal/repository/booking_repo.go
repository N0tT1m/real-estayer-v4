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

var ErrBookingNotFound = errors.New("booking not found")

// BookingRepository handles booking data operations
type BookingRepository struct {
	collection *mongo.Collection
}

// NewBookingRepository creates a new booking repository
func NewBookingRepository(db *database.DB) *BookingRepository {
	return &BookingRepository{
		collection: db.Collection("bookings"),
	}
}

// Create inserts a new booking
func (r *BookingRepository) Create(ctx context.Context, booking *models.Booking) error {
	booking.ID = primitive.NewObjectID()
	booking.CreatedAt = time.Now()
	booking.UpdatedAt = time.Now()

	_, err := r.collection.InsertOne(ctx, booking)
	return err
}

// FindByID finds a booking by ID
func (r *BookingRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*models.Booking, error) {
	var booking models.Booking
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&booking)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrBookingNotFound
	}
	return &booking, err
}

// FindByReference finds a booking by provider reference
func (r *BookingRepository) FindByReference(ctx context.Context, reference string) (*models.Booking, error) {
	var booking models.Booking
	err := r.collection.FindOne(ctx, bson.M{"provider_reference": reference}).Decode(&booking)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrBookingNotFound
	}
	return &booking, err
}

// FindByUserID finds all bookings for a user
func (r *BookingRepository) FindByUserID(ctx context.Context, userID primitive.ObjectID, page, limit int) ([]models.Booking, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}

	filter := bson.M{"user_id": userID}

	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, 0, err
	}

	skip := (page - 1) * limit
	opts := options.Find().
		SetSkip(int64(skip)).
		SetLimit(int64(limit)).
		SetSort(bson.M{"created_at": -1})

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, 0, err
	}
	defer func() { _ = cursor.Close(ctx) }()

	var bookings []models.Booking
	if err := cursor.All(ctx, &bookings); err != nil {
		return nil, 0, err
	}

	return bookings, total, nil
}

// FindByTripID finds all bookings for a trip
func (r *BookingRepository) FindByTripID(ctx context.Context, tripID primitive.ObjectID) ([]models.Booking, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"trip_id": tripID})
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()

	var bookings []models.Booking
	if err := cursor.All(ctx, &bookings); err != nil {
		return nil, err
	}

	return bookings, nil
}

// Update updates a booking
func (r *BookingRepository) Update(ctx context.Context, booking *models.Booking) error {
	booking.UpdatedAt = time.Now()
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"_id": booking.ID},
		bson.M{"$set": booking},
	)
	return err
}

// UpdateStatus updates booking status
func (r *BookingRepository) UpdateStatus(ctx context.Context, id primitive.ObjectID, status models.BookingStatus) error {
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"_id": id},
		bson.M{
			"$set": bson.M{
				"status":     status,
				"updated_at": time.Now(),
			},
		},
	)
	return err
}

// CountByType counts bookings by type for a user
func (r *BookingRepository) CountByType(ctx context.Context, userID primitive.ObjectID) (map[models.BookingType]int64, error) {
	pipeline := []bson.M{
		{"$match": bson.M{"user_id": userID}},
		{"$group": bson.M{
			"_id":   "$type",
			"count": bson.M{"$sum": 1},
		}},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()

	var results []struct {
		ID    models.BookingType `bson:"_id"`
		Count int64              `bson:"count"`
	}
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	counts := make(map[models.BookingType]int64)
	for _, r := range results {
		counts[r.ID] = r.Count
	}

	return counts, nil
}

// GetStats returns booking statistics
func (r *BookingRepository) GetStats(ctx context.Context) (map[string]interface{}, error) {
	total, err := r.collection.CountDocuments(ctx, bson.M{})
	if err != nil {
		return nil, err
	}

	confirmed, err := r.collection.CountDocuments(ctx, bson.M{"status": models.BookingStatusConfirmed})
	if err != nil {
		return nil, err
	}

	pipeline := []bson.M{
		{"$group": bson.M{
			"_id":   "$type",
			"count": bson.M{"$sum": 1},
		}},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()

	var typeResults []struct {
		ID    string `bson:"_id"`
		Count int64  `bson:"count"`
	}
	if err := cursor.All(ctx, &typeResults); err != nil {
		return nil, err
	}

	byType := make(map[string]int64)
	for _, r := range typeResults {
		byType[r.ID] = r.Count
	}

	return map[string]interface{}{
		"total":     total,
		"confirmed": confirmed,
		"by_type":   byType,
	}, nil
}
