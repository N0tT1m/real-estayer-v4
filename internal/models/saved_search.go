package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// SavedSearch stores a user's filter set so a background worker can re-run it
// and notify when new matches appear.
type SavedSearch struct {
	ID            primitive.ObjectID `bson:"_id,omitempty"        json:"id"`
	UserID        primitive.ObjectID `bson:"user_id"              json:"user_id"`
	Name          string             `bson:"name"                 json:"name"`
	Query         SavedSearchQuery   `bson:"query"                json:"query"`
	NotifyDiscord bool               `bson:"notify_discord"       json:"notify_discord"`
	LastRunAt     *time.Time         `bson:"last_run_at,omitempty" json:"last_run_at,omitempty"`
	LastSeenIDs   []string           `bson:"last_seen_ids,omitempty" json:"last_seen_ids,omitempty"`
	CreatedAt     time.Time          `bson:"created_at"           json:"created_at"`
	UpdatedAt     time.Time          `bson:"updated_at"           json:"updated_at"`
}

// SavedSearchQuery captures the subset of listing filters worth alerting on.
// Kept deliberately small — complex filters should be revisited by the user.
type SavedSearchQuery struct {
	Location string   `bson:"location,omitempty" json:"location,omitempty"`
	Region   string   `bson:"region,omitempty"   json:"region,omitempty"`
	Country  string   `bson:"country,omitempty"  json:"country,omitempty"`
	Features []string `bson:"features,omitempty" json:"features,omitempty"`
	MaxPrice float64  `bson:"max_price,omitempty" json:"max_price,omitempty"`
}
