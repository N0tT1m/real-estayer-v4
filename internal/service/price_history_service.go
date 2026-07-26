package service

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// PriceHistoryService wraps the repo with a couple of view-model helpers used
// by the watchlist sparkline.
type PriceHistoryService struct {
	repo *repository.PriceHistoryRepository
}

func NewPriceHistoryService(repo *repository.PriceHistoryRepository) *PriceHistoryService {
	return &PriceHistoryService{repo: repo}
}

// Record inserts a new price point for a listing.
func (s *PriceHistoryService) Record(ctx context.Context, listingID primitive.ObjectID, price float64, currency string) error {
	return s.repo.Record(ctx, listingID, price, currency)
}

// Recent returns chronological price points.
func (s *PriceHistoryService) Recent(ctx context.Context, listingID primitive.ObjectID, limit int) ([]models.ListingPricePoint, error) {
	return s.repo.Recent(ctx, listingID, limit)
}

// RenderSparklineSVG produces a simple SVG polyline the browser can drop
// straight into innerHTML. Width/height are fixed to the container's 16-unit
// height so the caller can style via CSS (`text-primary-500` etc).
//
// A single point renders as a flat line. No points renders a muted placeholder.
func (s *PriceHistoryService) RenderSparklineSVG(points []models.ListingPricePoint) string {
	const (
		w, h = 320, 64
		padY = 4
	)
	if len(points) == 0 {
		return `<svg class="sparkline" viewBox="0 0 320 64" preserveAspectRatio="none" aria-label="No price history yet"><text x="50%" y="50%" text-anchor="middle" dominant-baseline="middle" fill="currentColor" opacity="0.5" font-size="10">No price history yet</text></svg>`
	}

	minP, maxP := points[0].Price, points[0].Price
	for _, p := range points {
		if p.Price < minP {
			minP = p.Price
		}
		if p.Price > maxP {
			maxP = p.Price
		}
	}
	span := maxP - minP
	if span <= 0 {
		span = 1
	}

	var line, area strings.Builder
	fmt.Fprintf(&area, "M 0,%d ", h)
	for i, p := range points {
		x := float64(0)
		if len(points) > 1 {
			x = float64(i) * float64(w) / float64(len(points)-1)
		} else {
			x = float64(w) / 2
		}
		y := float64(h-padY) - (p.Price-minP)/span*float64(h-padY*2)
		if i == 0 {
			fmt.Fprintf(&line, "M %.2f,%.2f ", x, y)
		} else {
			fmt.Fprintf(&line, "L %.2f,%.2f ", x, y)
		}
		fmt.Fprintf(&area, "L %.2f,%.2f ", x, y)
	}
	fmt.Fprintf(&area, "L %d,%d Z", w, h)

	last := points[len(points)-1]
	lastX := float64(w)
	lastY := float64(h-padY) - (last.Price-minP)/math.Max(span, 1)*float64(h-padY*2)

	return fmt.Sprintf(
		`<svg class="sparkline" viewBox="0 0 %d %d" preserveAspectRatio="none" aria-label="Price history sparkline"><path class="area" d="%s"/><path class="line" d="%s"/><circle class="dot" cx="%.2f" cy="%.2f" r="2.5"/></svg>`,
		w, h, area.String(), line.String(), lastX, lastY,
	)
}
