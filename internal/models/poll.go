package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// AvailabilityPoll asks "which of these date windows work for everyone?".
// It lives alongside a trip (one trip → many polls) but doesn't require a
// trip — a crew can answer "when should we go to Lisbon?" before a trip is
// even created.
type AvailabilityPoll struct {
	ID          primitive.ObjectID  `bson:"_id,omitempty"      json:"id"`
	CreatedBy   primitive.ObjectID  `bson:"created_by"         json:"created_by"`
	TripID      *primitive.ObjectID `bson:"trip_id,omitempty"  json:"trip_id,omitempty"`
	Title       string              `bson:"title"              json:"title"`
	Description string              `bson:"description,omitempty" json:"description,omitempty"`
	Options     []PollOption        `bson:"options"            json:"options"`
	Responses   []PollResponse      `bson:"responses,omitempty" json:"responses,omitempty"`
	Slug        string              `bson:"slug"               json:"slug"` // unguessable; anyone with the link can respond
	ClosesAt    *time.Time          `bson:"closes_at,omitempty" json:"closes_at,omitempty"`
	CreatedAt   time.Time           `bson:"created_at"         json:"created_at"`
	UpdatedAt   time.Time           `bson:"updated_at"         json:"updated_at"`
}

// PollOption is one date-range choice.
type PollOption struct {
	ID    primitive.ObjectID `bson:"id"     json:"id"`
	Start time.Time          `bson:"start"  json:"start"`
	End   time.Time          `bson:"end"    json:"end"`
	Label string             `bson:"label,omitempty" json:"label,omitempty"` // optional "the long weekend"
}

// PollResponse is one participant's availability. Users can respond once and
// update in place (keyed by Email for anonymous respondents, or UserID when
// logged in).
type PollResponse struct {
	UserID      *primitive.ObjectID `bson:"user_id,omitempty"   json:"user_id,omitempty"`
	Email       string              `bson:"email,omitempty"     json:"email,omitempty"`
	DisplayName string              `bson:"display_name"        json:"display_name"`
	OptionVotes map[string]string   `bson:"option_votes"        json:"option_votes"` // map[optionID] = "yes"|"maybe"|"no"
	Note        string              `bson:"note,omitempty"      json:"note,omitempty"`
	CreatedAt   time.Time           `bson:"created_at"          json:"created_at"`
	UpdatedAt   time.Time           `bson:"updated_at"          json:"updated_at"`
}

// PollSummary is a tallied view the UI can render without doing the math.
type PollSummary struct {
	Option PollOption `json:"option"`
	Yes    int        `json:"yes"`
	Maybe  int        `json:"maybe"`
	No     int        `json:"no"`
	Score  float64    `json:"score"` // yes + 0.5*maybe, for default ranking
}
