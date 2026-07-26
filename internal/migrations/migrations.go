// Package migrations provides a small, linearly-versioned schema migration
// runner for MongoDB. It's deliberately minimal — no DSL, no down-migrations,
// no branching — because Mongo's schemaless model means most of what this
// runs is backfills and index reshaping, which aren't meaningfully reversible
// anyway.
//
// How it works:
//
//  1. Each migration is a (Version int, Name string, Apply func) triple
//     registered at package init time in this repo (see m001_*.go, etc.).
//  2. Registered migrations must have strictly increasing versions starting
//     at 1; the runner panics at startup if that invariant is violated.
//  3. On Run(), we read applied versions from the schema_migrations
//     collection, execute anything missing in order, and record the version
//     on success. Partial-failure leaves earlier migrations applied so a
//     rerun picks up where we stopped.
//
// A migration is expected to be idempotent — a repeat Apply on an
// already-migrated DB must be a no-op. That lets us rerun safely after a
// crash and lets dev machines catch up without needing to know what state
// they're in.
package migrations

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/realestayer/v4/internal/database"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Migration is one forward-only step against the Mongo schema.
type Migration struct {
	Version int
	Name    string
	Apply   func(ctx context.Context, db *database.DB) error
}

var (
	mu         sync.Mutex
	registered []Migration
)

// Register adds a migration to the package-level list. Called from init()
// functions in individual migration files. Panics if the version is <= 0 or
// collides with an already-registered version — both are programmer errors
// that would corrupt the run order silently.
func Register(m Migration) {
	mu.Lock()
	defer mu.Unlock()
	if m.Version <= 0 {
		panic(fmt.Sprintf("migration %q has non-positive version %d", m.Name, m.Version))
	}
	for _, existing := range registered {
		if existing.Version == m.Version {
			panic(fmt.Sprintf("duplicate migration version %d (%q vs %q)", m.Version, existing.Name, m.Name))
		}
	}
	registered = append(registered, m)
}

// migrationRecord is the shape of one row in schema_migrations.
type migrationRecord struct {
	Version   int       `bson:"_id"`
	Name      string    `bson:"name"`
	AppliedAt time.Time `bson:"applied_at"`
}

// Run executes any migrations whose version is greater than the highest
// applied version, in ascending order. Safe to call at app startup.
func Run(ctx context.Context, db *database.DB) error {
	mu.Lock()
	mlist := make([]Migration, len(registered))
	copy(mlist, registered)
	mu.Unlock()

	sort.Slice(mlist, func(i, j int) bool { return mlist[i].Version < mlist[j].Version })

	coll := db.Collection("schema_migrations")
	applied, err := loadApplied(ctx, coll)
	if err != nil {
		return fmt.Errorf("load applied migrations: %w", err)
	}

	ran := 0
	for _, m := range mlist {
		if applied[m.Version] {
			continue
		}
		slog.Info("migration running", "version", m.Version, "name", m.Name)
		if err := m.Apply(ctx, db); err != nil {
			return fmt.Errorf("migration %d (%s) failed: %w", m.Version, m.Name, err)
		}
		rec := migrationRecord{Version: m.Version, Name: m.Name, AppliedAt: time.Now()}
		if _, err := coll.ReplaceOne(ctx, bson.M{"_id": m.Version}, rec, options.Replace().SetUpsert(true)); err != nil {
			return fmt.Errorf("record migration %d: %w", m.Version, err)
		}
		ran++
		slog.Info("migration applied", "version", m.Version, "name", m.Name)
	}
	slog.Info("migrations complete", "ran", ran, "total_registered", len(mlist))
	return nil
}

func loadApplied(ctx context.Context, coll *mongo.Collection) (map[int]bool, error) {
	cur, err := coll.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = cur.Close(ctx) }()
	out := map[int]bool{}
	var rec migrationRecord
	for cur.Next(ctx) {
		if err := cur.Decode(&rec); err != nil {
			return nil, err
		}
		out[rec.Version] = true
	}
	return out, cur.Err()
}
