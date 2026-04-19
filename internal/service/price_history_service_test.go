package service

import (
	"strings"
	"testing"
	"time"

	"github.com/realestayer/v4/internal/models"
)

func TestRenderSparklineSVGEmpty(t *testing.T) {
	s := (&PriceHistoryService{}).RenderSparklineSVG(nil)
	if !strings.Contains(s, "<svg") || !strings.Contains(s, "No price history") {
		t.Fatalf("empty sparkline should say no history: %s", s)
	}
}

func TestRenderSparklineSVGWithPoints(t *testing.T) {
	pts := []models.ListingPricePoint{
		{Price: 100, CapturedAt: time.Now().Add(-72 * time.Hour)},
		{Price: 120, CapturedAt: time.Now().Add(-48 * time.Hour)},
		{Price: 80, CapturedAt: time.Now().Add(-24 * time.Hour)},
		{Price: 90, CapturedAt: time.Now()},
	}
	s := (&PriceHistoryService{}).RenderSparklineSVG(pts)
	if !strings.Contains(s, "<path") {
		t.Fatalf("sparkline should contain a path for %d points: %s", len(pts), s)
	}
	if !strings.Contains(s, "<circle") {
		t.Fatalf("sparkline should mark the latest point: %s", s)
	}
}

func TestRenderSparklineSVGFlatSeries(t *testing.T) {
	// All-same prices shouldn't divide-by-zero.
	pts := []models.ListingPricePoint{
		{Price: 100, CapturedAt: time.Now().Add(-2 * time.Hour)},
		{Price: 100, CapturedAt: time.Now()},
	}
	s := (&PriceHistoryService{}).RenderSparklineSVG(pts)
	if !strings.Contains(s, "<path") {
		t.Fatalf("flat series should still render: %s", s)
	}
}
