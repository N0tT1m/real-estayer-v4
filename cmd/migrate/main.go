// migrate runs any unapplied schema migrations against the configured
// MongoDB. Migrations are registered in internal/migrations; see that
// package's docs for how to add a new one.
//
// Usage:
//
//	MONGODB_URI=mongodb://localhost:27017/real_estayer go run ./cmd/migrate
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/realestayer/v4/internal/config"
	"github.com/realestayer/v4/internal/database"
	"github.com/realestayer/v4/internal/migrations"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("load config", "error", err)
		os.Exit(1)
	}

	db, err := database.Connect(cfg.MongoURI, cfg.MongoDatabase)
	if err != nil {
		slog.Error("connect mongo", "error", err)
		os.Exit(1)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = db.Disconnect(ctx)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	if err := migrations.Run(ctx, db); err != nil {
		slog.Error("migrations failed", "error", err)
		os.Exit(1)
	}
}
