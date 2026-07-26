package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// SavedSearchService wraps saved-search persistence and the background re-run
// worker. It depends on ListingService for matching and the user's Discord
// webhook config for delivery.
type SavedSearchService struct {
	repo     *repository.SavedSearchRepository
	listings *ListingService
	users    *repository.UserRepository
	webhook  string // global fallback webhook; per-user override preferred when we add it
	client   *http.Client
}

func NewSavedSearchService(repo *repository.SavedSearchRepository, listings *ListingService, users *repository.UserRepository, fallbackWebhook string) *SavedSearchService {
	return &SavedSearchService{
		repo:     repo,
		listings: listings,
		users:    users,
		webhook:  fallbackWebhook,
		client:   &http.Client{Timeout: 10 * time.Second},
	}
}

func (s *SavedSearchService) Create(ctx context.Context, userIDHex string, name string, q models.SavedSearchQuery, notifyDiscord bool) (*models.SavedSearch, error) {
	oid, err := primitive.ObjectIDFromHex(userIDHex)
	if err != nil {
		return nil, errors.New("invalid user ID")
	}
	if name == "" {
		return nil, errors.New("name is required")
	}
	ss := &models.SavedSearch{
		UserID:        oid,
		Name:          name,
		Query:         q,
		NotifyDiscord: notifyDiscord,
	}
	if err := s.repo.Create(ctx, ss); err != nil {
		return nil, err
	}
	return ss, nil
}

func (s *SavedSearchService) List(ctx context.Context, userIDHex string) ([]models.SavedSearch, error) {
	oid, err := primitive.ObjectIDFromHex(userIDHex)
	if err != nil {
		return nil, errors.New("invalid user ID")
	}
	return s.repo.ListByUser(ctx, oid)
}

func (s *SavedSearchService) Delete(ctx context.Context, userIDHex, idHex string) error {
	uid, err := primitive.ObjectIDFromHex(userIDHex)
	if err != nil {
		return errors.New("invalid user ID")
	}
	id, err := primitive.ObjectIDFromHex(idHex)
	if err != nil {
		return errors.New("invalid id")
	}
	return s.repo.Delete(ctx, uid, id)
}

// RunDue iterates due saved searches and sends Discord notifications when new
// matches are found. Intended to be invoked by a ticker from main.go.
//
// Matching is deliberately simple: for each search, the current matching
// listing IDs are compared against last_seen_ids. Anything new triggers a
// single Discord message listing all new matches.
func (s *SavedSearchService) RunDue(ctx context.Context) {
	before := time.Now().Add(-1 * time.Hour)
	due, err := s.repo.ListDue(ctx, before, 100)
	if err != nil {
		slog.Warn("saved search: list due failed", "error", err)
		return
	}
	for _, search := range due {
		s.runOne(ctx, search)
	}
}

func (s *SavedSearchService) runOne(ctx context.Context, search models.SavedSearch) {
	filter := s.listings.FilterForSavedSearch(search.Query)
	listings, err := s.listings.FindMatching(ctx, filter, 50)
	if err != nil {
		slog.Warn("saved search: query failed", "search", search.ID.Hex(), "error", err)
		return
	}
	currentIDs := make([]string, 0, len(listings))
	for _, l := range listings {
		currentIDs = append(currentIDs, l.ID.Hex())
	}
	seen := make(map[string]struct{}, len(search.LastSeenIDs))
	for _, id := range search.LastSeenIDs {
		seen[id] = struct{}{}
	}
	var newlyAdded []models.Listing
	for _, l := range listings {
		if _, ok := seen[l.ID.Hex()]; !ok {
			newlyAdded = append(newlyAdded, l)
		}
	}
	if search.LastRunAt != nil && len(newlyAdded) > 0 && search.NotifyDiscord {
		if webhook := s.webhookFor(ctx, search.UserID); webhook != "" {
			s.notify(ctx, webhook, search, newlyAdded)
		}
	}
	if err := s.repo.UpdateAfterRun(ctx, search.ID, currentIDs); err != nil {
		slog.Warn("saved search: mark run failed", "search", search.ID.Hex(), "error", err)
	}
}

// webhookFor prefers the user's per-account webhook and falls back to the
// global one if the user hasn't configured their own. Returns "" when nothing
// is configured; caller must skip delivery.
func (s *SavedSearchService) webhookFor(ctx context.Context, userID primitive.ObjectID) string {
	user, err := s.users.FindByID(ctx, userID)
	if err == nil && user != nil && user.Preferences.Notifications.DiscordWebhook != "" {
		return user.Preferences.Notifications.DiscordWebhook
	}
	return s.webhook
}

func (s *SavedSearchService) notify(ctx context.Context, webhook string, search models.SavedSearch, listings []models.Listing) {
	var lines []string
	for i, l := range listings {
		if i >= 5 {
			lines = append(lines, fmt.Sprintf("…and %d more", len(listings)-5))
			break
		}
		lines = append(lines, fmt.Sprintf("• **%s** — %s", l.Title, l.Price))
	}
	payload := map[string]interface{}{
		"content": fmt.Sprintf("New matches for **%s** (%d):\n%s", search.Name, len(listings), joinLines(lines)),
	}
	buf, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, webhook, bytes.NewReader(buf))
	if err != nil {
		slog.Warn("saved search: webhook request failed", "error", err)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		slog.Warn("saved search: webhook send failed", "error", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
}

func joinLines(ls []string) string {
	out := ""
	for i, l := range ls {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}
