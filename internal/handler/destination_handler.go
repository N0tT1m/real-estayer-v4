package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/realestayer/v4/internal/middleware"
	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/service"
)

type DestinationHandler struct {
	tmpl             *TemplateRenderer
	destService      *service.DestinationService
	discoveryService *service.DestinationDiscoveryService
	scraperService   *service.ScraperService
	// suggestService is held only to ask whether the activity finder is
	// configured, so the explore page can hide the panel entirely rather than
	// offering a control that 503s. The endpoint itself is served by Handler.
	suggestService *service.DestinationSuggestService
}

func NewDestinationHandler(
	tmpl *TemplateRenderer,
	destService *service.DestinationService,
	discoveryService *service.DestinationDiscoveryService,
	scraperService *service.ScraperService,
	suggestService *service.DestinationSuggestService,
) *DestinationHandler {
	return &DestinationHandler{
		tmpl:             tmpl,
		destService:      destService,
		discoveryService: discoveryService,
		scraperService:   scraperService,
		suggestService:   suggestService,
	}
}

// maxPageLimit caps how many rows any destination endpoint will return for a
// caller-supplied `limit`. These endpoints are unauthenticated and sit outside
// the rate limiter, and the service layer only floors at <= 0 — so an unbounded
// value reaches Mongo's SetLimit verbatim and dumps the collection.
const maxPageLimit = 100

// boundedLimit parses a caller-supplied limit, falling back to def unless the
// value is a positive integer no greater than max.
//
// Written once and shared: this bound previously existed inline in SearchAPI
// but was simply missing from FeaturedAPI, which is the failure mode two copies
// of the same parse invite.
func boundedLimit(raw string, def, max int) int {
	if raw == "" {
		return def
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 || n > max {
		return def
	}
	return n
}

func (h *DestinationHandler) ExplorePage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	filter := models.DestinationFilter{
		Category: r.URL.Query().Get("category"),
		Region:   r.URL.Query().Get("region"),
		BestFor:  r.URL.Query().Get("best_for"),
		Search:   r.URL.Query().Get("q"),
		SortBy:   r.URL.Query().Get("sort"),
		Limit:    24,
	}

	if month := r.URL.Query().Get("month"); month != "" {
		if m, err := strconv.Atoi(month); err == nil {
			filter.Month = m
		}
	}
	if minBudget := r.URL.Query().Get("min_budget"); minBudget != "" {
		if b, err := strconv.ParseFloat(minBudget, 64); err == nil {
			filter.MinBudget = b
		}
	}
	if maxBudget := r.URL.Query().Get("max_budget"); maxBudget != "" {
		if b, err := strconv.ParseFloat(maxBudget, 64); err == nil {
			filter.MaxBudget = b
		}
	}
	if page := r.URL.Query().Get("page"); page != "" {
		if p, err := strconv.Atoi(page); err == nil && p > 1 {
			filter.Offset = (p - 1) * filter.Limit
		}
	}

	destinations, total, err := h.destService.SearchDestinations(ctx, filter)
	if err != nil {
		slog.Warn("explore: SearchDestinations failed", "error", err)
	}
	// If the collection is empty we render the empty state; the background
	// seed worker (main.go) and the admin `reseed` endpoint own population.
	// Calling SeedDestinations from the request path is never safe — a single
	// cold page load would hammer the upstream for dozens of cities, serially.

	categories := []string{"beach", "city", "nature", "adventure", "cultural", "romantic", "luxury", "island"}
	regions := []string{"Europe", "Asia", "North America", "South America", "Africa", "Oceania", "Middle East"}

	// Same fail-soft rule as AISuggest below: a filter with no data behind it
	// is hidden rather than offered as a control that always returns nothing.
	// A lookup failure hides them too — the grid above it has already failed
	// in that case, so this is the quiet option.
	availability, err := h.destService.GetFilterAvailability(ctx)
	if err != nil {
		slog.Warn("explore: GetFilterAvailability failed", "error", err)
	}

	data := map[string]interface{}{
		"Destinations": destinations,
		"Total":        total,
		"Filter":       filter,
		"Categories":   categories,
		"Regions":      regions,
		"HasBestFor":   availability.BestFor,
		"HasMonths":    availability.Months,
		// Drives the "what do you want to do" panel. False hides it outright,
		// which is the fail-soft rule: an unconfigured integration shows no
		// control rather than one that errors when clicked.
		"AISuggest": h.suggestService != nil && h.suggestService.Configured(),
	}

	// RenderWithRequest populates data["User"] from the request context itself,
	// so there is nothing to set here.
	h.tmpl.RenderWithRequest(w, r, "explore.html", data)
}

