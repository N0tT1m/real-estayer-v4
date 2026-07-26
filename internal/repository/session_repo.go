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

var ErrSessionNotFound = errors.New("session not found")
var ErrSessionExpired = errors.New("session expired")

// SessionRepository handles session data operations
type SessionRepository struct {
	collection *mongo.Collection
}

// NewSessionRepository creates a new session repository
func NewSessionRepository(db *database.DB) *SessionRepository {
	return &SessionRepository{
		collection: db.Collection("sessions"),
	}
}

// Create inserts a new session
func (r *SessionRepository) Create(ctx context.Context, session *models.Session) error {
	session.ID = primitive.NewObjectID()
	session.CreatedAt = time.Now()

	_, err := r.collection.InsertOne(ctx, session)
	return err
}

// FindByToken finds a session by token
func (r *SessionRepository) FindByToken(ctx context.Context, token string) (*models.Session, error) {
	var session models.Session
	err := r.collection.FindOne(ctx, bson.M{"token": token}).Decode(&session)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrSessionNotFound
	}
	if err != nil {
		return nil, err
	}

	// Check if expired
	if time.Now().After(session.ExpiresAt) {
		// Clean up expired session
		_ = r.DeleteByToken(ctx, token)
		return nil, ErrSessionExpired
	}

	return &session, nil
}

// FindByUserID finds all sessions for a user
func (r *SessionRepository) FindByUserID(ctx context.Context, userID primitive.ObjectID) ([]models.Session, error) {
	cursor, err := r.collection.Find(ctx, bson.M{
		"user_id":    userID,
		"expires_at": bson.M{"$gt": time.Now()},
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()

	var sessions []models.Session
	if err := cursor.All(ctx, &sessions); err != nil {
		return nil, err
	}

	return sessions, nil
}

// DeleteByToken removes a session by token
func (r *SessionRepository) DeleteByToken(ctx context.Context, token string) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"token": token})
	return err
}

// DeleteByUserID removes all sessions for a user
func (r *SessionRepository) DeleteByUserID(ctx context.Context, userID primitive.ObjectID) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{"user_id": userID})
	return err
}

// DeleteByUserIDExceptToken removes every session for a user except the one
// matching the supplied token. Used during login rotation so the freshly
// issued cookie survives while older sessions are invalidated.
func (r *SessionRepository) DeleteByUserIDExceptToken(ctx context.Context, userID primitive.ObjectID, keepToken string) error {
	_, err := r.collection.DeleteMany(ctx, bson.M{
		"user_id": userID,
		"token":   bson.M{"$ne": keepToken},
	})
	return err
}

// CleanExpired removes all expired sessions
func (r *SessionRepository) CleanExpired(ctx context.Context) (int64, error) {
	result, err := r.collection.DeleteMany(ctx, bson.M{
		"expires_at": bson.M{"$lt": time.Now()},
	})
	if err != nil {
		return 0, err
	}
	return result.DeletedCount, nil
}
