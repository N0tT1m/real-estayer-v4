package database

import (
	"context"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// DB wraps the MongoDB client and database
type DB struct {
	Client   *mongo.Client
	Database *mongo.Database
}

// defaultDatabase is used when neither an explicit name nor a URI path
// supplies one. It matches the historical hardcoded value so existing
// deployments keep pointing at the same database.
const defaultDatabase = "real_estayer"

// databaseFromURI extracts the database name from a Mongo connection string
// ("mongodb://host:27017/name" -> "name"). Returns "" when the URI carries no
// path, which is valid — the caller then falls back.
func databaseFromURI(uri string) string {
	u, err := url.Parse(uri)
	if err != nil {
		return ""
	}
	return strings.TrimPrefix(u.Path, "/")
}

// resolveDatabaseName applies the precedence explicit name > URI path >
// defaultDatabase. Kept separate from Connect so the rule can be tested
// without opening a connection — notably without a test having to connect to
// the default (production) database just to observe the fallback.
func resolveDatabaseName(uri, dbName string) string {
	if dbName != "" {
		return dbName
	}
	if fromURI := databaseFromURI(uri); fromURI != "" {
		return fromURI
	}
	return defaultDatabase
}

// Connect establishes a connection to MongoDB and selects a database.
//
// Resolution order: explicit dbName, then the URI path, then defaultDatabase.
// Before dbName existed the database was hardcoded, so MONGODB_DATABASE was
// silently ignored and every deployment wrote to "real_estayer" whatever the
// config said. Defaults are unchanged — both the default config value and the
// default URI path resolve to the same name as before.
func Connect(uri, dbName string) (*DB, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	clientOptions := options.Client().
		ApplyURI(uri).
		SetMaxPoolSize(50).
		SetMinPoolSize(5).
		SetMaxConnIdleTime(30 * time.Second).
		SetServerSelectionTimeout(5 * time.Second).
		SetRetryWrites(true).
		SetRetryReads(true)

	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return nil, err
	}

	// Verify connection
	if err := client.Ping(ctx, readpref.Primary()); err != nil {
		return nil, err
	}

	slog.Info("connected to MongoDB")

	database := client.Database(resolveDatabaseName(uri, dbName))

	db := &DB{
		Client:   client,
		Database: database,
	}

	// Create indexes
	if err := db.createIndexes(ctx); err != nil {
		slog.Warn("failed to create some indexes", "error", err)
	}

	return db, nil
}

// Disconnect closes the MongoDB connection
func (db *DB) Disconnect(ctx context.Context) error {
	return db.Client.Disconnect(ctx)
}

// Collection returns a collection from the database
func (db *DB) Collection(name string) *mongo.Collection {
	return db.Database.Collection(name)
}

