package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Collection is an editorially-curated set of POIs for a destination. Think
// "12 hours in Lisbon": a short list of places with a note and optional times
// a user can consume top-to-bottom.
type Collection struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Slug          string             `bson:"slug"          json:"slug"`
	Destination   string             `bson:"destination"   json:"destination"`
	Title         string             `bson:"title"         json:"title"`
	Subtitle      string             `bson:"subtitle,omitempty" json:"subtitle,omitempty"`
	Description   string             `bson:"description,omitempty" json:"description,omitempty"`
	CoverImage    string             `bson:"cover_image,omitempty" json:"cover_image,omitempty"`
	DurationHours int                `bson:"duration_hours" json:"duration_hours"`
	Tags          []string           `bson:"tags,omitempty" json:"tags,omitempty"`
	Stops         []CollectionStop   `bson:"stops"         json:"stops"`
	Featured      bool               `bson:"featured"      json:"featured"`
	CreatedAt     time.Time          `bson:"created_at"    json:"created_at"`
	UpdatedAt     time.Time          `bson:"updated_at"    json:"updated_at"`
}

// CollectionStop is one point along a collection itinerary.
type CollectionStop struct {
	Title       string  `bson:"title"                 json:"title"`
	Description string  `bson:"description,omitempty" json:"description,omitempty"`
	Category    string  `bson:"category,omitempty"    json:"category,omitempty"`
	DurationMin int     `bson:"duration_min,omitempty" json:"duration_min,omitempty"`
	Lat         float64 `bson:"lat,omitempty"         json:"lat,omitempty"`
	Lng         float64 `bson:"lng,omitempty"         json:"lng,omitempty"`
	URL         string  `bson:"url,omitempty"         json:"url,omitempty"`
}
