package service

import (
	"testing"

	"github.com/realestayer/v4/internal/models"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// Approving a candidate must be a move between collections, not a
// re-derivation — if the budget were recomputed at approval time the reviewer
// would be approving one number and publishing another.
func TestCandidateToQueueRowSynthesisesTheStoredFigures(t *testing.T) {
	c := DiscoveryCandidate{
		Name:         "Banff",
		CountryCode:  "CA",
		Latitude:     51.17,
		Longitude:    -115.55,
		Description:  "A town in Alberta",
		ArticleTitle: "Banff,_Alberta",
	}

	row := candidateToQueueRow(c, 68, []string{"skiing"})

	if row.Status != models.CandidatePending {
		t.Errorf("status = %q, want pending", row.Status)
	}
	if row.NormalizedName != "banff" {
		t.Errorf("normalized_name = %q, want the dedupe key", row.NormalizedName)
	}
	if row.AvgDailyBudget != cityBudget("Banff", row.Region) {
		t.Errorf("budget = %v, want the same figure ConfirmAndInsert would store", row.AvgDailyBudget)
	}
	if row.PopularityScore != cityPopularity("Banff") {
		t.Errorf("popularity = %v", row.PopularityScore)
	}
	if row.SitelinkCount != 68 {
		t.Errorf("sitelink_count = %d, want the resolution signal preserved", row.SitelinkCount)
	}
	if len(row.FirstActivities) != 1 || row.FirstActivities[0] != "skiing" {
		t.Errorf("first_activities = %v, want the search that surfaced it", row.FirstActivities)
	}
}

// Country and region are filled in from the ISO code rather than left blank,
// so a reviewer never sees a bare country code.
func TestCandidateToQueueRowResolvesCountryAndRegion(t *testing.T) {
	row := candidateToQueueRow(DiscoveryCandidate{Name: "Churchill", CountryCode: "CA"}, 31, nil)

	if row.Country != isoCountryNames["CA"] {
		t.Errorf("country = %q, want the resolved name", row.Country)
	}
	if row.Region != regionForCountry("CA") {
		t.Errorf("region = %q, want the resolved region", row.Region)
	}
	if len(row.Categories) == 0 {
		t.Error("categories should default rather than be empty")
	}
}

// The candidate is shown to the searcher before review, so it has to render in
// the destination shape — but never with an ID, which would invite callers to
// treat it as a browsable row.
func TestAsDestinationCarriesNoID(t *testing.T) {
	c := models.DestinationCandidate{
		ID: primitive.NewObjectID(), Name: "Longyearbyen", Country: "Norway",
		Latitude: 78.2, Longitude: 15.6, AvgDailyBudget: 139,
	}

	d := c.AsDestination()

	if !d.ID.IsZero() {
		t.Error("a queued place must not carry a destinations ID")
	}
	if d.Name != "Longyearbyen" || d.AvgDailyBudget != 139 {
		t.Errorf("fields not carried through: %+v", d)
	}
	if d.Latitude == 0 || d.Longitude == 0 {
		t.Error("coordinates must survive — the card and map need them")
	}
}

// Without a queue configured the finder degrades to catalog-only rather than
// erroring, and review has nothing to act on.
func TestQueuelessDiscoveryReportsRatherThanPanics(t *testing.T) {
	s := &DestinationDiscoveryService{}

	if _, err := s.PendingCandidates(t.Context(), 10); err == nil {
		t.Error("PendingCandidates should report the queue is unconfigured")
	}
	if _, err := s.ReviewCandidate(t.Context(), primitive.NewObjectID(), true, "admin"); err == nil {
		t.Error("ReviewCandidate should report the queue is unconfigured")
	}
}
