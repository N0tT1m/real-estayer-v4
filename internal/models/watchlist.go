package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// WatchlistItem represents a listing being watched for price changes
type WatchlistItem struct {
	ID           primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	UserID       primitive.ObjectID `bson:"user_id" json:"user_id"`
	ListingID    primitive.ObjectID `bson:"listing_id" json:"listing_id"`
	TargetPrice  float64            `bson:"target_price" json:"target_price"`
	Notes        string             `bson:"notes,omitempty" json:"notes,omitempty"`
	PriceHistory []PricePoint       `bson:"price_history" json:"price_history"`
	AlertSent    bool               `bson:"alert_sent" json:"alert_sent"`
	IsActive     bool               `bson:"is_active" json:"is_active"`
	CreatedAt    time.Time          `bson:"created_at" json:"created_at"`
	UpdatedAt    time.Time          `bson:"updated_at" json:"updated_at"`
}

// PricePoint represents a historical price record
type PricePoint struct {
	Price      float64   `bson:"price" json:"price"`
	RecordedAt time.Time `bson:"recorded_at" json:"recorded_at"`
}

// WatchlistItemWithListing includes the full listing data
type WatchlistItemWithListing struct {
	WatchlistItem `bson:",inline"`
	Listing       *Listing `bson:"listing,omitempty" json:"listing,omitempty"`
}

// AddToWatchlistRequest is the payload for adding to watchlist
type AddToWatchlistRequest struct {
	ListingID   string  `json:"listing_id" validate:"required"`
	TargetPrice float64 `json:"target_price" validate:"required,gt=0"`
	Notes       string  `json:"notes,omitempty"`
}

// UpdateWatchlistRequest is the payload for updating a watchlist item
type UpdateWatchlistRequest struct {
	TargetPrice *float64 `json:"target_price,omitempty"`
	Notes       *string  `json:"notes,omitempty"`
	IsActive    *bool    `json:"is_active,omitempty"`
}

// WatchlistStats provides summary statistics
type WatchlistStats struct {
	TotalItems    int     `json:"total_items"`
	ActiveItems   int     `json:"active_items"`
	AlertsSent    int     `json:"alerts_sent"`
	AvgSavings    float64 `json:"avg_savings"`
	TotalSavings  float64 `json:"total_savings"`
}
