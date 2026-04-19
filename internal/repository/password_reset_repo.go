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
)

var ErrResetNotFound = errors.New("reset token not found")

// PasswordResetRepository persists password-reset tokens (hashed).
type PasswordResetRepository struct {
	collection *mongo.Collection
}

func NewPasswordResetRepository(db *database.DB) *PasswordResetRepository {
	return &PasswordResetRepository{collection: db.Collection("password_resets")}
}

func (r *PasswordResetRepository) Create(ctx context.Context, reset *models.PasswordReset) error {
	reset.ID = primitive.NewObjectID()
	reset.CreatedAt = time.Now()
	_, err := r.collection.InsertOne(ctx, reset)
	return err
}

// FindByHash returns an unused, unexpired reset matching the given token hash.
func (r *PasswordResetRepository) FindByHash(ctx context.Context, tokenHash string) (*models.PasswordReset, error) {
	var reset models.PasswordReset
	err := r.collection.FindOne(ctx, bson.M{
		"token_hash": tokenHash,
		"used_at":    nil,
		"expires_at": bson.M{"$gt": time.Now()},
	}).Decode(&reset)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrResetNotFound
	}
	return &reset, err
}

// MarkUsed records that the token was consumed so replays are rejected.
func (r *PasswordResetRepository) MarkUsed(ctx context.Context, id primitive.ObjectID) error {
	now := time.Now()
	_, err := r.collection.UpdateOne(ctx, bson.M{"_id": id}, bson.M{"$set": bson.M{"used_at": now}})
	return err
}

// DeleteForUser removes any outstanding tokens for a user. Call after a
// successful reset or change so a leaked link can't later be replayed.
func (r *PasswordResetRepository) DeleteForUser(ctx context.Context, userID primitive.ObjectID) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"user_id": userID})
	return err
}
