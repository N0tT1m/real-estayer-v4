package models

import (
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Candidate review states.
const (
	CandidatePending  = "pending"
	CandidateApproved = "approved"
	CandidateRejected = "rejected"
)

// DestinationCandidate is a real place the activity finder surfaced that is
// not in the destinations collection, held for an admin to accept or refuse.
//
// It exists because the finder is allowed to answer with places we do not
// cover — that is the whole point of it — but an answer reaching one user is
// a much lower bar than a row appearing in /explore for everyone. Candidates
// are shown to the person who searched and stay out of browse until approved.
//
// Every field here is copied from Wikidata or Wikipedia at resolution time, so
// approving one is a move between collections rather than a re-fetch.
type DestinationCandidate struct {
	ID primitive.ObjectID `bson:"_id,omitempty" json:"id"`

	// NormalizedName is the dedupe key: lowercased and whitespace-collapsed,
	// matching how the suggest service compares model output to catalog names.
	// Unique, so a place suggested by fifty searches is one row.
	NormalizedName string `bson:"normalized_name" json:"normalized_name"`

	Name        string   `bson:"name" json:"name"`
	Country     string   `bson:"country" json:"country"`
	CountryCode string   `bson:"country_code" json:"country_code"`
	Region      string   `bson:"region" json:"region"`
	Description string   `bson:"description" json:"description"`
	ImageURL    string   `bson:"image_url" json:"image_url"`
	Latitude    float64  `bson:"latitude" json:"latitude"`
	Longitude   float64  `bson:"longitude" json:"longitude"`
	Categories  []string `bson:"categories" json:"categories"`

	AvgDailyBudget  float64 `bson:"avg_daily_budget" json:"avg_daily_budget"`
	PopularityScore int     `bson:"popularity_score" json:"popularity_score"`

	// Provenance, so a reviewer can check the resolution rather than trust it.
	// SitelinkCount is the signal that picked this entity over its namesakes.
	WikidataID    string `bson:"wikidata_id" json:"wikidata_id"`
	ArticleTitle  string `bson:"article_title" json:"article_title"`
	WikipediaURL  string `bson:"wikipedia_url" json:"wikipedia_url"`
	SitelinkCount int    `bson:"sitelink_count" json:"sitelink_count"`

	Status string `bson:"status" json:"status"`
	// FirstActivities is the search that first surfaced this place. Useful
	// context when reviewing: "polar bears" explains Churchill far better than
	// the Wikipedia summary does.
	FirstActivities []string `bson:"first_activities,omitempty" json:"first_activities,omitempty"`
	// SuggestedCount is how many searches have landed on this place. A high
	// count on a pending row is the queue asking to be looked at.
	SuggestedCount int `bson:"suggested_count" json:"suggested_count"`

	CreatedAt  time.Time  `bson:"created_at" json:"created_at"`
	UpdatedAt  time.Time  `bson:"updated_at" json:"updated_at"`
	ReviewedAt *time.Time `bson:"reviewed_at,omitempty" json:"reviewed_at,omitempty"`
	ReviewedBy string     `bson:"reviewed_by,omitempty" json:"reviewed_by,omitempty"`
}

// AsDestination renders the candidate in the shape the rest of the app speaks,
// for returning to the searcher before any review has happened. The ID is
// deliberately left zero: this is not a destinations row, nothing can link to
// it, and giving it an ID would invite code to treat it as browsable.
func (c *DestinationCandidate) AsDestination() Destination {
	return Destination{
		Name:            c.Name,
		Country:         c.Country,
		CountryCode:     c.CountryCode,
		Region:          c.Region,
		Description:     c.Description,
		ImageURL:        c.ImageURL,
		Latitude:        c.Latitude,
		Longitude:       c.Longitude,
		Categories:      c.Categories,
		AvgDailyBudget:  c.AvgDailyBudget,
		PopularityScore: c.PopularityScore,
		Currency:        "USD",
	}
}
