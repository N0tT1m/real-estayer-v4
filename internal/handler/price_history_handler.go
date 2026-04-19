package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/realestayer/v4/internal/middleware"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// WatchlistPriceHistorySVG renders a sparkline SVG for a single watchlist
// item. Responds with `image/svg+xml` so the UI can drop it into innerHTML.
// Auth required; the endpoint resolves the listing from the watchlist item so
// one user can't sniff another user's list.
func (h *Handler) WatchlistPriceHistorySVG(w http.ResponseWriter, r *http.Request) {
	userID := middleware.GetUserID(r.Context())
	if userID == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	watchID := chi.URLParam(r, "id")
	oid, err := primitive.ObjectIDFromHex(watchID)
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}

	item, err := h.watchlistService.GetForUser(r.Context(), userID, oid)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}

	points, err := h.priceHistorySvc.Recent(r.Context(), item.ListingID, 30)
	if err != nil {
		http.Error(w, "failed to load price history", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "private, max-age=60")
	_, _ = w.Write([]byte(h.priceHistorySvc.RenderSparklineSVG(points)))
}
