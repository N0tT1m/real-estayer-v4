package migrations

import (
	"context"
	"log/slog"
	"reflect"

	"github.com/realestayer/v4/internal/database"
	"github.com/realestayer/v4/internal/listing"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func init() {
	Register(Migration{
		Version: 1,
		Name:    "normalize_listing_features",
		Apply:   normalizeListingFeatures,
	})
}

// normalizeListingFeatures walks the listings collection and rewrites the
// features array through listing.NormalizeFeatures. Idempotent — already
// normalized docs skip the write. Was previously the body of
// cmd/migrate/main.go, preserved here so future migrations have a sibling.
func normalizeListingFeatures(ctx context.Context, db *database.DB) error {
	type listingDoc struct {
		ID       primitive.ObjectID `bson:"_id"`
		Features []string           `bson:"features"`
	}

	coll := db.Collection("listings")
	cursor, err := coll.Find(ctx, bson.M{"features": bson.M{"$exists": true, "$ne": []string{}}})
	if err != nil {
		return err
	}
	defer func() { _ = cursor.Close(ctx) }()

	var scanned, changed int
	for cursor.Next(ctx) {
		var doc listingDoc
		if err := cursor.Decode(&doc); err != nil {
			slog.Warn("normalize_features: decode failed", "error", err)
			continue
		}
		scanned++
		normalized := listing.NormalizeFeatures(doc.Features)
		if reflect.DeepEqual(doc.Features, normalized) {
			continue
		}
		changed++
		if _, err := coll.UpdateOne(ctx, bson.M{"_id": doc.ID}, bson.M{"$set": bson.M{"features": normalized}}); err != nil {
			slog.Warn("normalize_features: update failed", "id", doc.ID.Hex(), "error", err)
		}
	}
	slog.Info("normalize_features done", "scanned", scanned, "changed", changed)
	return cursor.Err()
}
