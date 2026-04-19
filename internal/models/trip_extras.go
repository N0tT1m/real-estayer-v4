package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TripRole describes what a collaborator is allowed to do on a trip.
// Viewers see the same page the share-link shows; editors can add/remove/
// reorder items and amend budget/packing/etc. Owners (the trip's UserID)
// have implicit full control — this enum only covers non-owner collaborators.
type TripRole string

const (
	TripRoleViewer TripRole = "viewer"
	TripRoleEditor TripRole = "editor"
)

// TripCollaborator is a user invited to a trip by the owner.
// Stored inline on the trip rather than a join collection because the cap
// per trip is small (< ~50) and it keeps queries single-document.
type TripCollaborator struct {
	UserID primitive.ObjectID `bson:"user_id" json:"user_id"`
	Email  string             `bson:"email,omitempty" json:"email,omitempty"` // cached for display even if user is deleted
	Name   string             `bson:"name,omitempty"  json:"name,omitempty"`
	Role   TripRole           `bson:"role"            json:"role"`
	AddedAt time.Time         `bson:"added_at"        json:"added_at"`
}

// TripComment is a note attached to either the trip as a whole (ItemID zero)
// or a single trip item.
type TripComment struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"        json:"id"`
	TripID    primitive.ObjectID `bson:"trip_id"              json:"trip_id"`
	ItemID    primitive.ObjectID `bson:"item_id,omitempty"    json:"item_id,omitempty"`
	UserID    primitive.ObjectID `bson:"user_id"              json:"user_id"`
	AuthorName string            `bson:"author_name,omitempty" json:"author_name,omitempty"`
	Body      string             `bson:"body"                 json:"body"`
	CreatedAt time.Time          `bson:"created_at"           json:"created_at"`
}

// TripExpense is a recorded spend against a trip, used for the
// budget-vs-actual rollup and the group split view.
type TripExpense struct {
	ID         primitive.ObjectID   `bson:"_id,omitempty" json:"id"`
	TripID     primitive.ObjectID   `bson:"trip_id"       json:"trip_id"`
	PaidBy     primitive.ObjectID   `bson:"paid_by"       json:"paid_by"`
	PaidByName string               `bson:"paid_by_name,omitempty" json:"paid_by_name,omitempty"`
	Description string              `bson:"description"   json:"description"`
	Category   TripItemType         `bson:"category"      json:"category"` // flight, hotel, car, activity, etc.
	Amount     float64              `bson:"amount"        json:"amount"`
	Currency   string               `bson:"currency"      json:"currency"`
	SplitWith  []primitive.ObjectID `bson:"split_with,omitempty" json:"split_with,omitempty"`
	SpentAt    time.Time            `bson:"spent_at"      json:"spent_at"`
	CreatedAt  time.Time            `bson:"created_at"    json:"created_at"`
}

// PackingItem is one row on a trip's packing list. Ownership is implicit —
// the item belongs to the trip, not a specific user.
type PackingItem struct {
	ID       primitive.ObjectID `bson:"_id,omitempty" json:"id"`
	TripID   primitive.ObjectID `bson:"trip_id"       json:"trip_id"`
	Category string             `bson:"category"      json:"category"` // "Clothing", "Tech", etc.
	Name     string             `bson:"name"          json:"name"`
	Quantity int                `bson:"quantity"      json:"quantity"`
	Packed   bool               `bson:"packed"        json:"packed"`
	Note     string             `bson:"note,omitempty" json:"note,omitempty"`
	AutoSuggested bool          `bson:"auto_suggested,omitempty" json:"auto_suggested,omitempty"`
	CreatedAt time.Time         `bson:"created_at"    json:"created_at"`
}

// ChecklistItem is a pre-trip to-do: "book airport transfer", "check visa",
// "print boarding passes".
type ChecklistItem struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"         json:"id"`
	TripID    primitive.ObjectID `bson:"trip_id"               json:"trip_id"`
	Title     string             `bson:"title"                 json:"title"`
	Done      bool               `bson:"done"                  json:"done"`
	DueAt     *time.Time         `bson:"due_at,omitempty"      json:"due_at,omitempty"`
	AutoSuggested bool           `bson:"auto_suggested,omitempty" json:"auto_suggested,omitempty"`
	CreatedAt time.Time          `bson:"created_at"            json:"created_at"`
}

// JournalEntry is a post-trip (or in-trip) log entry. MediaURLs are a stable
// list of external URLs — there's no upload path yet, so users paste links.
type JournalEntry struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"  json:"id"`
	TripID    primitive.ObjectID `bson:"trip_id"        json:"trip_id"`
	UserID    primitive.ObjectID `bson:"user_id"        json:"user_id"`
	AuthorName string            `bson:"author_name,omitempty" json:"author_name,omitempty"`
	Title     string             `bson:"title,omitempty" json:"title,omitempty"`
	Body      string             `bson:"body"           json:"body"`
	MediaURLs []string           `bson:"media_urls,omitempty" json:"media_urls,omitempty"`
	EntryDate time.Time          `bson:"entry_date"     json:"entry_date"`
	CreatedAt time.Time          `bson:"created_at"     json:"created_at"`
}

// ItemReview captures a star rating plus freeform notes for a single trip
// item — used post-trip to feed "your favorite places" later.
type ItemReview struct {
	ID        primitive.ObjectID `bson:"_id,omitempty"  json:"id"`
	TripID    primitive.ObjectID `bson:"trip_id"        json:"trip_id"`
	ItemID    primitive.ObjectID `bson:"item_id"        json:"item_id"`
	UserID    primitive.ObjectID `bson:"user_id"        json:"user_id"`
	Rating    int                `bson:"rating"         json:"rating"` // 1..5
	Body      string             `bson:"body,omitempty" json:"body,omitempty"`
	CreatedAt time.Time          `bson:"created_at"     json:"created_at"`
}

// TripBudgetSummary is a view-model returned by the budget service.
type TripBudgetSummary struct {
	Estimated    float64             `json:"estimated"`
	PlannedTotal float64             `json:"planned_total"` // sum of item prices
	SpentTotal   float64             `json:"spent_total"`   // sum of recorded expenses
	Currency     string              `json:"currency"`
	ByCategory   map[string]BudgetLine `json:"by_category"`
}

// BudgetLine is one category in the rollup: planned (trip items) vs actual
// (recorded expenses).
type BudgetLine struct {
	Planned float64 `json:"planned"`
	Actual  float64 `json:"actual"`
}
