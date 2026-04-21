package service

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"sync"
	"time"

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
	repo     *repository.DestinationRepository
	wikidata *WikidataService
	wiki     *wikipedia.Client
}

func NewDestinationDiscoveryService(
	repo *repository.DestinationRepository,
	wikidata *WikidataService,
	wiki *wikipedia.Client,
) *DestinationDiscoveryService {
	return &DestinationDiscoveryService{
		repo:     repo,
		wikidata: wikidata,
		wiki:     wiki,
	}
}

// DiscoveryCandidate is what we return to the admin UI for each candidate.
// It includes everything the Confirm step needs so the client can echo the
// chosen rows back without a round-trip through the SPARQL endpoint.
type DiscoveryCandidate struct {
	WikidataID      string   `json:"wikidata_id"`
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	ImageURL        string   `json:"image_url"`
	WikipediaURL    string   `json:"wikipedia_url"`
	ArticleTitle    string   `json:"article_title"`
	Latitude        float64  `json:"latitude"`
	Longitude       float64  `json:"longitude"`
	CountryCode     string   `json:"country_code"`
	Country         string   `json:"country"`
	Region          string   `json:"region"`
	Categories      []string `json:"categories"`
	PageViewsYear   int64    `json:"page_views_year"`
	SitelinkCount   int      `json:"sitelink_count"`
	AlreadyExists   bool     `json:"already_exists"`
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

			p := r.place
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
				PageViewsYear: r.views,
				Categories:    []string{"city"},
			}
			if cand.Country == "" {
				cand.Country = p.CountryCode
			}

			if p.ArticleTitle != "" {
				if info, err := s.wiki.GetCitySummary(enrichCtx, p.ArticleTitle); err == nil && info != nil {
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
			if existing, err := s.repo.FindByName(enrichCtx, p.Name); err == nil && existing != nil {
				cand.AlreadyExists = true
			}

			out[i] = cand
		}(i, r)
	}
	wg.Wait()
	return out
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
