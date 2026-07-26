package service

import (
	"testing"
	"time"

	"github.com/realestayer/v4/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Summarize is pure-logic — skip the repo and call it directly on a fixture.

func TestPollSummarizeOrderingAndScore(t *testing.T) {
	base := time.Now()
	o1 := models.PollOption{ID: primitive.NewObjectID(), Start: base, End: base.Add(48 * time.Hour), Label: "Weekend A"}
	o2 := models.PollOption{ID: primitive.NewObjectID(), Start: base.Add(7 * 24 * time.Hour), End: base.Add(9 * 24 * time.Hour), Label: "Weekend B"}
	o3 := models.PollOption{ID: primitive.NewObjectID(), Start: base.Add(14 * 24 * time.Hour), End: base.Add(16 * 24 * time.Hour), Label: "Weekend C"}
	poll := &models.AvailabilityPoll{
		Options: []models.PollOption{o1, o2, o3},
		Responses: []models.PollResponse{
			{DisplayName: "Alex", OptionVotes: map[string]string{
				o1.ID.Hex(): "yes", o2.ID.Hex(): "maybe", o3.ID.Hex(): "no",
			}},
			{DisplayName: "Sam", OptionVotes: map[string]string{
				o1.ID.Hex(): "yes", o2.ID.Hex(): "yes", o3.ID.Hex(): "no",
			}},
			{DisplayName: "Jo", OptionVotes: map[string]string{
				o1.ID.Hex(): "maybe", o2.ID.Hex(): "yes", o3.ID.Hex(): "maybe",
			}},
		},
	}
	s := &PollService{}
	summary := s.Summarize(poll)
	if len(summary) != 3 {
		t.Fatalf("expected 3 rows, got %d", len(summary))
	}
	// o2 has 2 yes + 1 maybe → 2.5. o1 has 2 yes + 1 maybe → 2.5. Tie broken
	// stably by order, so o1 comes first (it appears first in Options).
	if summary[0].Option.ID != o1.ID && summary[0].Option.ID != o2.ID {
		t.Errorf("top score should be Weekend A or B, got %s", summary[0].Option.Label)
	}
	// o3 ranks last: 0 yes + 1 maybe + 2 no → 0.5.
	if summary[2].Option.ID != o3.ID {
		t.Errorf("bottom should be Weekend C, got %s", summary[2].Option.Label)
	}
	if summary[2].No != 2 {
		t.Errorf("expected 2 'no' on Weekend C, got %d", summary[2].No)
	}
}

func TestPollSummarizeIgnoresUnknownOptions(t *testing.T) {
	opt := models.PollOption{ID: primitive.NewObjectID()}
	poll := &models.AvailabilityPoll{
		Options: []models.PollOption{opt},
		Responses: []models.PollResponse{
			{DisplayName: "x", OptionVotes: map[string]string{
				opt.ID.Hex():     "yes",
				"deadbeef-ghost": "yes", // stale / tampered option id
			}},
		},
	}
	summary := (&PollService{}).Summarize(poll)
	if len(summary) != 1 {
		t.Fatalf("only real options should appear, got %d", len(summary))
	}
	if summary[0].Yes != 1 {
		t.Errorf("expected 1 yes on real option")
	}
}

func TestRandomPollSlugShape(t *testing.T) {
	s, err := randomPollSlug()
	if err != nil {
		t.Fatal(err)
	}
	if len(s) < 12 {
		t.Errorf("slug too short: %q", s)
	}
	for _, r := range s {
		ok := (r >= 'a' && r <= 'z') || (r >= '2' && r <= '7')
		if !ok {
			t.Errorf("unexpected slug char %q in %q", r, s)
		}
	}
}
