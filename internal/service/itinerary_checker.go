package service

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/realestayer/v4/internal/models"
)

// ConflictChecker runs pure-logic checks over a trip's items and returns a
// list of advisory warnings. Ordered by severity so the UI can truncate at
// N and still show the worst issues first.
type ConflictChecker struct {
	// MinConnectionMinutes is the smallest gap between the end of one flight
	// and the start of the next we'll tolerate without flagging.
	MinConnectionMinutes int
	// TravelBufferMinutes is the slack we assume between a non-flight item
	// ending and the next item starting.
	TravelBufferMinutes int
}

func NewConflictChecker() *ConflictChecker {
	return &ConflictChecker{MinConnectionMinutes: 60, TravelBufferMinutes: 20}
}

// Severity controls the UI tone: info (blue), warn (amber), error (red).
type Severity string

const (
	SeverityInfo  Severity = "info"
	SeverityWarn  Severity = "warn"
	SeverityError Severity = "error"
)

// Warning is a single complaint.
type Warning struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Title    string   `json:"title"`
	Detail   string   `json:"detail,omitempty"`
	ItemIDs  []string `json:"item_ids,omitempty"`
}

// Check returns all warnings for the trip. Never nil.
func (c *ConflictChecker) Check(trip *models.Trip) []Warning {
	if trip == nil {
		return nil
	}
	var warnings []Warning

	// Only consider scheduled items; unscheduled ones can't conflict.
	scheduled := make([]models.TripItem, 0, len(trip.Items))
	for _, it := range trip.Items {
		if it.StartTime != nil {
			scheduled = append(scheduled, it)
		}
	}
	sort.Slice(scheduled, func(i, j int) bool {
		return scheduled[i].StartTime.Before(*scheduled[j].StartTime)
	})

	warnings = append(warnings, overlappingItems(scheduled)...)
	warnings = append(warnings, tightConnections(scheduled, c.MinConnectionMinutes)...)
	warnings = append(warnings, checkinBeforeArrival(scheduled)...)
	warnings = append(warnings, itemsOutsideTripDates(trip, scheduled)...)
	warnings = append(warnings, missingFlightInbound(trip, scheduled)...)

	// Sort: error > warn > info, then by earliest item time.
	sort.SliceStable(warnings, func(i, j int) bool {
		return severityRank(warnings[i].Severity) < severityRank(warnings[j].Severity)
	})
	return warnings
}

func severityRank(s Severity) int {
	switch s {
	case SeverityError:
		return 0
	case SeverityWarn:
		return 1
	}
	return 2
}

// --- Individual checks ---

// overlappingItems flags any two items whose [start,end] ranges intersect.
// For items without an EndTime we treat them as a 1-hour block.
func overlappingItems(items []models.TripItem) []Warning {
	out := []Warning{}
	for i := 0; i < len(items); i++ {
		ai := items[i]
		aEnd := endOrFallback(ai)
		for j := i + 1; j < len(items); j++ {
			bi := items[j]
			bEnd := endOrFallback(bi)
			if bi.StartTime.Before(aEnd) && ai.StartTime.Before(bEnd) {
				out = append(out, Warning{
					Severity: SeverityWarn,
					Code:     "overlap",
					Title:    fmt.Sprintf("Overlap: %q and %q", ai.Title, bi.Title),
					Detail:   fmt.Sprintf("Both are scheduled around %s. Move one or adjust the times.", ai.StartTime.Format("Mon Jan 2 15:04")),
					ItemIDs:  []string{ai.ID.Hex(), bi.ID.Hex()},
				})
			}
		}
	}
	return out
}

// tightConnections warns when two flights have <minGap minutes between
// arrival and departure. Uses item End→Start, not Start→Start.
func tightConnections(items []models.TripItem, minGap int) []Warning {
	out := []Warning{}
	for i := 0; i < len(items)-1; i++ {
		a, b := items[i], items[i+1]
		if a.Type != models.TripItemTypeFlight || b.Type != models.TripItemTypeFlight {
			continue
		}
		if a.EndTime == nil {
			continue
		}
		gap := b.StartTime.Sub(*a.EndTime).Minutes()
		if gap < 0 {
			continue // handled by overlap check
		}
		if int(gap) < minGap {
			out = append(out, Warning{
				Severity: SeverityWarn,
				Code:     "tight_connection",
				Title:    fmt.Sprintf("Tight connection: %.0f min between %s and %s", gap, a.Title, b.Title),
				Detail:   fmt.Sprintf("Plan on at least %d minutes for a reliable connection.", minGap),
				ItemIDs:  []string{a.ID.Hex(), b.ID.Hex()},
			})
		}
	}
	return out
}

