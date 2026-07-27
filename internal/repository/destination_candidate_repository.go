package repository

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/realestayer/v4/internal/models"
)

type DestinationCandidateRepository struct {
	collection *mongo.Collection
}

func NewDestinationCandidateRepository(db *mongo.Database) *DestinationCandidateRepository {
	return &DestinationCandidateRepository{
		collection: db.Collection("destination_candidates"),
	}
}

// FindByNormalizedName returns the candidate for a normalised name, or
// (nil, nil) when there is none. Callers use the absence to decide whether a
// place still needs resolving.
func (r *DestinationCandidateRepository) FindByNormalizedName(ctx context.Context, key string) (*models.DestinationCandidate, error) {
	var c models.DestinationCandidate
	err := r.collection.FindOne(ctx, bson.M{"normalized_name": key}).Decode(&c)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// RecordSuggestion files a place for review, or notes another sighting of one
// already filed.
//
// Written as a single upsert rather than a read-then-write because the finder
// resolves several places concurrently and the same place can be suggested by
// two requests at once. $setOnInsert carries the resolved facts so a second
// sighting never overwrites what a reviewer may already be looking at, and
// never resurrects a rejected row into pending — only $inc and the timestamp
// touch an existing document.
//
// Returns the stored candidate, which is what the caller shows the searcher.
func (r *DestinationCandidateRepository) RecordSuggestion(ctx context.Context, c *models.DestinationCandidate) (*models.DestinationCandidate, error) {
	now := time.Now()

	update := bson.M{
		"$setOnInsert": bson.M{
			"normalized_name":  c.NormalizedName,
			"name":             c.Name,
			"country":          c.Country,
			"country_code":     c.CountryCode,
			"region":           c.Region,
			"description":      c.Description,
			"image_url":        c.ImageURL,
			"latitude":         c.Latitude,
			"longitude":        c.Longitude,
			"categories":       c.Categories,
			"avg_daily_budget": c.AvgDailyBudget,
			"popularity_score": c.PopularityScore,
			"wikidata_id":      c.WikidataID,
			"article_title":    c.ArticleTitle,
			"wikipedia_url":    c.WikipediaURL,
			"sitelink_count":   c.SitelinkCount,
			"status":           models.CandidatePending,
			"first_activities": c.FirstActivities,
			"created_at":       now,
		},
		"$set": bson.M{"updated_at": now},
		"$inc": bson.M{"suggested_count": 1},
	}

	opts := options.FindOneAndUpdate().
		SetUpsert(true).
		SetReturnDocument(options.After)

	var out models.DestinationCandidate
	err := r.collection.FindOneAndUpdate(ctx, bson.M{"normalized_name": c.NormalizedName}, update, opts).Decode(&out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// NoteSuggested records another search landing on a place already queued.
//
// The count is what orders the queue, and the resolver short-circuits on a
// queue hit without re-running RecordSuggestion — so without this the counter
// would sit at 1 forever and the ordering would be meaningless.
//
// Only pending rows move. A decision already taken should not be reordered by
// later traffic, and an approved row is served from the catalog anyway.
// Returns (nil, nil) when the row is not pending.
func (r *DestinationCandidateRepository) NoteSuggested(ctx context.Context, id primitive.ObjectID) (*models.DestinationCandidate, error) {
	update := bson.M{
		"$inc": bson.M{"suggested_count": 1},
		"$set": bson.M{"updated_at": time.Now()},
	}
	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var out models.DestinationCandidate
	err := r.collection.FindOneAndUpdate(ctx,
		bson.M{"_id": id, "status": models.CandidatePending}, update, opts).Decode(&out)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ListByStatus returns candidates in a given state, most-suggested first so
// the places people keep asking for surface at the top of the queue.
func (r *DestinationCandidateRepository) ListByStatus(ctx context.Context, status string, limit int) ([]models.DestinationCandidate, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	opts := options.Find().
		SetSort(bson.D{{Key: "suggested_count", Value: -1}, {Key: "created_at", Value: 1}}).
		SetLimit(int64(limit))

	cur, err := r.collection.Find(ctx, bson.M{"status": status}, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cur.Close(ctx) }()

	out := []models.DestinationCandidate{}
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// CountByStatus reports the queue depth, for the admin badge.
func (r *DestinationCandidateRepository) CountByStatus(ctx context.Context, status string) (int64, error) {
	return r.collection.CountDocuments(ctx, bson.M{"status": status})
}

// SetStatus records a review decision. It matches on the current status as
// well as the id so that two admins acting on the same row cannot both
// succeed — the second gets (nil, nil) and can be told it was already handled,
// which matters because approving twice would insert the destination twice.
func (r *DestinationCandidateRepository) SetStatus(ctx context.Context, id primitive.ObjectID, from, to, reviewer string) (*models.DestinationCandidate, error) {
	now := time.Now()
	update := bson.M{"$set": bson.M{
		"status":      to,
		"reviewed_at": now,
		"reviewed_by": reviewer,
		"updated_at":  now,
	}}

	opts := options.FindOneAndUpdate().SetReturnDocument(options.After)

	var out models.DestinationCandidate
	err := r.collection.FindOneAndUpdate(ctx, bson.M{"_id": id, "status": from}, update, opts).Decode(&out)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &out, nil
}
