package service

import (
	"testing"
	"time"

	"github.com/realestayer/v4/internal/models"
)

func TestTripDays(t *testing.T) {
	base := time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		trip models.Trip
		want int
	}{
		{"same day", models.Trip{StartDate: base, EndDate: base}, 1},
		{"three nights", models.Trip{StartDate: base, EndDate: base.AddDate(0, 0, 3)}, 4},
		{"end before start", models.Trip{StartDate: base, EndDate: base.AddDate(0, 0, -1)}, 1},
	}
	for _, c := range cases {
		if got := tripDays(c.trip); got != c.want {
			t.Errorf("%s: tripDays = %d; want %d", c.name, got, c.want)
		}
	}
}
