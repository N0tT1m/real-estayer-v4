package service

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// TravelStatsService aggregates a user's trips into the kind of numbers that
// look great on a dashboard. All data is derived — no new collections.
type TravelStatsService struct {
	trips    *repository.TripRepository
	expenses *repository.TripExpenseRepository
}

func NewTravelStatsService(trips *repository.TripRepository, expenses *repository.TripExpenseRepository) *TravelStatsService {
	return &TravelStatsService{trips: trips, expenses: expenses}
}

// UserStats is the dashboard view-model.
type UserStats struct {
	TotalTrips       int                  `json:"total_trips"`
	UpcomingTrips    int                  `json:"upcoming_trips"`
	DaysOnTheRoad    int                  `json:"days_on_the_road"`
	FutureDays       int                  `json:"future_days"`
	Countries        []string             `json:"countries"`
	Cities           []string             `json:"cities"`
	TopMonths        []MonthCount         `json:"top_months"`
	CarbonKg         float64              `json:"carbon_kg"`
	TotalSpent       map[string]float64   `json:"total_spent_by_currency"`
	AnnualHeatmap    map[int][]MonthCount `json:"annual_heatmap"` // year → month counts
	FavoriteCategory string               `json:"favorite_category,omitempty"`
}

// MonthCount is one row of "March: 12 days traveled."
type MonthCount struct {
	Month int `json:"month"`
	Days  int `json:"days"`
}

// ForUser computes everything in one pass over the user's trips.
func (s *TravelStatsService) ForUser(ctx context.Context, userIDHex string) (*UserStats, error) {
	uid, err := primitive.ObjectIDFromHex(userIDHex)
	if err != nil {
		return nil, errors.New("invalid user id")
	}
	trips, _, err := s.trips.FindByUserID(ctx, uid, 1, 1000)
	if err != nil {
		return nil, err
	}

	out := &UserStats{
		Countries:     nil,
		Cities:        nil,
		AnnualHeatmap: map[int][]MonthCount{},
		TotalSpent:    map[string]float64{},
	}
	now := time.Now()
	countrySet := map[string]struct{}{}
	citySet := map[string]struct{}{}
	monthSum := map[int]int{} // overall per-month day totals
	categoryCount := map[string]int{}
	carbon := NewCarbonService()

	for _, t := range trips {
		out.TotalTrips++
		if t.StartDate.After(now) {
			out.UpcomingTrips++
		}
		days := tripDays(t)
		if t.EndDate.Before(now) {
			out.DaysOnTheRoad += days
		} else if t.StartDate.After(now) {
			out.FutureDays += days
		} else {
			// In-progress: count elapsed days.
			elapsed := int(now.Sub(t.StartDate).Hours()/24) + 1
			if elapsed < 0 {
				elapsed = 0
			}
			if elapsed > days {
				elapsed = days
			}
			out.DaysOnTheRoad += elapsed
			out.FutureDays += days - elapsed
		}

		// Countries and cities from destinations — use the Name field and fall
		// back to manual parsing.
		for _, d := range t.Destinations {
			if d.Name != "" {
				citySet[strings.TrimSpace(d.Name)] = struct{}{}
			}
		}

		// Category popularity from items.
		for _, it := range t.Items {
			categoryCount[string(it.Type)]++
		}

		// Carbon per trip.
		out.CarbonKg += carbon.EstimateTrip(&t).TotalKg

		// Month breakdown: each trip contributes its days to the starting
		// month for simplicity.
		m := int(t.StartDate.Month())
		monthSum[m] += days
		y := t.StartDate.Year()
		merge := out.AnnualHeatmap[y]
		found := false
		for i := range merge {
			if merge[i].Month == m {
				merge[i].Days += days
				found = true
				break
			}
		}
		if !found {
			merge = append(merge, MonthCount{Month: m, Days: days})
		}
		out.AnnualHeatmap[y] = merge

		// Expenses by currency.
		expenses, _ := s.expenses.ListForTrip(ctx, t.ID)
		for _, e := range expenses {
			out.TotalSpent[e.Currency] += e.Amount
		}
	}

	for c := range countrySet {
		out.Countries = append(out.Countries, c)
	}
	for c := range citySet {
		out.Cities = append(out.Cities, c)
	}
	sort.Strings(out.Countries)
	sort.Strings(out.Cities)

	// Top-3 months.
	rows := make([]MonthCount, 0, len(monthSum))
	for m, d := range monthSum {
		rows = append(rows, MonthCount{Month: m, Days: d})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Days > rows[j].Days })
	if len(rows) > 3 {
		rows = rows[:3]
	}
	out.TopMonths = rows
	out.CarbonKg = round1(out.CarbonKg)

	// Favorite category.
	maxN := 0
	for k, n := range categoryCount {
		if n > maxN {
			maxN, out.FavoriteCategory = n, k
		}
	}
	return out, nil
}

