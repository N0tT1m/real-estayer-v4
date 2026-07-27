package repository

import (
	"testing"

	"github.com/realestayer/v4/internal/dbtest"
	"github.com/realestayer/v4/internal/models"
)

// Integration tests for the activity finder's review queue. Set
// MONGODB_TEST_URI to enable; otherwise they skip.

func candidate(name string) *models.DestinationCandidate {
	return &models.DestinationCandidate{
		NormalizedName: name,
		Name:           name,
		Country:        "Canada",
		CountryCode:    "CA",
		Region:         "North America",
		Latitude:       51.17,
		Longitude:      -115.55,
		Categories:     []string{"city"},
		AvgDailyBudget: 180,
		SitelinkCount:  60,
		Status:         models.CandidatePending,
	}
}

// The dedupe key is the whole point of the queue: a place fifty searches land
// on must be one row to review, not fifty.
func TestRecordSuggestionDedupesAndCounts(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewDestinationCandidateRepository(db.Database)

	first, err := repo.RecordSuggestion(ctx, candidate("banff"))
	if err != nil {
		t.Fatalf("RecordSuggestion: %v", err)
	}
	if first.SuggestedCount != 1 {
		t.Errorf("suggested_count = %d, want 1", first.SuggestedCount)
	}
	if first.Status != models.CandidatePending {
		t.Errorf("status = %q, want pending", first.Status)
	}

	second, err := repo.RecordSuggestion(ctx, candidate("banff"))
	if err != nil {
		t.Fatalf("RecordSuggestion (repeat): %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("a repeat suggestion created a second row: %v vs %v", second.ID, first.ID)
	}
	if second.SuggestedCount != 2 {
		t.Errorf("suggested_count = %d, want 2", second.SuggestedCount)
	}
}

// The resolver short-circuits on a queue hit rather than re-recording, so the
// count has to move through this path or the queue ordering is meaningless.
func TestNoteSuggestedCountsOnlyPendingRows(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewDestinationCandidateRepository(db.Database)

	stored, err := repo.RecordSuggestion(ctx, candidate("dallas"))
	if err != nil {
		t.Fatalf("RecordSuggestion: %v", err)
	}

	bumped, err := repo.NoteSuggested(ctx, stored.ID)
	if err != nil {
		t.Fatalf("NoteSuggested: %v", err)
	}
	if bumped == nil || bumped.SuggestedCount != 2 {
		t.Fatalf("suggested_count = %+v, want 2", bumped)
	}

	// Once reviewed, later traffic must not reorder the decision.
	if _, err := repo.SetStatus(ctx, stored.ID, models.CandidatePending, models.CandidateRejected, "admin"); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	after, err := repo.NoteSuggested(ctx, stored.ID)
	if err != nil {
		t.Fatalf("NoteSuggested (reviewed): %v", err)
	}
	if after != nil {
		t.Errorf("a reviewed row was counted again: %+v", after)
	}
}

// A rejected place must stay rejected however many times the model names it —
// otherwise a refusal is undone by the next search.
func TestRecordSuggestionDoesNotResurrectRejected(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewDestinationCandidateRepository(db.Database)

	stored, err := repo.RecordSuggestion(ctx, candidate("nowhere"))
	if err != nil {
		t.Fatalf("RecordSuggestion: %v", err)
	}
	if _, err := repo.SetStatus(ctx, stored.ID, models.CandidatePending, models.CandidateRejected, "admin"); err != nil {
		t.Fatalf("SetStatus: %v", err)
	}

	again, err := repo.RecordSuggestion(ctx, candidate("nowhere"))
	if err != nil {
		t.Fatalf("RecordSuggestion (after reject): %v", err)
	}
	if again.Status != models.CandidateRejected {
		t.Errorf("status = %q, want the rejection to stick", again.Status)
	}
}

// SetStatus matches on the current status so two admins deciding the same row
// cannot both succeed — approving twice would insert the destination twice.
func TestSetStatusIsClaimedOnce(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewDestinationCandidateRepository(db.Database)

	stored, err := repo.RecordSuggestion(ctx, candidate("churchill"))
	if err != nil {
		t.Fatalf("RecordSuggestion: %v", err)
	}

	claimed, err := repo.SetStatus(ctx, stored.ID, models.CandidatePending, models.CandidateApproved, "admin-1")
	if err != nil {
		t.Fatalf("SetStatus: %v", err)
	}
	if claimed == nil {
		t.Fatal("first claim returned nothing")
	}
	if claimed.ReviewedBy != "admin-1" || claimed.ReviewedAt == nil {
		t.Errorf("review metadata not recorded: %+v", claimed)
	}

	second, err := repo.SetStatus(ctx, stored.ID, models.CandidatePending, models.CandidateApproved, "admin-2")
	if err != nil {
		t.Fatalf("SetStatus (second): %v", err)
	}
	if second != nil {
		t.Error("a second claim on the same row succeeded")
	}
}

func TestListByStatusRanksMostSuggestedFirst(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewDestinationCandidateRepository(db.Database)

	if _, err := repo.RecordSuggestion(ctx, candidate("quiet")); err != nil {
		t.Fatalf("RecordSuggestion: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := repo.RecordSuggestion(ctx, candidate("popular")); err != nil {
			t.Fatalf("RecordSuggestion: %v", err)
		}
	}

	got, err := repo.ListByStatus(ctx, models.CandidatePending, 10)
	if err != nil {
		t.Fatalf("ListByStatus: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}
	if got[0].Name != "popular" {
		t.Errorf("first row = %q, want the most-suggested place", got[0].Name)
	}

	count, err := repo.CountByStatus(ctx, models.CandidatePending)
	if err != nil {
		t.Fatalf("CountByStatus: %v", err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2", count)
	}
}

func TestFindByNormalizedNameMissingIsNotAnError(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewDestinationCandidateRepository(db.Database)

	got, err := repo.FindByNormalizedName(ctx, "never-seen")
	if err != nil {
		t.Fatalf("FindByNormalizedName: %v", err)
	}
	if got != nil {
		t.Errorf("got %+v, want nil", got)
	}
}
