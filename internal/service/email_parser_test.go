package service

import (
	"context"
	"testing"

	"github.com/realestayer/v4/internal/models"
)

func TestParseAirbnbConfirmation(t *testing.T) {
	body := `From: Airbnb <noreply@airbnb.com>
Subject: You're going to Lisbon!

Trip to Lisbon, Portugal
Confirmation code: HMAB5K2Q3X
Check-in: June 12, 2025
Check-out: June 18, 2025

Total: $720.00
`
	p := NewEmailParserService(nil)
	out, err := p.Parse(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	if out.Source != "airbnb" || len(out.Items) != 1 {
		t.Fatalf("unexpected: %+v", out)
	}
	it := out.Items[0]
	if it.Type != models.TripItemTypeListing || it.StartTime == nil || it.EndTime == nil {
		t.Errorf("expected listing with both times set, got %+v", it)
	}
	if it.ReferenceID != "HMAB5K2Q3X" {
		t.Errorf("confirmation code missing, got %q", it.ReferenceID)
	}
	if it.Price == nil || it.Price.Amount != 720 {
		t.Errorf("price not parsed, got %+v", it.Price)
	}
}

func TestParseBookingConfirmation(t *testing.T) {
	body := `From: Booking.com
Subject: Your booking is confirmed

Booked: Hotel das Artes
Check-in: July 1, 2025
Check-out: July 4, 2025
Booking reference: 1234.567.890

Total: €440.00`
	p := NewEmailParserService(nil)
	out, err := p.Parse(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	if out.Source != "booking.com" || len(out.Items) != 1 {
		t.Fatalf("unexpected: %+v", out)
	}
	if out.Items[0].Price == nil || out.Items[0].Price.Currency != "EUR" {
		t.Errorf("expected EUR price, got %+v", out.Items[0].Price)
	}
}

func TestParseFlightConfirmation(t *testing.T) {
	body := `Your flight is confirmed.

United Airlines UA 283
SFO → NRT
Departure: May 1, 2025, 10:15 AM
Confirmation code: ABC123
Total: $842.50`
	p := NewEmailParserService(nil)
	out, err := p.Parse(context.Background(), body)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Items) != 1 {
		t.Fatalf("expected 1 item, got %+v", out.Items)
	}
	it := out.Items[0]
	if it.Type != models.TripItemTypeFlight {
		t.Errorf("expected flight; got %s", it.Type)
	}
	if it.Details["flight_number"] != "UA283" {
		t.Errorf("flight number = %v", it.Details["flight_number"])
	}
	if it.Details["from_iata"] != "SFO" || it.Details["to_iata"] != "NRT" {
		t.Errorf("airports = %v / %v", it.Details["from_iata"], it.Details["to_iata"])
	}
}

func TestParseEmptyErrors(t *testing.T) {
	p := NewEmailParserService(nil)
	if _, err := p.Parse(context.Background(), ""); err == nil {
		t.Errorf("empty email should error")
	}
}

func TestParseUnknownWithoutAIErrors(t *testing.T) {
	p := NewEmailParserService(nil)
	if _, err := p.Parse(context.Background(), "random text with no bookings"); err == nil {
		t.Errorf("unmatched email without AI fallback should error")
	}
}

func TestFindConfirmationVariants(t *testing.T) {
	cases := map[string]string{
		"Confirmation code: ABCDEF":       "ABCDEF",
		"Confirmation number ZZZ9999":    "ZZZ9999",
		"Your reservation code: XX1111":  "XX1111",
		"Booking ref: R4N50M":            "R4N50M",
		"just no confirmation here":      "",
	}
	for in, want := range cases {
		if got := findConfirmation(in); got != want {
			t.Errorf("findConfirmation(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestFindCurrencyFromSymbols(t *testing.T) {
	cases := map[string]string{
		"Total: €120":   "EUR",
		"Total: £80":    "GBP",
		"Total: ¥8500":  "JPY",
		"Total: $99":    "USD",
	}
	for in, want := range cases {
		if got := findCurrency(in); got != want {
			t.Errorf("findCurrency(%q) = %q; want %q", in, got, want)
		}
	}
}