// createIndexes sets up database indexes for optimal queries
func (db *DB) createIndexes(ctx context.Context) error {
	// Users collection indexes
	usersIndexes := []mongo.IndexModel{
		{
			Keys:    map[string]int{"email": 1},
			Options: options.Index().SetUnique(true),
		},
	}
	if _, err := db.Collection("users").Indexes().CreateMany(ctx, usersIndexes); err != nil {
		slog.Warn("failed to create users indexes", "error", err)
	}

	// Sessions collection indexes
	sessionsIndexes := []mongo.IndexModel{
		{
			Keys:    map[string]int{"token": 1},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys:    map[string]int{"user_id": 1},
			Options: options.Index(),
		},
		{
			Keys:    map[string]int{"expires_at": 1},
			Options: options.Index().SetExpireAfterSeconds(0),
		},
	}
	if _, err := db.Collection("sessions").Indexes().CreateMany(ctx, sessionsIndexes); err != nil {
		slog.Warn("failed to create sessions indexes", "error", err)
	}

	// Listings collection indexes
	listingsIndexes := []mongo.IndexModel{
		{
			Keys:    map[string]int{"url": 1},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "location", Value: 1}, {Key: "price_numeric", Value: 1}},
		},
		{
			Keys: bson.D{{Key: "region", Value: 1}, {Key: "country", Value: 1}},
		},
		{
			Keys: map[string]int{"rating_numeric": -1},
		},
	}
	if _, err := db.Collection("listings").Indexes().CreateMany(ctx, listingsIndexes); err != nil {
		slog.Warn("failed to create listings indexes", "error", err)
	}

	// Watchlists collection indexes
	watchlistIndexes := []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "listing_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: map[string]int{"user_id": 1},
		},
	}
	if _, err := db.Collection("watchlists").Indexes().CreateMany(ctx, watchlistIndexes); err != nil {
		slog.Warn("failed to create watchlists indexes", "error", err)
	}

	// Trips collection indexes
	tripsIndexes := []mongo.IndexModel{
		{
			Keys: map[string]int{"user_id": 1},
		},
		{
			Keys: map[string]int{"status": 1},
		},
	}
	if _, err := db.Collection("trips").Indexes().CreateMany(ctx, tripsIndexes); err != nil {
		slog.Warn("failed to create trips indexes", "error", err)
	}

	// Trip-scoped collections — every query is keyed on trip_id so those
	// indexes are non-optional.
	tripCommentIndexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "trip_id", Value: 1}, {Key: "created_at", Value: 1}}},
	}
	_, _ = db.Collection("trip_comments").Indexes().CreateMany(ctx, tripCommentIndexes)

	tripExpenseIndexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "trip_id", Value: 1}, {Key: "spent_at", Value: -1}}},
	}
	_, _ = db.Collection("trip_expenses").Indexes().CreateMany(ctx, tripExpenseIndexes)

	tripJournalIndexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "trip_id", Value: 1}, {Key: "entry_date", Value: -1}}},
	}
	_, _ = db.Collection("trip_journal").Indexes().CreateMany(ctx, tripJournalIndexes)

	_, _ = db.Collection("trip_reviews").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "user_id", Value: 1}, {Key: "trip_id", Value: 1}, {Key: "item_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	})

	priceHistoryIndexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "listing_id", Value: 1}, {Key: "captured_at", Value: -1}}},
	}
	_, _ = db.Collection("price_history").Indexes().CreateMany(ctx, priceHistoryIndexes)

	savedSearchIndexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}}},
		{Keys: map[string]int{"last_run_at": 1}},
	}
	_, _ = db.Collection("saved_searches").Indexes().CreateMany(ctx, savedSearchIndexes)

	_, _ = db.Collection("password_resets").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    map[string]int{"expires_at": 1},
		Options: options.Index().SetExpireAfterSeconds(0),
	})
	_, _ = db.Collection("password_resets").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: map[string]int{"token_hash": 1},
	})

	collectionIndexes := []mongo.IndexModel{
		{Keys: map[string]int{"slug": 1}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "destination", Value: 1}, {Key: "featured", Value: -1}}},
	}
	_, _ = db.Collection("collections").Indexes().CreateMany(ctx, collectionIndexes)

	_, _ = db.Collection("polls").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    map[string]int{"slug": 1},
		Options: options.Index().SetUnique(true),
	})
	_, _ = db.Collection("polls").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: map[string]int{"trip_id": 1},
	})

	// Trip queries hit FindByUserID (with shared_with OR) and share-slug
	// lookups constantly.
	_, _ = db.Collection("trips").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "start_date", Value: -1}},
	})
	_, _ = db.Collection("trips").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys: map[string]int{"shared_with": 1},
	})
	_, _ = db.Collection("trips").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    map[string]int{"share_slug": 1},
		Options: options.Index().SetUnique(true).SetSparse(true),
	})

	// Bookings collection indexes
	bookingsIndexes := []mongo.IndexModel{
		{
			Keys: map[string]int{"user_id": 1},
		},
		{
			Keys:    map[string]int{"provider_reference": 1},
			Options: options.Index().SetUnique(true).SetSparse(true),
		},
		{
			Keys: bson.D{{Key: "type", Value: 1}, {Key: "status", Value: 1}},
		},
	}
	if _, err := db.Collection("bookings").Indexes().CreateMany(ctx, bookingsIndexes); err != nil {
		slog.Warn("failed to create bookings indexes", "error", err)
	}

	auditIndexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "user_id", Value: 1}, {Key: "created_at", Value: -1}}},
		{Keys: bson.D{{Key: "action", Value: 1}, {Key: "created_at", Value: -1}}},
	}
	if _, err := db.Collection("audit_logs").Indexes().CreateMany(ctx, auditIndexes); err != nil {
		slog.Warn("failed to create audit indexes", "error", err)
	}

	slog.Info("database indexes created")
	return nil
}
