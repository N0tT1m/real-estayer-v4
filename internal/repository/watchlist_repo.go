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

var ErrWatchlistItemNotFound = errors.New("watchlist item not found")
var ErrWatchlistItemExists = errors.New("listing already in watchlist")

// WatchlistRepository handles watchlist data operations
type WatchlistRepository struct {
	collection        *mongo.Collection
	listingCollection *mongo.Collection
}

// NewWatchlistRepository creates a new watchlist repository
func NewWatchlistRepository(db *database.DB) *WatchlistRepository {
	return &WatchlistRepository{
		collection:        db.Collection("watchlists"),
		listingCollection: db.Collection("listings"),
	}
}

// Create adds a listing to watchlist
func (r *WatchlistRepository) Create(ctx context.Context, item *models.WatchlistItem) error {
	item.ID = primitive.NewObjectID()
	item.CreatedAt = time.Now()
	item.UpdatedAt = time.Now()
	item.IsActive = true

	if item.PriceHistory == nil {
		item.PriceHistory = []models.PricePoint{}
	}

	_, err := r.collection.InsertOne(ctx, item)
	if mongo.IsDuplicateKeyError(err) {
		return ErrWatchlistItemExists
	}
	return err
}

// FindByID finds a watchlist item by ID
func (r *WatchlistRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*models.WatchlistItem, error) {
	var item models.WatchlistItem
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&item)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrWatchlistItemNotFound
	}
	return &item, err
}

// FindByUserID finds all watchlist items for a user
func (r *WatchlistRepository) FindByUserID(ctx context.Context, userID primitive.ObjectID) ([]models.WatchlistItem, error) {
	cursor, err := r.collection.Find(ctx, bson.M{
		"user_id":   userID,
		"is_active": true,
	}, options.Find().SetSort(bson.M{"created_at": -1}))
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()

	var items []models.WatchlistItem
	if err := cursor.All(ctx, &items); err != nil {
		return nil, err
	}

	return items, nil
}

// FindByUserAndListing finds a specific watchlist item
func (r *WatchlistRepository) FindByUserAndListing(ctx context.Context, userID, listingID primitive.ObjectID) (*models.WatchlistItem, error) {
	var item models.WatchlistItem
	err := r.collection.FindOne(ctx, bson.M{
		"user_id":    userID,
		"listing_id": listingID,
	}).Decode(&item)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrWatchlistItemNotFound
	}
	return &item, err
}

// FindWithListings finds watchlist items with their associated listings
func (r *WatchlistRepository) FindWithListings(ctx context.Context, userID primitive.ObjectID) ([]models.WatchlistItemWithListing, error) {
	pipeline := []bson.M{
		{"$match": bson.M{
			"user_id":   userID,
			"is_active": true,
		}},
		{"$lookup": bson.M{
			"from":         "listings",
			"localField":   "listing_id",
			"foreignField": "_id",
			"as":           "listing_data",
		}},
		{"$addFields": bson.M{
			"listing": bson.M{"$arrayElemAt": []interface{}{"$listing_data", 0}},
		}},
		{"$project": bson.M{
			"listing_data": 0,
		}},
		{"$sort": bson.M{"created_at": -1}},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()

	var items []models.WatchlistItemWithListing
	if err := cursor.All(ctx, &items); err != nil {
		return nil, err
	}

	return items, nil
}

// Update updates a watchlist item
func (r *WatchlistRepository) Update(ctx context.Context, item *models.WatchlistItem) error {
	item.UpdatedAt = time.Now()
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"_id": item.ID},
		bson.M{"$set": item},
	)
	return err
}

// Delete removes a watchlist item
func (r *WatchlistRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

// DeleteByUserAndListing removes a specific watchlist item
func (r *WatchlistRepository) DeleteByUserAndListing(ctx context.Context, userID, listingID primitive.ObjectID) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{
		"user_id":    userID,
		"listing_id": listingID,
	})
	return err
}

// AddPricePoint adds a price point to history
func (r *WatchlistRepository) AddPricePoint(ctx context.Context, id primitive.ObjectID, price float64) error {
	pricePoint := models.PricePoint{
		Price:      price,
		RecordedAt: time.Now(),
	}

	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"_id": id},
		bson.M{
			"$push": bson.M{"price_history": pricePoint},
			"$set":  bson.M{"updated_at": time.Now()},
		},
	)
	return err
}

// MarkAlertSent marks that an alert was sent
func (r *WatchlistRepository) MarkAlertSent(ctx context.Context, id primitive.ObjectID) error {
	_, err := r.collection.UpdateOne(
		ctx,
		bson.M{"_id": id},
		bson.M{
			"$set": bson.M{
				"alert_sent": true,
				"updated_at": time.Now(),
			},
		},
	)
	return err
}

// GetStats returns watchlist statistics for a user
func (r *WatchlistRepository) GetStats(ctx context.Context, userID primitive.ObjectID) (*models.WatchlistStats, error) {
	items, err := r.FindByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	stats := &models.WatchlistStats{
		TotalItems: len(items),
	}

	for _, item := range items {
		if item.IsActive {
			stats.ActiveItems++
		}
		if item.AlertSent {
			stats.AlertsSent++
		}
	}

	return stats, nil
}

// GetAllActive returns all active watchlist items (for background price checking)
func (r *WatchlistRepository) GetAllActive(ctx context.Context) ([]models.WatchlistItem, error) {
	cursor, err := r.collection.Find(ctx, bson.M{"is_active": true})
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()

	var items []models.WatchlistItem
	if err := cursor.All(ctx, &items); err != nil {
		return nil, err
	}

	return items, nil
}
