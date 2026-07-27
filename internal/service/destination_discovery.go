package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/provider/wikipedia"
	"github.com/realestayer/v4/internal/repository"
)

// DestinationDiscoveryService finds popular travel destinations within a named
// region (US state, country, province, etc.) using two public data sources:
//
//  1. Wikidata SPARQL — gives us the candidate list of settlements, parks,
//     museums, and natural features located in the region, pre-filtered by
//     Wikipedia sitelink count (a weak popularity floor).
//  2. Wikipedia Pageviews — gives us a real popularity ranking signal;
//     Yosemite's article has ~3M annual views, an obscure township has ~200.
//
// The flow is a two-step: admin POSTs /discover with a region name, we return
// ranked candidates with a preview (description, image, already_exists). The
// admin picks which ones to keep, POSTs /discover/confirm, and we upsert them
// into the destinations collection.
type DestinationDiscoveryService struct {
	repo      *repository.DestinationRepository
	candidate *repository.DestinationCandidateRepository
	wikidata  *WikidataService
	wiki      *wikipedia.Client
}

// NewDestinationDiscoveryService wires discovery. candidate may be nil, which
// disables the activity finder's review queue and with it its ability to
// answer with places outside the catalog; the region-based admin flow above is
// unaffected.
func NewDestinationDiscoveryService(
	repo *repository.DestinationRepository,
	candidate *repository.DestinationCandidateRepository,
	wikidata *WikidataService,
	wiki *wikipedia.Client,
) *DestinationDiscoveryService {
	return &DestinationDiscoveryService{
		repo:      repo,
		candidate: candidate,
		wikidata:  wikidata,
		wiki:      wiki,
	}
}

// DiscoveryCandidate is what we return to the admin UI for each candidate.
// It includes everything the Confirm step needs so the client can echo the
// chosen rows back without a round-trip through the SPARQL endpoint.
type DiscoveryCandidate struct {
	WikidataID    string   `json:"wikidata_id"`
	Name          string   `json:"name"`
	Description   string   `json:"description"`
	ImageURL      string   `json:"image_url"`
	WikipediaURL  string   `json:"wikipedia_url"`
	ArticleTitle  string   `json:"article_title"`
	Latitude      float64  `json:"latitude"`
	Longitude     float64  `json:"longitude"`
	CountryCode   string   `json:"country_code"`
	Country       string   `json:"country"`
	Region        string   `json:"region"`
	Categories    []string `json:"categories"`
	PageViewsYear int64    `json:"page_views_year"`
	SitelinkCount int      `json:"sitelink_count"`
	AlreadyExists bool     `json:"already_exists"`
}

// DiscoveryResult is the top-level response: info about what region we
// resolved plus the ranked candidate list.
type DiscoveryResult struct {
	Region     *ResolvedEntity      `json:"region"`
	Candidates []DiscoveryCandidate `json:"candidates"`
	Total      int                  `json:"total"`
}

var errNoRegionMatch = errors.New("no Wikidata entity matched the region name")

// Discover runs the full flow: resolve -> SPARQL -> rank by pageviews ->
// enrich top N with Wikipedia summary -> flag already-existing rows.
// `limit` is the number of ranked candidates returned to the caller; set
// <= 0 for a default of 30.
func (s *DestinationDiscoveryService) Discover(ctx context.Context, regionName string, limit int) (*DiscoveryResult, error) {
	if limit <= 0 || limit > 100 {
		limit = 30
	}

	region, err := s.wikidata.ResolveEntity(ctx, regionName)
	if err != nil {
		return nil, err
	}
	if region == nil {
		return nil, errNoRegionMatch
	}

	raw, err := s.wikidata.DiscoverTourismPlaces(ctx, region.QID)
	if err != nil {
		return nil, err
	}

	// Wikidata already sorted by sitelink count (a decent popularity proxy),
	// so pageview-ranking all ~200 rows wastes HTTP — 200 calls × rate limit
	// can push this past a minute. Cap to 2× the requested limit: enough
	// headroom for pageviews to reorder the head of the list without paying
	// for every obscure entry.
	if pageviewCap := limit * 2; len(raw) > pageviewCap {
		raw = raw[:pageviewCap]
	}

	ranked := s.rankByPageviews(ctx, raw)

	// Trim to the requested limit before we spend time on Wikipedia enrichment,
	// since that's ~1 HTTP call per candidate.
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}

	candidates := s.enrichAndFinalize(ctx, ranked)

	return &DiscoveryResult{
		Region:     region,
		Candidates: candidates,
		Total:      len(candidates),
	}, nil
}

// rankByPageviews fetches the last-12-months view count for each candidate
// in parallel (capped at maxConcurrentPageviews) and returns the list
// sorted descending by views. Candidates with zero views fall to the bottom
// but aren't dropped — a brand-new article may still be a real place.
const maxConcurrentPageviews = 8