func tripDays(t models.Trip) int {
	if t.EndDate.Before(t.StartDate) {
		return 1
	}
	d := int(t.EndDate.Sub(t.StartDate).Hours()/24) + 1
	if d < 1 {
		d = 1
	}
	return d
}

// Profile classifies the user based on destination categories / item types
// they've chosen. The buckets are heuristic — think personality quiz, not
// science.
type Profile struct {
	Label       string   `json:"label"` // "City explorer" etc.
	Description string   `json:"description"`
	Tags        []string `json:"tags,omitempty"`
}

func (s *TravelStatsService) ProfileFor(ctx context.Context, userIDHex string, collections *repository.CollectionRepository) (*Profile, error) {
	uid, err := primitive.ObjectIDFromHex(userIDHex)
	if err != nil {
		return nil, errors.New("invalid user id")
	}
	trips, _, err := s.trips.FindByUserID(ctx, uid, 1, 500)
	if err != nil {
		return nil, err
	}
	tags := map[string]int{}
	for _, t := range trips {
		for _, d := range t.Destinations {
			key := strings.ToLower(d.Name)
			for _, k := range []string{"beach", "ski", "mountain", "city", "island", "park"} {
				if strings.Contains(key, k) {
					tags[k]++
				}
			}
		}
		for _, it := range t.Items {
			tags[string(it.Type)]++
		}
	}
	if len(trips) == 0 {
		return &Profile{
			Label:       "Aspiring traveler",
			Description: "Plan your first trip and we'll size up your style.",
		}, nil
	}

	// Pick a label from the loudest signal.
	label := "Eclectic traveler"
	desc := "A bit of everything — comfortable in cities, on beaches, and on trails."
	picked := ""
	maxN := 0
	for k, n := range tags {
		if n > maxN {
			maxN, picked = n, k
		}
	}
	switch picked {
	case "beach", "island":
		label, desc = "Beach-seeker", "Coastline, cocktails, and sunsets. You optimise for shoreline."
	case "mountain", "ski":
		label, desc = "Alpine wanderer", "Elevation and crisp air; you like trails more than traffic."
	case "city":
		label, desc = "City explorer", "Dense itineraries, museum fatigue, and late-night food finds."
	case "park":
		label, desc = "Nature-first", "Parks, hikes, and open sky over hotel lobbies."
	case "flight":
		label, desc = "Long-haul adventurer", "You treat timezones like suggestions."
	}
	// Convert the map to a slice so order is stable.
	type kv struct {
		k string
		n int
	}
	all := make([]kv, 0, len(tags))
	for k, n := range tags {
		all = append(all, kv{k, n})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].n > all[j].n })
	topTags := make([]string, 0, 5)
	for i, p := range all {
		if i >= 5 {
			break
		}
		topTags = append(topTags, p.k)
	}
	return &Profile{Label: label, Description: desc, Tags: topTags}, nil
}
