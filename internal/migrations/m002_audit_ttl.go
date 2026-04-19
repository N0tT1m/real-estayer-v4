package migrations

import (
	"context"

	"github.com/realestayer/v4/internal/database"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func init() {
	Register(Migration{
		Version: 2,
		Name:    "audit_logs_ttl_365d",
		Apply:   auditLogsTTL,
	})
}

// auditLogsTTL installs a 365-day TTL on audit_logs.created_at. Mongo's TTL
// monitor sweeps expired docs out once a minute — we keep a year so users
// can review "security activity" over the last twelve months without letting
// the collection grow unbounded forever.
//
// Idempotent via createIndex upsert semantics: if the index already exists
// with the same options, Mongo no-ops; if it exists with different options
// the migration fails loudly (by design — silent TTL changes lose data).
func auditLogsTTL(ctx context.Context, db *database.DB) error {
	const year = 365 * 24 * 60 * 60
	_, err := db.Collection("audit_logs").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "created_at", Value: 1}},
		Options: options.Index().SetExpireAfterSeconds(int32(year)).SetName("created_at_ttl"),
	})
	return err
}
