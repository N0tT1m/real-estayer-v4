package repository

import (
	"context"
	"time"

	"github.com/realestayer/v4/internal/database"
	"github.com/realestayer/v4/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// AuditRepository persists AuditEvent rows. Writes are insert-only; nothing
// in this codebase updates or deletes audit rows, which is the whole point.
type AuditRepository struct {
	collection *mongo.Collection
}

func NewAuditRepository(db *database.DB) *AuditRepository {
	return &AuditRepository{collection: db.Collection("audit_logs")}
}

// Create inserts one audit event. CreatedAt is set server-side if the caller
// leaves it zero so tests can pin time deterministically when they need to.
func (r *AuditRepository) Create(ctx context.Context, e *models.AuditEvent) error {
	if e.ID.IsZero() {
		e.ID = primitive.NewObjectID()
	}
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now()
	}
	_, err := r.collection.InsertOne(ctx, e)
	return err
}

// ListByUser returns the most recent N events for a user, newest first.
// Used by the forthcoming admin/profile "security activity" view.
func (r *AuditRepository) ListByUser(ctx context.Context, userID primitive.ObjectID, limit int64) ([]models.AuditEvent, error) {
	cur, err := r.collection.Find(ctx, bson.M{"user_id": userID}, options.Find().
		SetSort(bson.D{{Key: "created_at", Value: -1}}).
		SetLimit(limit))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []models.AuditEvent
	if err := cur.All(ctx, &out); err != nil {
		return nil, err
	}
	return out, nil
}
