package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

type Destination struct {
	ID          primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	Name        string             `bson:"name" json:"name"`
	Country     string             `bson:"country" json:"country"`
	CountryCode string             `bson:"country_code" json:"country_code"`
	Region      string             `bson:"region" json:"region"`
	Description string             `bson:"description" json:"description"`
	ImageURL    string             `bson:"image_url" json:"image_url"`
	Images      []string           `bson:"images" json:"images"`

	// Location
	Latitude    float64 `bson:"latitude" json:"latitude"`
	Longitude   float64 `bson:"longitude" json:"longitude"`
	AirportCode string  `bson:"airport_code" json:"airport_code"`

	// Categorization
	Tags       []string `bson:"tags" json:"tags"`
	Categories []string `bson:"categories" json:"categories"` // beach, mountain, city, etc.
	BestFor    []string `bson:"best_for" json:"best_for"`     // couples, families, solo, etc.

	// Seasonal info
	BestMonths  []int   `bson:"best_months" json:"best_months"` // 1-12
	Climate     string  `bson:"climate" json:"climate"`
	AvgTempHigh float64 `bson:"avg_temp_high" json:"avg_temp_high"`
	AvgTempLow  float64 `bson:"avg_temp_low" json:"avg_temp_low"`

	// Popularity & pricing
	PopularityScore int     `bson:"popularity_score" json:"popularity_score"`
	AvgDailyBudget  float64 `bson:"avg_daily_budget" json:"avg_daily_budget"`
	Currency        string  `bson:"currency" json:"currency"`

	// Stats
	ListingsCount int `bson:"listings_count" json:"listings_count"`
	HotelsCount   int `bson:"hotels_count" json:"hotels_count"`

	// Highlights
	Highlights []DestinationHighlight `bson:"highlights" json:"highlights"`

	// Metadata
	Featured  bool      `bson:"featured" json:"featured"`
	Active    bool      `bson:"active" json:"active"`
	CreatedAt time.Time `bson:"created_at" json:"created_at"`
	UpdatedAt time.Time `bson:"updated_at" json:"updated_at"`
}

type DestinationHighlight struct {
	Title       string `bson:"title" json:"title"`
	Description string `bson:"description" json:"description"`
	Icon        string `bson:"icon" json:"icon"`
}

type DestinationFilter struct {
	Category  string
	Region    string
	Country   string
	BestFor   string
	Month     int
	MinBudget float64
	MaxBudget float64
	Featured  *bool
	Search    string
	SortBy    string // popularity, name, budget_asc, budget_desc
	Limit     int
	Offset    int
}
