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

// PriceHistoryRepository stores PricePoints. Append-only from the scraper's
// perspective; queries are always "give me the last N points for a listing".
type PriceHistoryRepository struct {
	collection *mongo.Collection
}

func NewPriceHistoryRepository(db *database.DB) *PriceHistoryRepository {
	return &PriceHistoryRepository{collection: db.Collection("price_history")}
}

// Record appends a price point. No deduplication — callers that want to avoid
// storing identical consecutive prices should check the most recent point
// first via Recent.
func (r *PriceHistoryRepository) Record(ctx context.Context, listingID primitive.ObjectID, price float64, currency string) error {
	_, err := r.collection.InsertOne(ctx, models.ListingPricePoint{
		ListingID:  listingID,
		Price:      price,
		Currency:   currency,
		CapturedAt: time.Now(),
	})
	return err
}

// Recent returns up to `limit` most recent price points, oldest first, so the
// caller can plot them left-to-right directly.
func (r *PriceHistoryRepository) Recent(ctx context.Context, listingID primitive.ObjectID, limit int) ([]models.ListingPricePoint, error) {
	if limit <= 0 {
		limit = 30
	}
	opts := options.Find().
		SetSort(bson.M{"captured_at": -1}).
		SetLimit(int64(limit))
	cursor, err := r.collection.Find(ctx, bson.M{"listing_id": listingID}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var points []models.ListingPricePoint
	if err := cursor.All(ctx, &points); err != nil {
		return nil, err
	}
	for i, j := 0, len(points)-1; i < j; i, j = i+1, j-1 {
		points[i], points[j] = points[j], points[i]
	}
	return points, nil
}

// RecentForListings returns price points for multiple listings at once, keyed
// by listing ID. Used by the watchlist page to fetch histories in one query.
func (r *PriceHistoryRepository) RecentForListings(ctx context.Context, listingIDs []primitive.ObjectID, limitPer int) (map[primitive.ObjectID][]models.ListingPricePoint, error) {
	result := make(map[primitive.ObjectID][]models.ListingPricePoint, len(listingIDs))
	for _, id := range listingIDs {
		pts, err := r.Recent(ctx, id, limitPer)
		if err != nil {
			return nil, err
		}
		result[id] = pts
	}
	return result, nil
}