type rankedCandidate struct {
	place DiscoveredPlace
	views int64
}

func (s *DestinationDiscoveryService) rankByPageviews(ctx context.Context, places []DiscoveredPlace) []rankedCandidate {
	out := make([]rankedCandidate, len(places))
	sem := make(chan struct{}, maxConcurrentPageviews)
	var wg sync.WaitGroup

	fetchCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	for i, p := range places {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, p DiscoveredPlace) {
			defer wg.Done()
			defer func() { <-sem }()

			views := int64(0)
			if p.ArticleTitle != "" {
				v, err := s.wiki.MonthlyViews(fetchCtx, p.ArticleTitle)
				if err != nil {
					slog.Debug("pageviews fetch failed", "article", p.ArticleTitle, "error", err)
				}
				views = v
			}
			out[i] = rankedCandidate{place: p, views: views}
		}(i, p)
	}
	wg.Wait()

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].views != out[j].views {
			return out[i].views > out[j].views
		}
		return out[i].place.SitelinkCount > out[j].place.SitelinkCount
	})
	return out
}

// enrichAndFinalize pulls Wikipedia summary (description + hero image) for
// each ranked candidate and resolves supplementary fields (country name,
// region, already-exists flag).
func (s *DestinationDiscoveryService) enrichAndFinalize(ctx context.Context, ranked []rankedCandidate) []DiscoveryCandidate {
	out := make([]DiscoveryCandidate, len(ranked))
	sem := make(chan struct{}, maxConcurrentPageviews)
	var wg sync.WaitGroup

	enrichCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	for i, r := range ranked {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, r rankedCandidate) {
			defer wg.Done()
			defer func() { <-sem }()
			out[i] = s.buildCandidate(enrichCtx, r.place, r.views)
		}(i, r)
	}
	wg.Wait()
	return out
}

// buildCandidate turns one raw SPARQL row into a UI-ready candidate: country
// and region resolved from the ISO code, description and hero image pulled
// from Wikipedia, and the already-in-DB flag set.
func (s *DestinationDiscoveryService) buildCandidate(ctx context.Context, p DiscoveredPlace, views int64) DiscoveryCandidate {
	cand := DiscoveryCandidate{
		WikidataID:    p.QID,
		Name:          p.Name,
		Description:   p.Description,
		WikipediaURL:  p.WikipediaURL,
		ArticleTitle:  p.ArticleTitle,
		Latitude:      p.Latitude,
		Longitude:     p.Longitude,
		CountryCode:   p.CountryCode,
		Country:       isoCountryNames[p.CountryCode],
		Region:        regionForCountry(p.CountryCode),
		SitelinkCount: p.SitelinkCount,
		PageViewsYear: views,
		Categories:    []string{"city"},
	}
	if cand.Country == "" {
		cand.Country = p.CountryCode
	}

	if p.ArticleTitle != "" {
		if info, err := s.wiki.GetCitySummary(ctx, p.ArticleTitle); err == nil && info != nil {
			if info.Description != "" {
				cand.Description = info.Description
			}
			if info.ImageURL != "" {
				cand.ImageURL = info.ImageURL
			}
		}
	}

	// Mark already-in-DB so the UI can dim the row and pre-skip it on
	// confirm. Cheap query per row; for a 30-row batch this is ~30
	// indexed point reads and not worth batching.
	if existing, err := s.repo.FindByName(ctx, p.Name); err == nil && existing != nil {
		cand.AlreadyExists = true
	}

	return cand
}

// minSitelinksForQueue is the popularity floor for entering the review queue.
// The admin region flow can afford a floor of 3 because someone reads the list
// before confirming anything; this path files rows unattended, and a queue
// full of hamlets is a queue nobody works through. Review is a check on
// quality, not a substitute for one.
const minSitelinksForQueue = 5

// SuggestedPlace is a place the activity finder can show. Awaiting is true
// when it came from the review queue rather than the catalog, which means it
// has no destinations row and nothing may link to it yet.
type SuggestedPlace struct {
	Destination models.Destination
	Awaiting    bool
}

