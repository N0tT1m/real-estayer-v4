package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TripStatus represents the status of a trip
type TripStatus string

const (
	TripStatusPlanning  TripStatus = "planning"
	TripStatusBooked    TripStatus = "booked"
	TripStatusCompleted TripStatus = "completed"
	TripStatusCancelled TripStatus = "cancelled"
)

// Trip represents a user's travel itinerary
type Trip struct {
	ID           primitive.ObjectID   `bson:"_id,omitempty" json:"id"`
	UserID       primitive.ObjectID   `bson:"user_id" json:"user_id"`
	Name         string               `bson:"name" json:"name"`
	Description  string               `bson:"description,omitempty" json:"description,omitempty"`
	Status       TripStatus           `bson:"status" json:"status"`
	StartDate    time.Time            `bson:"start_date" json:"start_date"`
	EndDate      time.Time            `bson:"end_date" json:"end_date"`
	Destinations []TripDestination    `bson:"destinations" json:"destinations"`
	Items        []TripItem           `bson:"items" json:"items"`
	TotalBudget  TripBudget           `bson:"total_budget" json:"total_budget"`
	SharedWith    []primitive.ObjectID `bson:"shared_with,omitempty"  json:"shared_with,omitempty"` // legacy, still honored
	Collaborators []TripCollaborator   `bson:"collaborators,omitempty" json:"collaborators,omitempty"`
	ShareSlug     string               `bson:"share_slug,omitempty"   json:"share_slug,omitempty"` // set when public sharing enabled
	CoverImage    string               `bson:"cover_image,omitempty"  json:"cover_image,omitempty"`
	PackingList   []PackingItem        `bson:"packing_list,omitempty" json:"packing_list,omitempty"`
	Checklist     []ChecklistItem      `bson:"checklist,omitempty"    json:"checklist,omitempty"`
	CreatedAt     time.Time            `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time            `bson:"updated_at" json:"updated_at"`
}

// TripDestination represents a stop in the trip
type TripDestination struct {
	Name          string       `bson:"name" json:"name"`
	ArrivalDate   time.Time    `bson:"arrival_date" json:"arrival_date"`
	DepartureDate time.Time    `bson:"departure_date" json:"departure_date"`
	Coordinates   *Coordinates `bson:"coordinates,omitempty" json:"coordinates,omitempty"`
	Notes         string       `bson:"notes,omitempty" json:"notes,omitempty"`
}

// TripItemType represents what kind of item is in the trip
type TripItemType string

const (
	TripItemTypeFlight   TripItemType = "flight"
	TripItemTypeHotel    TripItemType = "hotel"
	TripItemTypeCar      TripItemType = "car"
	TripItemTypeListing  TripItemType = "listing"
	TripItemTypeActivity TripItemType = "activity"
)

// TripItem represents a component of the trip (flight, hotel, car, listing, activity)
type TripItem struct {
	ID          primitive.ObjectID `bson:"id" json:"id"`
	Type        TripItemType       `bson:"type" json:"type"`
	ReferenceID string             `bson:"reference_id" json:"reference_id"` // booking ref or listing ID
	Provider    string             `bson:"provider" json:"provider"`
	Status      string             `bson:"status" json:"status"` // planned, booked, cancelled
	Title       string             `bson:"title" json:"title"`
	Details     map[string]any     `bson:"details" json:"details"` // flexible details
	Price       *TripItemPrice     `bson:"price,omitempty" json:"price,omitempty"`
	StartTime   *time.Time         `bson:"start_time,omitempty" json:"start_time,omitempty"`
	EndTime     *time.Time         `bson:"end_time,omitempty" json:"end_time,omitempty"`
	Notes       string             `bson:"notes,omitempty" json:"notes,omitempty"`
}

// TripItemPrice represents the price of a trip item
type TripItemPrice struct {
	Amount   float64 `bson:"amount" json:"amount"`
	Currency string  `bson:"currency" json:"currency"`
}

// TripBudget holds budget information
type TripBudget struct {
	Estimated float64 `bson:"estimated" json:"estimated"`
	Actual    float64 `bson:"actual" json:"actual"`
	Currency  string  `bson:"currency" json:"currency"`
}

// CreateTripRequest is the payload for creating a trip
type CreateTripRequest struct {
	Name         string            `json:"name" validate:"required,min=2"`
	Description  string            `json:"description,omitempty"`
	StartDate    time.Time         `json:"start_date" validate:"required"`
	EndDate      time.Time         `json:"end_date" validate:"required"`
	Destinations []TripDestination `json:"destinations,omitempty"`
	Budget       float64           `json:"budget,omitempty"`
	Currency     string            `json:"currency,omitempty"`
}

// UpdateTripRequest is the payload for updating a trip
type UpdateTripRequest struct {
	Name         string            `json:"name,omitempty"`
	Description  string            `json:"description,omitempty"`
	Status       TripStatus        `json:"status,omitempty"`
	StartDate    *time.Time        `json:"start_date,omitempty"`
	EndDate      *time.Time        `json:"end_date,omitempty"`
	Destinations []TripDestination `json:"destinations,omitempty"`
	Budget       *float64          `json:"budget,omitempty"`
}

// AddTripItemRequest is the payload for adding an item to a trip
type AddTripItemRequest struct {
	Type        TripItemType   `json:"type" validate:"required"`
	ReferenceID string         `json:"reference_id,omitempty"`
	Provider    string         `json:"provider,omitempty"`
	Title       string         `json:"title" validate:"required"`
	Details     map[string]any `json:"details,omitempty"`
	Price       *TripItemPrice `json:"price,omitempty"`
	StartTime   *time.Time     `json:"start_time,omitempty"`
	EndTime     *time.Time     `json:"end_time,omitempty"`
	Notes       string         `json:"notes,omitempty"`
}
