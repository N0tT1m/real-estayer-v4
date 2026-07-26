package repository

import (
	"context"
	"errors"
	"time"

	"github.com/realestayer/v4/internal/database"
	"github.com/realestayer/v4/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var ErrPollNotFound = errors.New("poll not found")

// PollRepository persists AvailabilityPolls. Responses live inside the same
// document — polls are small and always read whole.
type PollRepository struct {
	collection *mongo.Collection
}

func NewPollRepository(db *database.DB) *PollRepository {
	return &PollRepository{collection: db.Collection("polls")}
}

func (r *PollRepository) Create(ctx context.Context, p *models.AvailabilityPoll) error {
	now := time.Now()
	p.ID = primitive.NewObjectID()
	p.CreatedAt, p.UpdatedAt = now, now
	for i := range p.Options {
		if p.Options[i].ID.IsZero() {
			p.Options[i].ID = primitive.NewObjectID()
		}
	}
	_, err := r.collection.InsertOne(ctx, p)
	return err
}

func (r *PollRepository) FindBySlug(ctx context.Context, slug string) (*models.AvailabilityPoll, error) {
	var p models.AvailabilityPoll
	err := r.collection.FindOne(ctx, bson.M{"slug": slug}).Decode(&p)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrPollNotFound
	}
	return &p, err
}

func (r *PollRepository) ListForTrip(ctx context.Context, tripID primitive.ObjectID) ([]models.AvailabilityPoll, error) {
	opts := options.Find().SetSort(bson.M{"created_at": -1})
	cursor, err := r.collection.Find(ctx, bson.M{"trip_id": tripID}, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()
	var out []models.AvailabilityPoll
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *PollRepository) ListForUser(ctx context.Context, userID primitive.ObjectID) ([]models.AvailabilityPoll, error) {
	opts := options.Find().SetSort(bson.M{"created_at": -1})
	cursor, err := r.collection.Find(ctx, bson.M{"created_by": userID}, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()
	var out []models.AvailabilityPoll
	if err := cursor.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// UpsertResponse replaces a matching participant's vote (matched by UserID
// when present, otherwise Email). If no match, appends.
func (r *PollRepository) UpsertResponse(ctx context.Context, slug string, resp models.PollResponse) error {
	now := time.Now()
	resp.UpdatedAt = now
	if resp.CreatedAt.IsZero() {
		resp.CreatedAt = now
	}

	// Remove any existing entry that matches, then push the fresh one.
	pullFilter := bson.M{}
	if resp.UserID != nil {
		pullFilter["user_id"] = *resp.UserID
	} else {
		pullFilter["email"] = resp.Email
	}
	if _, err := r.collection.UpdateOne(ctx, bson.M{"slug": slug}, bson.M{
		"$pull": bson.M{"responses": pullFilter},
		"$set":  bson.M{"updated_at": now},
	}); err != nil {
		return err
	}
	res, err := r.collection.UpdateOne(ctx, bson.M{"slug": slug}, bson.M{
		"$push": bson.M{"responses": resp},
		"$set":  bson.M{"updated_at": now},
	})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return ErrPollNotFound
	}
	return nil
}

func (r *PollRepository) Delete(ctx context.Context, ownerID, id primitive.ObjectID) error {
	res, err := r.collection.DeleteOne(ctx, bson.M{"_id": id, "created_by": ownerID})
	if err != nil {
		return err
	}
	if res.DeletedCount == 0 {
		return ErrPollNotFound
	}
	return nil
}
