package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Host represents the listing host information
type Host struct {
	Name        string `bson:"name,omitempty" json:"name,omitempty"`
	IsSuperhost bool   `bson:"is_superhost,omitempty" json:"is_superhost,omitempty"`
	ImageURL    string `bson:"image_url,omitempty" json:"image_url,omitempty"`
}

// Listing represents an Airbnb property listing (from Rust scraper)
type Listing struct {
	ID            primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	URL           string             `bson:"url" json:"url"`
	Title         string             `bson:"title" json:"title"`
	PictureURL    string             `bson:"picture_url" json:"picture_url"`
	Pictures      []string           `bson:"pictures,omitempty" json:"pictures,omitempty"`
	Description   string             `bson:"description" json:"description"`
	Price         string             `bson:"price" json:"price"`
	PriceNumeric  float64            `bson:"price_numeric,omitempty" json:"price_numeric,omitempty"`
	Rating        string             `bson:"rating" json:"rating"`
	RatingNumeric float64            `bson:"rating_numeric,omitempty" json:"rating_numeric,omitempty"`
	ReviewsCount  int                `bson:"reviews_count,omitempty" json:"reviews_count,omitempty"`
	Location      string             `bson:"location" json:"location"`
	Coordinates   *Coordinates       `bson:"coordinates,omitempty" json:"coordinates,omitempty"`
	Features      []string           `bson:"features" json:"features"`
	// Amenities is the canonical, filterable facet set derived from Features.
	// Features stays as scraped: re-deriving needs the source strings, and the
	// previous in-place normalisation destroyed them.
	Amenities []string `bson:"amenities,omitempty" json:"amenities,omitempty"`
	// Badges are marketing labels (Superhost, Guest Favorite), not properties
	// of the place, so they are kept out of the amenity facets.
	Badges []string `bson:"badges,omitempty" json:"badges,omitempty"`
	// Structured room facts recovered from Features and Description. Zero means
	// unknown, never "none".
	Bedrooms     int       `bson:"bedrooms,omitempty" json:"bedrooms,omitempty"`
	Bathrooms    float64   `bson:"bathrooms,omitempty" json:"bathrooms,omitempty"`
	Beds         int       `bson:"beds,omitempty" json:"beds,omitempty"`
	Sleeps       int       `bson:"sleeps,omitempty" json:"sleeps,omitempty"`
	HouseDetails []string  `bson:"house_details" json:"house_details"`
	Host         *Host     `bson:"host,omitempty" json:"host,omitempty"`
	Region       string    `bson:"region,omitempty" json:"region,omitempty"`
	Country      string    `bson:"country,omitempty" json:"country,omitempty"`
	PropertyType string    `bson:"property_type,omitempty" json:"property_type,omitempty"`
	CreatedAt    time.Time `bson:"created_at,omitempty" json:"created_at,omitempty"`
	ScrapedAt    time.Time `bson:"scraped_at,omitempty" json:"scraped_at,omitempty"`
}

// Coordinates represents geographic coordinates
type Coordinates struct {
	Lat float64 `bson:"lat" json:"lat"`
	Lng float64 `bson:"lng" json:"lng"`
}

// ListingSearchParams holds search/filter parameters
type ListingSearchParams struct {
	Query    string `json:"query"`
	Location string `json:"location"`
	Region   string `json:"region"`
	Country  string `json:"country"`
	// City matches the first comma-separated segment of the listing's raw
	// location string — set by the /listings location-type toggle ("city"
	// mode). Region covers state/province since both live in the same bson
	// field; the toggle just constrains which values the dropdown offers.
	City      string   `json:"city"`
	MinPrice  float64  `json:"min_price"`
	MaxPrice  float64  `json:"max_price"`
	MinRating float64  `json:"min_rating"`
	Features  []string `json:"features"`
	// Amenities filters on the canonical facet set; Features remains for
	// backwards compatibility with existing saved searches.
	Amenities    []string `json:"amenities"`
	MinBedrooms  int      `json:"min_bedrooms"`
	MinSleeps    int      `json:"min_sleeps"`
	PropertyType string   `json:"property_type"`
	SortBy       string   `json:"sort_by"` // price_asc, price_desc, rating_desc, newest
	Page         int      `json:"page"`
	Limit        int      `json:"limit"`
}

// ListingSearchResult is paginated search results
type ListingSearchResult struct {
	Listings   []Listing `json:"listings"`
	Total      int64     `json:"total"`
	Page       int       `json:"page"`
	Limit      int       `json:"limit"`
	TotalPages int       `json:"total_pages"`
}

// DefaultListingSearchParams returns sensible defaults
func DefaultListingSearchParams() ListingSearchParams {
	return ListingSearchParams{
		Page:  1,
		Limit: 20,
	}
}