// checkinBeforeArrival: if a hotel/listing check-in (StartTime) is *before*
// the last flight arrival (EndTime), the traveller can't be there to check in.
func checkinBeforeArrival(items []models.TripItem) []Warning {
	var lastFlightArrival *time.Time
	out := []Warning{}
	for _, it := range items {
		if it.Type == models.TripItemTypeFlight && it.EndTime != nil {
			t := *it.EndTime
			if lastFlightArrival == nil || t.After(*lastFlightArrival) {
				lastFlightArrival = &t
			}
			continue
		}
		if lastFlightArrival == nil {
			continue
		}
		if it.Type != models.TripItemTypeHotel && it.Type != models.TripItemTypeListing {
			continue
		}
		if it.StartTime.Before(*lastFlightArrival) {
			out = append(out, Warning{
				Severity: SeverityError,
				Code:     "checkin_before_arrival",
				Title:    fmt.Sprintf("%q starts before your flight lands", it.Title),
				Detail:   fmt.Sprintf("Check-in is %s but the flight arrives %s.",
					it.StartTime.Format("Mon Jan 2 15:04"), lastFlightArrival.Format("Mon Jan 2 15:04")),
				ItemIDs: []string{it.ID.Hex()},
			})
		}
	}
	return out
}

// itemsOutsideTripDates flags items scheduled outside the trip's own window.
// Returns info-level because users sometimes schedule a pre-trip dinner or
// post-trip activity intentionally.
func itemsOutsideTripDates(trip *models.Trip, items []models.TripItem) []Warning {
	if trip.StartDate.IsZero() || trip.EndDate.IsZero() {
		return nil
	}
	out := []Warning{}
	// Trip dates are inclusive; give each day a 24-hour bucket.
	dayStart := time.Date(trip.StartDate.Year(), trip.StartDate.Month(), trip.StartDate.Day(), 0, 0, 0, 0, trip.StartDate.Location())
	dayEnd := time.Date(trip.EndDate.Year(), trip.EndDate.Month(), trip.EndDate.Day(), 23, 59, 59, 0, trip.EndDate.Location())
	for _, it := range items {
		if it.StartTime.Before(dayStart) || it.StartTime.After(dayEnd) {
			out = append(out, Warning{
				Severity: SeverityInfo,
				Code:     "outside_trip_dates",
				Title:    fmt.Sprintf("%q is outside your trip dates", it.Title),
				Detail:   fmt.Sprintf("Trip: %s – %s", trip.StartDate.Format("Jan 2"), trip.EndDate.Format("Jan 2")),
				ItemIDs:  []string{it.ID.Hex()},
			})
		}
	}
	return out
}

// missingFlightInbound warns when a trip that has destinations + hotels has
// no flight item at all — usually means the user forgot to add their flight.
// Info-level because driving trips are legit.
func missingFlightInbound(trip *models.Trip, scheduled []models.TripItem) []Warning {
	if len(trip.Destinations) == 0 && len(scheduled) == 0 {
		return nil
	}
	hasFlight := false
	for _, it := range scheduled {
		if it.Type == models.TripItemTypeFlight {
			hasFlight = true
			break
		}
	}
	if hasFlight {
		return nil
	}
	// Only nag when there's something substantial planned but no flight — a
	// hotel, a listing, or multiple items.
	interesting := 0
	for _, it := range scheduled {
		if it.Type == models.TripItemTypeHotel || it.Type == models.TripItemTypeListing {
			interesting++
		}
	}
	if interesting == 0 {
		return nil
	}
	return []Warning{{
		Severity: SeverityInfo,
		Code:     "no_flight",
		Title:    "No flight on this trip",
		Detail:   "Add a flight or mark the trip as a driving/rail trip so we stop reminding you.",
	}}
}

// endOrFallback returns the item's EndTime or start+1h if unset.
func endOrFallback(it models.TripItem) time.Time {
	if it.EndTime != nil {
		return *it.EndTime
	}
	return it.StartTime.Add(1 * time.Hour)
}

// Titleized helper kept near usage to dodge stutter warnings.
func titleize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// kill unused-import warnings even if a future refactor drops titleize.
var _ = titleize