func (h *DestinationHandler) DestinationPage(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	id := chi.URLParam(r, "id")

	dest, err := h.destService.GetDestination(ctx, id)
	if err != nil {
		dest, err = h.destService.GetDestinationByName(ctx, id)
		if err != nil {
			http.Error(w, "Destination not found", http.StatusNotFound)
			return
		}
	}

	// Fetch live highlights from Overpass if the destination has none stored.
	if len(dest.Highlights) == 0 && dest.Latitude != 0 {
		if highlights, hErr := h.destService.GetHighlights(ctx, dest.Latitude, dest.Longitude); hErr == nil {
			dest.Highlights = highlights
		}
	}

	// Get similar destinations by first category.
	var similar []models.Destination
	if len(dest.Categories) > 0 {
		similar, _ = h.destService.GetDestinationsByCategory(ctx, dest.Categories[0], 4)
	}
	// Fall back to popular destinations if no similar found.
	if len(similar) == 0 {
		popular, _ := h.destService.GetPopularDestinations(ctx, 4)
		for _, d := range popular {
			if d.Name != dest.Name {
				similar = append(similar, d)
			}
		}
	}

	data := map[string]interface{}{
		"Destination": dest,
		"Similar":     similar,
	}

	// See ExplorePage: RenderWithRequest owns data["User"].
	h.tmpl.RenderWithRequest(w, r, "destination.html", data)
}

func (h *DestinationHandler) FeaturedAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	limit := boundedLimit(r.URL.Query().Get("limit"), 6, maxPageLimit)

	destinations, err := h.destService.GetFeaturedDestinations(ctx, limit)
	if err != nil {
		destinations, _ = h.destService.GetPopularDestinations(ctx, limit)
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{"destinations": destinations})
}

// AdminReseedDestinations re-seeds all destinations from the geocoder + Wikipedia.
// POST /api/v1/admin/destinations/reseed
func (h *DestinationHandler) AdminReseedDestinations(w http.ResponseWriter, r *http.Request) {
	// Detach from the request context so the goroutine survives the response
	// being written, but cap it at 30 minutes.
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if err := h.destService.SeedDestinations(ctx); err != nil {
			slog.Warn("admin reseed failed", "error", err)
		}
	}()
	respondJSON(w, http.StatusAccepted, map[string]string{"status": "reseed started in background"})
}

