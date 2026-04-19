package migrations

import (
	"context"
	"log/slog"

	"github.com/realestayer/v4/internal/database"
	"go.mongodb.org/mongo-driver/bson"
)

func init() {
	Register(Migration{
		Version: 3,
		Name:    "share_slug_normalize",
		Apply:   shareSlugNormalize,
	})
}

// shareSlugNormalize unsets empty string share_slug values so the sparse
// unique index on trips.share_slug doesn't collide on the empty key. Older
// docs (pre-share feature) may have `share_slug: ""` rather than absent,
// which violates sparse + unique semantics on Mongo ≤ 4.x and logs
// noisy upsert duplicates on newer versions.
func shareSlugNormalize(ctx context.Context, db *database.DB) error {
	res, err := db.Collection("trips").UpdateMany(ctx,
		bson.M{"share_slug": ""},
		bson.M{"$unset": bson.M{"share_slug": ""}})
	if err != nil {
		return err
	}
	slog.Info("share_slug_normalize", "matched", res.MatchedCount, "modified", res.ModifiedCount)
	return nil
}
