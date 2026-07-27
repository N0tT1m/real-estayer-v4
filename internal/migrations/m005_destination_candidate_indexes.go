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
		Version: 5,
		Name:    "destination_candidate_indexes",
		Apply:   destinationCandidateIndexes,
	})
}

// destinationCandidateIndexes prepares the activity finder's review queue.
//
// The unique index on normalized_name is load-bearing rather than an
// optimisation. The finder resolves several places concurrently and multiple
// requests can surface the same place at the same moment, so RecordSuggestion
// relies on an upsert being atomic against a unique key. Without it, a place
// suggested twice in the same second becomes two queue rows, and approving
// both inserts the destination twice.
//
// Idempotent via createIndex upsert semantics: an identical index is a no-op.
func destinationCandidateIndexes(ctx context.Context, db *database.DB) error {
	_, err := db.Collection("destination_candidates").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "normalized_name", Value: 1}},
			Options: options.Index().SetUnique(true).SetName("normalized_name_unique"),
		},
		{
			// Serves the queue listing: filter by status, most-suggested first.
			Keys:    bson.D{{Key: "status", Value: 1}, {Key: "suggested_count", Value: -1}},
			Options: options.Index().SetName("status_suggested_count"),
		},
	})
	return err
}