// AdminAddDestination adds any city to the destinations collection via the geocoder + Wikipedia.
// POST /api/v1/admin/destinations  {"name":"Lisbon","country_code":"PT","region":"Europe"}
func (h *DestinationHandler) AdminAddDestination(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name        string `json:"name"`
		CountryCode string `json:"country_code"`
		Region      string `json:"region"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}

	dest, err := h.destService.AddDestination(r.Context(), body.Name, body.CountryCode, body.Region)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	respondJSON(w, http.StatusCreated, dest)
}

// AdminDiscoverDestinations runs a Wikidata + Wikipedia-Pageviews discovery
// pass for a named region (e.g. "Michigan", "Bavaria", "Kyushu") and returns
// ranked candidates for admin review. No DB writes happen here.
// POST /api/v1/admin/destinations/discover  {"region":"Michigan","limit":30}
func (h *DestinationHandler) AdminDiscoverDestinations(w http.ResponseWriter, r *http.Request) {
	if h.discoveryService == nil {
		http.Error(w, "discovery service not configured", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		Region string `json:"region"`
		Limit  int    `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Region == "" {
		http.Error(w, "region is required", http.StatusBadRequest)
		return
	}

	// SPARQL + pageviews + enrichment can take 60-120 seconds for a large
	// region (e.g. a US state with thousands of subdivisions). Detach from
	// the request context's default so a client that hung up doesn't cancel
	// in the middle of enrichment. Wikipedia calls are rate-limited at 5/s
	// which throttles the enrichment fan-out.
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()

	result, err := h.discoveryService.Discover(ctx, body.Region, body.Limit)
	if err != nil {
		slog.Warn("admin discover failed", "region", body.Region, "error", err)
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	respondJSON(w, http.StatusOK, result)
}

// AdminConfirmDiscovered takes the admin-approved subset of candidates and
// upserts them into the destinations collection. When a scraper is wired in,
// it also dispatches a background rust-scraper job per candidate so the
// /listings page has inventory for the newly-added cities.
// POST /api/v1/admin/destinations/discover/confirm  {"candidates": [...]}
func (h *DestinationHandler) AdminConfirmDiscovered(w http.ResponseWriter, r *http.Request) {
	if h.discoveryService == nil {
		http.Error(w, "discovery service not configured", http.StatusServiceUnavailable)
		return
	}

	var body struct {
		Candidates []service.DiscoveryCandidate `json:"candidates"`
		// RegionContext is the state/province name the admin searched for
		// ("Michigan", "Quebec"). We use it as the `state` query param on
		// each scrape call so listings get tagged with a region — the
		// /listings State+Province filters rely on this field.
		RegionContext string `json:"region_context"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if len(body.Candidates) == 0 {
		http.Error(w, "no candidates provided", http.StatusBadRequest)
		return
	}

	inserted, skipped, err := h.discoveryService.ConfirmAndInsert(r.Context(), body.Candidates)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	scrapingStarted := 0
	if h.scraperService != nil {
		scrapingStarted = h.scrapeDiscoveredAsync(body.Candidates, body.RegionContext)
	}

	respondJSON(w, http.StatusOK, map[string]int{
		"inserted": inserted,
		"skipped":  skipped,
		"scraping": scrapingStarted,
	})
}

// scrapeDiscoveredAsync fires off per-city rust-scraper jobs for every
// candidate we just upserted. Runs in a detached goroutine because a 30-city
// batch can take hours — the rust scraper serializes via a single Chrome, so
// we call sequentially with a generous per-job timeout. Returns the count of
// jobs queued so the UI can surface it in the confirm toast.
func (h *DestinationHandler) scrapeDiscoveredAsync(cands []service.DiscoveryCandidate, regionContext string) int {
	// Sensible admin defaults: 14 days out, 5-night stay, 2 adults, 50 listings
	// per city. If an operator wants finer control we'll promote these to
	// fields on the confirm request, but baking defaults in keeps the flow
	// one-click.
	const (
		daysUntilCheckIn = 14
		stayNights       = 5
		adults           = 2
		limitPerCity     = 50
	)
	checkIn := time.Now().AddDate(0, 0, daysUntilCheckIn)
	checkOut := checkIn.AddDate(0, 0, stayNights)

	jobs := make([]service.ScrapeParams, 0, len(cands))
	for _, c := range cands {
		if c.Name == "" {
			continue
		}
		country := c.Country
		if country == "" {
			country = c.CountryCode
		}
		jobs = append(jobs, service.ScrapeParams{
			City:     c.Name,
			State:    regionContext,
			Country:  country,
			CheckIn:  checkIn.Format("2006-01-02"),
			CheckOut: checkOut.Format("2006-01-02"),
			Adults:   adults,
			Limit:    limitPerCity,
		})
	}
	if len(jobs) == 0 {
		return 0
	}

	slog.Info("discover scrape batch queued", "count", len(jobs), "check_in", checkIn.Format("2006-01-02"), "check_out", checkOut.Format("2006-01-02"))

	go func(jobs []service.ScrapeParams) {
		// Detached context — the admin's HTTP response has already returned.
		// Cap the whole batch at 2 hours; each ScrapeCity call has its own
		// 5-minute HTTP timeout inside ScraperService.
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		slog.Info("discover scrape batch starting", "count", len(jobs))
		for i, p := range jobs {
			if ctx.Err() != nil {
				slog.Warn("discover scrape batch aborted", "remaining", len(jobs)-i, "error", ctx.Err())
				return
			}
			slog.Info("discover scrape starting job", "index", i+1, "of", len(jobs), "city", p.City, "country", p.Country)
			if _, err := h.scraperService.ScrapeCity(ctx, p); err != nil {
				slog.Warn("discover scrape failed", "city", p.City, "country", p.Country, "error", err)
				continue
			}
			slog.Info("discover scrape finished", "city", p.City, "country", p.Country)
		}
	}(jobs)
	return len(jobs)
}

// SearchAPI is the JSON backend for the dynamic /explore page. It accepts
// every filter the template exposes — category, region, best_for, search
// string, travel month, budget band, sort, page size/offset — and returns
// a JSON envelope with destinations + total.
//
// When the destinations collection is empty we kick off a one-shot seed in
// the background (not blocking this response) and flag `seeding: true` so
// the client can poll again in a few seconds.
func (h *DestinationHandler) SearchAPI(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	q := r.URL.Query()

	filter := models.DestinationFilter{
		Category: q.Get("category"),
		Region:   q.Get("region"),
		BestFor:  q.Get("best_for"),
		Search:   q.Get("q"),
		SortBy:   q.Get("sort"),
		Limit:    24,
	}

	filter.Limit = boundedLimit(q.Get("limit"), filter.Limit, maxPageLimit)
	if v := q.Get("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			filter.Offset = n
		}
	}
	if v := q.Get("page"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 1 {
			filter.Offset = (n - 1) * filter.Limit
		}
	}
	if v := q.Get("month"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 1 && n <= 12 {
			filter.Month = n
		}
	}
	if v := q.Get("min_budget"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			filter.MinBudget = f
		}
	}
	if v := q.Get("max_budget"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			filter.MaxBudget = f
		}
	}

	destinations, total, err := h.destService.SearchDestinations(ctx, filter)
	if err != nil {
		slog.Warn("destinations search: query failed", "error", err)
	}

	// Auto-seed on first-ever request to /api/v1/destinations when the
	// collection is empty. We detach from the request context so the
	// response returns immediately; the client polls again after a beat.
	seeding := false
	if total == 0 && filter.Search == "" && filter.Category == "" && filter.Region == "" {
		if h.destService.TryStartSeed() {
			seeding = true
		}
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"destinations": destinations,
		"total":        total,
		"seeding":      seeding,
	})
}

// AdminListCandidates returns the activity finder's review queue: real places
// the finder surfaced that are not in the catalog, waiting on a decision.
// GET /api/v1/admin/destinations/candidates?limit=100
func (h *DestinationHandler) AdminListCandidates(w http.ResponseWriter, r *http.Request) {
	if h.discoveryService == nil {
		http.Error(w, "discovery service not configured", http.StatusServiceUnavailable)
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	candidates, err := h.discoveryService.PendingCandidates(r.Context(), limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	respondJSON(w, http.StatusOK, map[string]interface{}{
		"candidates": candidates,
		"total":      len(candidates),
	})
}

// AdminReviewCandidate accepts or refuses one queued place. Approving inserts
// it into the destinations collection; refusing suppresses it from future
// suggestions permanently.
// POST /api/v1/admin/destinations/candidates/{id}/review  {"approve":true}
func (h *DestinationHandler) AdminReviewCandidate(w http.ResponseWriter, r *http.Request) {
	if h.discoveryService == nil {
		http.Error(w, "discovery service not configured", http.StatusServiceUnavailable)
		return
	}

	id, err := primitive.ObjectIDFromHex(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid candidate id", http.StatusBadRequest)
		return
	}

	var body struct {
		Approve bool `json:"approve"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	reviewed, err := h.discoveryService.ReviewCandidate(r.Context(), id, body.Approve, middleware.GetUserID(r.Context()))
	switch {
	case errors.Is(err, service.ErrCandidateAlreadyReviewed):
		// 409 rather than 404: the row exists, someone else just decided it.
		http.Error(w, "this candidate was already reviewed", http.StatusConflict)
		return
	case err != nil:
		slog.Warn("candidate review failed", "id", id.Hex(), "approve", body.Approve, "error", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// A newly approved destination has no listings behind it, same as the
	// region discovery flow — kick off a scrape so it isn't an empty page.
	if body.Approve && h.scraperService != nil {
		h.scrapeDiscoveredAsync([]service.DiscoveryCandidate{{
			Name:        reviewed.Name,
			Country:     reviewed.Country,
			CountryCode: reviewed.CountryCode,
			Region:      reviewed.Region,
		}}, reviewed.Region)
	}

	respondJSON(w, http.StatusOK, reviewed)
}