// ResolveForSuggestion verifies one free-text place name against Wikidata and
// returns something the finder can show — without adding anything to the
// catalog.
//
// The split matters. Answering a search with a place we do not cover is the
// point of the finder, but publishing that place into /explore for everyone is
// a separate decision with a much higher bar, and it belongs to an admin. So a
// resolved place is filed in the review queue and handed straight back to the
// person who searched; only approval moves it into the catalog.
//
// Returns (nil, nil) when the name does not resolve, falls under the
// popularity floor, or names a place a reviewer has already rejected — callers
// report all three as dropped. Rejection is deliberately sticky: a place
// refused once should not reappear in results every time the model names it.
func (s *DestinationDiscoveryService) ResolveForSuggestion(ctx context.Context, name string, activities []string) (*SuggestedPlace, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}

	// A catalog row always wins. It may have been curated by an admin or
	// enriched by another flow, and re-deriving it from Wikidata would show
	// the generic version instead.
	if existing, err := s.repo.FindByName(ctx, name); err == nil && existing != nil {
		return &SuggestedPlace{Destination: *existing}, nil
	}
	if s.candidate == nil {
		return nil, nil
	}

	// Check the queue before spending any network calls: a place suggested
	// last week is already resolved, and this is the common case once the
	// queue has warmed up.
	if known, err := s.candidate.FindByNormalizedName(ctx, normalizeName(name)); err == nil && known != nil {
		return s.seenAgain(ctx, known)
	}

	place, err := s.wikidata.PlaceByName(ctx, name)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", name, err)
	}
	// Models qualify a place with its country even when told not to, and
	// "Banff, Canada" is a worse search string than "Banff" — the comma makes
	// it look like a phrase rather than a name. Retry on the leading segment,
	// mirroring what catalog matching already does for the same reason.
	if place == nil {
		if base, _, found := strings.Cut(name, ","); found {
			if base = strings.TrimSpace(base); base != "" {
				if place, err = s.wikidata.PlaceByName(ctx, base); err != nil {
					return nil, fmt.Errorf("resolve %q: %w", base, err)
				}
			}
		}
	}
	if place == nil || place.SitelinkCount < minSitelinksForQueue {
		return nil, nil
	}

	// PlaceByName canonicalises the name, which can land on a catalog row or a
	// queue entry the caller's spelling missed.
	if canonical := normalizeName(place.Name); canonical != normalizeName(name) {
		if existing, err := s.repo.FindByName(ctx, place.Name); err == nil && existing != nil {
			return &SuggestedPlace{Destination: *existing}, nil
		}
		if known, err := s.candidate.FindByNormalizedName(ctx, canonical); err == nil && known != nil {
			return s.seenAgain(ctx, known)
		}
	}

	cand := s.buildCandidate(ctx, *place, 0)
	stored, err := s.candidate.RecordSuggestion(ctx, candidateToQueueRow(cand, place.SitelinkCount, activities))
	if err != nil {
		return nil, fmt.Errorf("queue %q: %w", cand.Name, err)
	}
	return s.fromQueue(ctx, stored)
}

// seenAgain notes another search landing on a place already queued, then
// returns it. The increment lives here rather than in fromQueue because
// fromQueue also runs immediately after RecordSuggestion, which has already
// counted the sighting.
func (s *DestinationDiscoveryService) seenAgain(ctx context.Context, c *models.DestinationCandidate) (*SuggestedPlace, error) {
	if updated, err := s.candidate.NoteSuggested(ctx, c.ID); err != nil {
		slog.Debug("candidate count update failed", "name", c.Name, "error", err)
	} else if updated != nil {
		c = updated
	}
	return s.fromQueue(ctx, c)
}

// fromQueue turns a queue row into a result, applying the review decision:
// approved rows have a catalog entry to return instead, rejected rows return
// nothing at all.
func (s *DestinationDiscoveryService) fromQueue(ctx context.Context, c *models.DestinationCandidate) (*SuggestedPlace, error) {
	switch c.Status {
	case models.CandidateRejected:
		return nil, nil
	case models.CandidateApproved:
		// Normally the catalog lookup upstream already caught this. Falling
		// through to the candidate covers the window where a row was approved
		// but the destination was later deactivated or renamed.
		if existing, err := s.repo.FindByName(ctx, c.Name); err == nil && existing != nil {
			return &SuggestedPlace{Destination: *existing}, nil
		}
	}
	return &SuggestedPlace{Destination: c.AsDestination(), Awaiting: true}, nil
}

// candidateToQueueRow maps an enriched discovery candidate onto a queue row,
// synthesising the same budget and popularity figures ConfirmAndInsert would,
// so approving a candidate is a move between collections rather than a
// re-derivation that could produce different numbers.
func candidateToQueueRow(c DiscoveryCandidate, sitelinks int, activities []string) *models.DestinationCandidate {
	region := c.Region
	if region == "" {
		region = regionForCountry(c.CountryCode)
	}
	country := c.Country
	if country == "" {
		country = isoCountryNames[c.CountryCode]
	}
	if country == "" {
		country = c.CountryCode
	}
	categories := c.Categories
	if len(categories) == 0 {
		categories = []string{"city"}
	}

	return &models.DestinationCandidate{
		NormalizedName:  normalizeName(c.Name),
		Name:            c.Name,
		Country:         country,
		CountryCode:     c.CountryCode,
		Region:          region,
		Description:     c.Description,
		ImageURL:        c.ImageURL,
		Latitude:        c.Latitude,
		Longitude:       c.Longitude,
		Categories:      categories,
		AvgDailyBudget:  cityBudget(c.Name, region),
		PopularityScore: cityPopularity(c.Name),
		WikidataID:      c.WikidataID,
		ArticleTitle:    c.ArticleTitle,
		WikipediaURL:    c.WikipediaURL,
		SitelinkCount:   sitelinks,
		Status:          models.CandidatePending,
		FirstActivities: activities,
	}
}

