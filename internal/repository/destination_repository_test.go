package repository

import (
	"testing"

	"github.com/realestayer/v4/internal/dbtest"
	"github.com/realestayer/v4/internal/models"
)

// Integration tests for category filtering. Set MONGODB_TEST_URI to enable;
// otherwise they skip.

// seedRankingFixtures inserts destinations whose `categories` arrays put
// "nature" at a different position each time, mirroring the real seed data:
// a genuine nature destination lists it first, a city that merely has parks
// lists it last.
func seedRankingFixtures(t *testing.T, repo *DestinationRepository) {
	t.Helper()
	ctx := dbtest.Context(t)

	fixtures := []*models.Destination{
		// Lowest popularity of the three, but "nature" is its primary tag.
		{Name: "Queenstown", Categories: []string{"nature", "adventure"}, PopularityScore: 70},
		{Name: "Seattle", Categories: []string{"city", "nature", "food"}, PopularityScore: 80},
		// Highest popularity, but "nature" is only its third tag — this is
		// the document that used to lead the results.
		{Name: "Zurich", Categories: []string{"city", "luxury", "nature"}, PopularityScore: 90},
		// Must not appear in a "nature" query at all.
		{Name: "Paris", Categories: []string{"city", "romantic"}, PopularityScore: 95},
	}
	for _, d := range fixtures {
		if err := repo.Create(ctx, d); err != nil {
			t.Fatalf("Create(%s): %v", d.Name, err)
		}
	}
}

func names(dests []models.Destination) []string {
	out := make([]string, 0, len(dests))
	for _, d := range dests {
		out = append(out, d.Name)
	}
	return out
}

func equalNames(got []models.Destination, want []string) bool {
	g := names(got)
	if len(g) != len(want) {
		return false
	}
	for i := range g {
		if g[i] != want[i] {
			return false
		}
	}
	return true
}

// The bug: ?category=nature&sort=popularity ranked Zurich (nature third)
// above Queenstown (nature first) purely on popularity_score, so the page
// led with places that aren't nature destinations.
func TestFindCategoryRanksPrimaryTagFirst(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewDestinationRepository(db.Database)
	seedRankingFixtures(t, repo)

	got, total, err := repo.Find(ctx, models.DestinationFilter{
		Category: "nature",
		SortBy:   "popularity",
		Limit:    24,
	})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if total != 3 {
		t.Errorf("total = %d, want 3 (Paris must not match)", total)
	}

	want := []string{"Queenstown", "Seattle", "Zurich"}
	if !equalNames(got, want) {
		t.Errorf("order = %v, want %v (rank by tag position, then popularity)", names(got), want)
	}
}

// Within a rank tier the caller's sort still decides. Both documents here
// carry "nature" second, so popularity orders them.
func TestFindCategorySortAppliesWithinTier(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewDestinationRepository(db.Database)

	fixtures := []*models.Destination{
		{Name: "Lisbon", Categories: []string{"city", "nature"}, PopularityScore: 60},
		{Name: "Porto", Categories: []string{"city", "nature"}, PopularityScore: 85},
	}
	for _, d := range fixtures {
		if err := repo.Create(ctx, d); err != nil {
			t.Fatalf("Create(%s): %v", d.Name, err)
		}
	}

	got, _, err := repo.Find(ctx, models.DestinationFilter{
		Category: "nature",
		SortBy:   "popularity",
		Limit:    24,
	})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if want := []string{"Porto", "Lisbon"}; !equalNames(got, want) {
		t.Errorf("order = %v, want %v", names(got), want)
	}
}

// The ranked path goes through an aggregation, so paging is $skip/$limit
// rather than Find options. Successive offsets must not repeat or drop a
// document — the explore page's "load more" issues them as separate queries.
func TestFindCategoryPagingIsStable(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewDestinationRepository(db.Database)
	seedRankingFixtures(t, repo)

	var seen []string
	for offset := range 3 {
		page, _, err := repo.Find(ctx, models.DestinationFilter{
			Category: "nature",
			SortBy:   "popularity",
			Limit:    1,
			Offset:   offset,
		})
		if err != nil {
			t.Fatalf("Find(offset=%d): %v", offset, err)
		}
		if len(page) != 1 {
			t.Fatalf("offset %d returned %d docs, want 1", offset, len(page))
		}
		seen = append(seen, page[0].Name)
	}

	want := []string{"Queenstown", "Seattle", "Zurich"}
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("paged order = %v, want %v", seen, want)
			break
		}
	}
}

