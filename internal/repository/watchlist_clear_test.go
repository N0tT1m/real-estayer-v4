package repository

import (
	"testing"
	"time"

	"github.com/realestayer/v4/internal/dbtest"
	"github.com/realestayer/v4/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Clearing a field must actually clear it.
//
// Update writes the whole struct through one $set, so any bson `omitempty` on
// a user-mutable field turns "set this back to empty" into a silent no-op: the
// write succeeds, the API answers 200, and the old value survives. That is the
// same defect that made DisableTOTP a no-op, and UpdateWatchlistRequest.Notes
// is a *string specifically so a caller can ask for the empty value.
func TestWatchlistUpdateClearsNotes(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewWatchlistRepository(db)

	item := &models.WatchlistItem{
		UserID:      primitive.NewObjectID(),
		ListingID:   primitive.NewObjectID(),
		TargetPrice: 100,
		Notes:       "watch for a dip before June",
		IsActive:    true,
		CreatedAt:   time.Now(),
	}
	if err := repo.Create(ctx, item); err != nil {
		t.Fatalf("Create: %v", err)
	}

	stored, err := repo.FindByID(ctx, item.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if stored.Notes == "" {
		t.Fatal("precondition failed: the note was never stored")
	}

	stored.Notes = ""
	if err := repo.Update(ctx, stored); err != nil {
		t.Fatalf("Update: %v", err)
	}

	after, err := repo.FindByID(ctx, item.ID)
	if err != nil {
		t.Fatalf("FindByID after clear: %v", err)
	}
	if after.Notes != "" {
		t.Errorf("Notes = %q after clearing; the empty value was dropped from the update", after.Notes)
	}
}

// The neighbouring booleans and numbers must round-trip their zero values too
// — those fields have no omitempty, and this pins that.
func TestWatchlistUpdateClearsZeroValuedFields(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewWatchlistRepository(db)

	item := &models.WatchlistItem{
		UserID:      primitive.NewObjectID(),
		ListingID:   primitive.NewObjectID(),
		TargetPrice: 250,
		IsActive:    true,
		CreatedAt:   time.Now(),
	}
	if err := repo.Create(ctx, item); err != nil {
		t.Fatalf("Create: %v", err)
	}

	item.IsActive = false
	item.TargetPrice = 0
	if err := repo.Update(ctx, item); err != nil {
		t.Fatalf("Update: %v", err)
	}

	after, err := repo.FindByID(ctx, item.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if after.IsActive {
		t.Error("IsActive stayed true; deactivating a watchlist item silently failed")
	}
	if after.TargetPrice != 0 {
		t.Errorf("TargetPrice = %v, want 0", after.TargetPrice)
	}
}
