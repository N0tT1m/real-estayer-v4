package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// ListingPricePoint is a single snapshot of a listing's price at a point in time.
// Kept in its own collection so watchlist sparklines and "lowest ever" queries
// stay O(n) without touching the listing document.
//
// Separate from the older embedded `PricePoint` in watchlist.go, which stores
// per-watchlist-item tracking data. This type is indexed by listing instead.
type ListingPricePoint struct {
	ID         primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	ListingID  primitive.ObjectID `bson:"listing_id"    json:"listing_id"`
	Price      float64            `bson:"price"         json:"price"`
	Currency   string             `bson:"currency,omitempty" json:"currency,omitempty"`
	CapturedAt time.Time          `bson:"captured_at"   json:"captured_at"`
}