// FindByCategory backs the "similar destinations" strip on a destination
// page, and routes through the same ranked path.
func TestFindByCategoryRanksPrimaryTagFirst(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewDestinationRepository(db.Database)
	seedRankingFixtures(t, repo)

	got, err := repo.FindByCategory(ctx, "nature", 8)
	if err != nil {
		t.Fatalf("FindByCategory: %v", err)
	}
	if want := []string{"Queenstown", "Seattle", "Zurich"}; !equalNames(got, want) {
		t.Errorf("order = %v, want %v", names(got), want)
	}
}

// Ranking added a branch to Find; the unfiltered path must still sort purely
// by the requested field.
func TestFindWithoutCategoryStillSortsByPopularity(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewDestinationRepository(db.Database)
	seedRankingFixtures(t, repo)

	got, total, err := repo.Find(ctx, models.DestinationFilter{SortBy: "popularity", Limit: 24})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if total != 4 {
		t.Errorf("total = %d, want 4", total)
	}
	if want := []string{"Paris", "Zurich", "Seattle", "Queenstown"}; !equalNames(got, want) {
		t.Errorf("order = %v, want %v", names(got), want)
	}
}

// The seeder populates categories but never best_for or best_months, so the
// explore page needs to know those filters have nothing behind them.
func TestFilterAvailabilityReportsEmptyFields(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewDestinationRepository(db.Database)
	seedRankingFixtures(t, repo)

	got, err := repo.FilterAvailability(ctx)
	if err != nil {
		t.Fatalf("FilterAvailability: %v", err)
	}
	if got.BestFor {
		t.Error("BestFor = true, want false (no fixture sets best_for)")
	}
	if got.Months {
		t.Error("Months = true, want false (no fixture sets best_months)")
	}
}

// And it must flip once the data exists, so the controls come back on their
// own if a future seed populates them.
func TestFilterAvailabilityDetectsPopulatedFields(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewDestinationRepository(db.Database)
	seedRankingFixtures(t, repo)

	if err := repo.Create(ctx, &models.Destination{
		Name:       "Lisbon",
		Categories: []string{"city"},
		BestFor:    []string{"couples"},
		BestMonths: []int{5, 6},
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FilterAvailability(ctx)
	if err != nil {
		t.Fatalf("FilterAvailability: %v", err)
	}
	if !got.BestFor {
		t.Error("BestFor = false, want true")
	}
	if !got.Months {
		t.Error("Months = false, want true")
	}
}

// An empty array is not data. A document storing best_for: [] must not switch
// the control back on.
func TestFilterAvailabilityIgnoresEmptyArrays(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewDestinationRepository(db.Database)

	if err := repo.Create(ctx, &models.Destination{
		Name:       "Porto",
		Categories: []string{"city"},
		BestFor:    []string{},
		BestMonths: []int{},
	}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.FilterAvailability(ctx)
	if err != nil {
		t.Fatalf("FilterAvailability: %v", err)
	}
	if got.BestFor || got.Months {
		t.Errorf("availability = %+v, want both false for empty arrays", got)
	}
}

// A category filter has to compose with the other filters rather than
// replacing them.
func TestFindCategoryComposesWithOtherFilters(t *testing.T) {
	db := dbtest.New(t)
	ctx := dbtest.Context(t)
	repo := NewDestinationRepository(db.Database)

	fixtures := []*models.Destination{
		{Name: "Queenstown", Region: "Oceania", Categories: []string{"nature"}, PopularityScore: 70},
		{Name: "Reykjavik", Region: "Europe", Categories: []string{"nature"}, PopularityScore: 90},
	}
	for _, d := range fixtures {
		if err := repo.Create(ctx, d); err != nil {
			t.Fatalf("Create(%s): %v", d.Name, err)
		}
	}

	got, total, err := repo.Find(ctx, models.DestinationFilter{
		Category: "nature",
		Region:   "Europe",
		Limit:    24,
	})
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if total != 1 {
		t.Errorf("total = %d, want 1", total)
	}
	if want := []string{"Reykjavik"}; !equalNames(got, want) {
		t.Errorf("results = %v, want %v", names(got), want)
	}
}