// ErrCandidateAlreadyReviewed means the row was not pending when the decision
// landed — someone else got there first. Surfaced rather than swallowed
// because approving twice would insert the destination twice.
var ErrCandidateAlreadyReviewed = errors.New("destination candidate already reviewed")

// ReviewCandidate applies an admin decision. Approving inserts the place into
// the destinations collection and marks the queue row; rejecting only marks
// it, which permanently suppresses the place from suggestions.
func (s *DestinationDiscoveryService) ReviewCandidate(ctx context.Context, id primitive.ObjectID, approve bool, reviewer string) (*models.DestinationCandidate, error) {
	if s.candidate == nil {
		return nil, errors.New("destination candidate queue not configured")
	}

	status := models.CandidateRejected
	if approve {
		status = models.CandidateApproved
	}

	// Claim the row first. If this returns nothing the row was not pending,
	// so a concurrent approval has already inserted the destination and this
	// call must not insert it again.
	claimed, err := s.candidate.SetStatus(ctx, id, models.CandidatePending, status, reviewer)
	if err != nil {
		return nil, err
	}
	if claimed == nil {
		return nil, ErrCandidateAlreadyReviewed
	}
	if !approve {
		return claimed, nil
	}

	dest := &models.Destination{
		Name:            claimed.Name,
		Country:         claimed.Country,
		CountryCode:     claimed.CountryCode,
		Region:          claimed.Region,
		Description:     claimed.Description,
		ImageURL:        claimed.ImageURL,
		Latitude:        claimed.Latitude,
		Longitude:       claimed.Longitude,
		Categories:      claimed.Categories,
		AvgDailyBudget:  claimed.AvgDailyBudget,
		Currency:        "USD",
		PopularityScore: claimed.PopularityScore,
		Active:          true,
	}
	if err := s.repo.Upsert(ctx, dest); err != nil {
		// Hand the row back to the queue so the decision can be retried
		// rather than being lost as approved-but-absent.
		if _, rErr := s.candidate.SetStatus(ctx, id, status, models.CandidatePending, ""); rErr != nil {
			slog.Warn("candidate rollback failed", "name", claimed.Name, "error", rErr)
		}
		return nil, fmt.Errorf("insert approved destination %q: %w", claimed.Name, err)
	}
	return claimed, nil
}

// PendingCandidates returns the review queue.
func (s *DestinationDiscoveryService) PendingCandidates(ctx context.Context, limit int) ([]models.DestinationCandidate, error) {
	if s.candidate == nil {
		return nil, errors.New("destination candidate queue not configured")
	}
	return s.candidate.ListByStatus(ctx, models.CandidatePending, limit)
}

// ConfirmAndInsert upserts the given candidates into the destinations
// collection. Fields already populated on the candidate (image_url, lat/lng,
// description) are used verbatim; budget and popularity are synthesised
// deterministically so the cards on /explore don't all read the same flat
// values.
//
// Returns counts of (inserted or refreshed, skipped because the row already
// existed or the candidate was malformed).
func (s *DestinationDiscoveryService) ConfirmAndInsert(ctx context.Context, cands []DiscoveryCandidate) (inserted, skipped int, err error) {
	for _, c := range cands {
		if c.Name == "" {
			skipped++
			continue
		}

		region := c.Region
		if region == "" {
			region = regionForCountry(c.CountryCode)
		}
		country := c.Country
		if country == "" {
			country = isoCountryNames[c.CountryCode]
		}
		if country == "" {
			country = c.CountryCode
		}
		categories := c.Categories
		if len(categories) == 0 {
			categories = []string{"city"}
		}

		dest := &models.Destination{
			Name:            c.Name,
			Country:         country,
			CountryCode:     c.CountryCode,
			Region:          region,
			Description:     c.Description,
			ImageURL:        c.ImageURL,
			Latitude:        c.Latitude,
			Longitude:       c.Longitude,
			Categories:      categories,
			AvgDailyBudget:  cityBudget(c.Name, region),
			Currency:        "USD",
			PopularityScore: cityPopularity(c.Name),
			Active:          true,
		}

		if err := s.repo.Upsert(ctx, dest); err != nil {
			slog.Warn("discovery: upsert failed", "name", c.Name, "error", err)
			skipped++
			continue
		}
		inserted++
	}
	return inserted, skipped, nil
}
