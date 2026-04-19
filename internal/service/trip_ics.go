package service

import (
	"fmt"
	"strings"
	"time"

	"github.com/realestayer/v4/internal/models"
)

// RenderTripICS renders a trip as an iCalendar (.ics) document. Each TripItem
// with a StartTime becomes a VEVENT; items without times become all-day events
// on the trip start date so nothing is silently dropped.
//
// Designed for Apple Calendar / Google Calendar subscription or one-off
// import. No timezone component (VTIMEZONE) is emitted — times are written
// as their stored time and the client's timezone interprets them. Upgrading
// to explicit VTIMEZONE blocks is straightforward when we start storing per-
// item IANA zones.
func RenderTripICS(trip *models.Trip) string {
	if trip == nil {
		return ""
	}
	var b strings.Builder
	b.WriteString("BEGIN:VCALENDAR\r\n")
	b.WriteString("VERSION:2.0\r\n")
	b.WriteString("PRODID:-//Real-Estayer//Trip//EN\r\n")
	b.WriteString("CALSCALE:GREGORIAN\r\n")
	b.WriteString("METHOD:PUBLISH\r\n")
	fmt.Fprintf(&b, "X-WR-CALNAME:%s\r\n", icsEscape(trip.Name))
	if trip.Description != "" {
		fmt.Fprintf(&b, "X-WR-CALDESC:%s\r\n", icsEscape(trip.Description))
	}

	stamp := time.Now().UTC().Format("20060102T150405Z")

	// A whole-trip envelope event so the calendar shows the span.
	writeEvent(&b, ICSEvent{
		UID:         fmt.Sprintf("trip-%s@real-estayer", trip.ID.Hex()),
		Stamp:       stamp,
		Summary:     fmt.Sprintf("Trip: %s", trip.Name),
		Description: trip.Description,
		StartAllDay: trip.StartDate,
		EndAllDay:   trip.EndDate.Add(24 * time.Hour), // ics DTEND is exclusive
	})

	for _, it := range trip.Items {
		ev := ICSEvent{
			UID:         fmt.Sprintf("trip-%s-item-%s@real-estayer", trip.ID.Hex(), it.ID.Hex()),
			Stamp:       stamp,
			Summary:     fmt.Sprintf("%s: %s", titleCaseWord(string(it.Type)), it.Title),
			Description: itemDescription(it),
			Location:    itemLocation(it),
		}
		if it.StartTime != nil {
			ev.Start = *it.StartTime
			if it.EndTime != nil {
				ev.End = *it.EndTime
			} else {
				ev.End = ev.Start.Add(1 * time.Hour)
			}
		} else {
			ev.StartAllDay = trip.StartDate
			ev.EndAllDay = trip.StartDate.Add(24 * time.Hour)
		}
		writeEvent(&b, ev)
	}

	b.WriteString("END:VCALENDAR\r\n")
	return b.String()
}

// ICSEvent is the internal struct we render. Either (Start,End) or
// (StartAllDay,EndAllDay) must be populated.
type ICSEvent struct {
	UID         string
	Stamp       string
	Summary     string
	Description string
	Location    string
	Start       time.Time
	End         time.Time
	StartAllDay time.Time
	EndAllDay   time.Time
}

func writeEvent(b *strings.Builder, e ICSEvent) {
	b.WriteString("BEGIN:VEVENT\r\n")
	fmt.Fprintf(b, "UID:%s\r\n", e.UID)
	fmt.Fprintf(b, "DTSTAMP:%s\r\n", e.Stamp)
	if !e.Start.IsZero() {
		fmt.Fprintf(b, "DTSTART:%s\r\n", e.Start.UTC().Format("20060102T150405Z"))
		fmt.Fprintf(b, "DTEND:%s\r\n", e.End.UTC().Format("20060102T150405Z"))
	} else {
		fmt.Fprintf(b, "DTSTART;VALUE=DATE:%s\r\n", e.StartAllDay.Format("20060102"))
		fmt.Fprintf(b, "DTEND;VALUE=DATE:%s\r\n", e.EndAllDay.Format("20060102"))
	}
	fmt.Fprintf(b, "SUMMARY:%s\r\n", icsEscape(e.Summary))
	if e.Description != "" {
		fmt.Fprintf(b, "DESCRIPTION:%s\r\n", icsEscape(e.Description))
	}
	if e.Location != "" {
		fmt.Fprintf(b, "LOCATION:%s\r\n", icsEscape(e.Location))
	}
	b.WriteString("END:VEVENT\r\n")
}

// icsEscape handles the escape rules in RFC 5545 §3.3.11.
func icsEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, ",", `\,`)
	s = strings.ReplaceAll(s, ";", `\;`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return s
}

func titleCaseWord(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func itemDescription(it models.TripItem) string {
	parts := []string{}
	if it.Notes != "" {
		parts = append(parts, it.Notes)
	}
	if it.Provider != "" {
		parts = append(parts, "Provider: "+it.Provider)
	}
	if it.Price != nil && it.Price.Amount > 0 {
		parts = append(parts, fmt.Sprintf("Price: %.2f %s", it.Price.Amount, it.Price.Currency))
	}
	return strings.Join(parts, "\n")
}

func itemLocation(it models.TripItem) string {
	if it.Details == nil {
		return ""
	}
	if v, ok := it.Details["location"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	if v, ok := it.Details["address"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
