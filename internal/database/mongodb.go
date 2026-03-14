package database

import (
	"context"
	"log/slog"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// DB wraps the MongoDB client and database
type DB struct {
	Client   *mongo.Client
	Database *mongo.Database
}

// Connect establishes a connection to MongoDB
func Connect(uri string) (*DB, error) {
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

	// Get database from URI or use default
	database := client.Database("real_estayer")

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
			Keys: map[string]int{"location": 1, "price_numeric": 1},
		},
		{
			Keys: map[string]int{"region": 1, "country": 1},
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
			Keys:    map[string]int{"user_id": 1, "listing_id": 1},
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
			Keys: map[string]int{"type": 1, "status": 1},
		},
	}
	if _, err := db.Collection("bookings").Indexes().CreateMany(ctx, bookingsIndexes); err != nil {
		slog.Warn("failed to create bookings indexes", "error", err)
	}

	slog.Info("database indexes created")
	return nil
}
