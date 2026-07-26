package repository

import (
	"context"
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/realestayer/v4/internal/database"
	"github.com/realestayer/v4/internal/models"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var ErrListingNotFound = errors.New("listing not found")

// ListingRepository handles listing data operations
type ListingRepository struct {
	collection *mongo.Collection
}

// NewListingRepository creates a new listing repository
func NewListingRepository(db *database.DB) *ListingRepository {
	return &ListingRepository{
		collection: db.Collection("listings"),
	}
}

// FindByID finds a listing by ID
func (r *ListingRepository) FindByID(ctx context.Context, id primitive.ObjectID) (*models.Listing, error) {
	var listing models.Listing
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&listing)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrListingNotFound
	}
	return &listing, err
}

// Search finds listings based on search parameters
func (r *ListingRepository) Search(ctx context.Context, params models.ListingSearchParams) (*models.ListingSearchResult, error) {
	unlimited, skip := normalizeListingPaging(&params)
	filter := buildListingFilter(params)

	// Count total
	total, err := r.collection.CountDocuments(ctx, filter)
	if err != nil {
		return nil, err
	}

	opts := options.Find().SetSort(buildListingSort(params.SortBy))
	if !unlimited {
		opts.SetSkip(int64(skip)).SetLimit(int64(params.Limit))
	}

	cursor, err := r.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()

	var listings []models.Listing
	if err := cursor.All(ctx, &listings); err != nil {
		return nil, err
	}

	// Parse and set numeric values if not already set
	for i := range listings {
		if listings[i].PriceNumeric == 0 && listings[i].Price != "" {
			listings[i].PriceNumeric = parsePrice(listings[i].Price)
		}
		if listings[i].RatingNumeric == 0 && listings[i].Rating != "" {
			listings[i].RatingNumeric = parseRating(listings[i].Rating)
		}
	}

	totalPages := 1
	if !unlimited {
		totalPages = int(total) / params.Limit
		if int(total)%params.Limit != 0 {
			totalPages++
		}
	}

	return &models.ListingSearchResult{
		Listings:   listings,
		Total:      total,
		Page:       params.Page,
		Limit:      params.Limit,
		TotalPages: totalPages,
	}, nil
}

// GetFeatures returns all unique features across listings
func (r *ListingRepository) GetFeatures(ctx context.Context) ([]string, error) {
	pipeline := []bson.M{
		{"$unwind": "$features"},
		{"$group": bson.M{"_id": "$features"}},
		{"$sort": bson.M{"_id": 1}},
	}

	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()

	var results []struct {
		ID string `bson:"_id"`
	}
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	features := make([]string, len(results))
	for i, r := range results {
		features[i] = r.ID
	}

	return features, nil
}

// GetRegions returns all unique regions
func (r *ListingRepository) GetRegions(ctx context.Context) ([]string, error) {
	results, err := r.collection.Distinct(ctx, "region", bson.M{})
	if err != nil {
		return nil, err
	}

	regions := make([]string, 0, len(results))
	for _, v := range results {
		if s, ok := v.(string); ok && s != "" {
			regions = append(regions, s)
		}
	}

	return regions, nil
}

// GetCountries returns all unique countries
func (r *ListingRepository) GetCountries(ctx context.Context) ([]string, error) {
	results, err := r.collection.Distinct(ctx, "country", bson.M{})
	if err != nil {
		return nil, err
	}

	countries := make([]string, 0, len(results))
	for _, v := range results {
		if s, ok := v.(string); ok && s != "" {
			countries = append(countries, s)
		}
	}

	return countries, nil
}

// Count returns total listing count
func (r *ListingRepository) Count(ctx context.Context) (int64, error) {
	return r.collection.CountDocuments(ctx, bson.M{})
}

// usCountries / caCountries capture the spelling variants the scraper has
// emitted across its lifetime. Listings older than the normalisation pass
// may still carry "USA" / "US" instead of "United States".
var (
	usCountryVariants = []string{"United States", "USA", "US", "U.S.", "U.S.A."}
	caCountryVariants = []string{"Canada", "CA"}
)

// GetStates returns distinct regions (the scraper's state/province field) for
// listings located in the United States, sorted alphabetically. Powers the
// /listings "State" filter toggle.
func (r *ListingRepository) GetStates(ctx context.Context) ([]string, error) {
	return r.distinctRegionsByCountry(ctx, usCountryVariants)
}

// GetProvinces is the Canadian twin of GetStates.
func (r *ListingRepository) GetProvinces(ctx context.Context) ([]string, error) {
	return r.distinctRegionsByCountry(ctx, caCountryVariants)
}

func (r *ListingRepository) distinctRegionsByCountry(ctx context.Context, variants []string) ([]string, error) {
	results, err := r.collection.Distinct(ctx, "region", bson.M{
		"country": bson.M{"$in": variants},
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(results))
	for _, v := range results {
		if s, ok := v.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out, nil
}

// GetCities returns distinct cities derived from the first comma-separated
// segment of `location`. The aggregation does the split server-side so we
// don't ship every listing row to the app just to pick a prefix. Sorted
// alphabetically for UI stability.
func (r *ListingRepository) GetCities(ctx context.Context) ([]string, error) {
	pipeline := mongo.Pipeline{
		{{Key: "$match", Value: bson.M{"location": bson.M{"$ne": ""}}}},
		{{Key: "$group", Value: bson.M{
			"_id": bson.M{
				"$trim": bson.M{
					"input": bson.M{
						"$arrayElemAt": bson.A{
							bson.M{"$split": bson.A{"$location", ","}},
							0,
						},
					},
				},
			},
		}}},
		{{Key: "$match", Value: bson.M{"_id": bson.M{"$ne": ""}}}},
		{{Key: "$sort", Value: bson.M{"_id": 1}}},
	}
	cursor, err := r.collection.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()
	var rows []struct {
		ID string `bson:"_id"`
	}
	if err := cursor.All(ctx, &rows); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		if r.ID != "" {
			out = append(out, r.ID)
		}
	}
	return out, nil
}

// Delete removes a listing by ID
func (r *ListingRepository) Delete(ctx context.Context, id primitive.ObjectID) error {
	result, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return ErrListingNotFound
	}
	return nil
}

// parsePrice extracts numeric value from price string like "$199"
// normalizeListingPaging clamps page/limit to their defaults and reports both
// whether the caller asked for every row and the resulting skip offset.
// A Limit of 0 or -1 means "no limit"; Limit is still defaulted to 20 so the
// pagination display has a sane page size to render.
func normalizeListingPaging(params *models.ListingSearchParams) (unlimited bool, skip int) {
	if params.Page < 1 {
		params.Page = 1
	}
	unlimited = params.Limit <= 0
	if params.Limit < 1 {
		params.Limit = 20
	}
	return unlimited, (params.Page - 1) * params.Limit
}

// buildListingFilter turns search params into the Mongo query document. Split
// out of Search so the filter semantics can be tested without a live database.
func buildListingFilter(params models.ListingSearchParams) bson.M {
	filter := bson.M{}

	// Text search across multiple fields
	if params.Query != "" {
		filter["$or"] = []bson.M{
			{"title": bson.M{"$regex": params.Query, "$options": "i"}},
			{"description": bson.M{"$regex": params.Query, "$options": "i"}},
			{"location": bson.M{"$regex": params.Query, "$options": "i"}},
		}
	}

	// Location filter
	if params.Location != "" {
		filter["location"] = bson.M{"$regex": params.Location, "$options": "i"}
	}

	// Region filter
	if params.Region != "" {
		filter["region"] = bson.M{"$regex": params.Region, "$options": "i"}
	}

	// Country filter
	if params.Country != "" {
		filter["country"] = bson.M{"$regex": params.Country, "$options": "i"}
	}

	// City filter: anchored to the start of `location` so "Port Huron" doesn't
	// also match "Export Huron" (hypothetical) and ends at a comma so
	// "Detroit" matches "Detroit, Michigan..." but not "Detroit Lakes, MN".
	// Note this deliberately overwrites any Location filter set above.
	if params.City != "" {
		escaped := regexp.QuoteMeta(params.City)
		filter["location"] = bson.M{"$regex": "^" + escaped + "\\s*,", "$options": "i"}
	}

	// Price range filter
	if params.MinPrice > 0 || params.MaxPrice > 0 {
		priceFilter := bson.M{}
		if params.MinPrice > 0 {
			priceFilter["$gte"] = params.MinPrice
		}
		if params.MaxPrice > 0 {
			priceFilter["$lte"] = params.MaxPrice
		}
		filter["price_numeric"] = priceFilter
	}

	// Rating filter
	if params.MinRating > 0 {
		filter["rating_numeric"] = bson.M{"$gte": params.MinRating}
	}

	// Features filter (must have all specified features)
	if len(params.Features) > 0 {
		filter["features"] = bson.M{"$all": params.Features}
	}

	// Amenity facets are the canonical, filterable set; conjunctive like
	// features so "hot tub AND pool" narrows rather than widens.
	if len(params.Amenities) > 0 {
		filter["amenities"] = bson.M{"$all": params.Amenities}
	}

	// Room facts. These are stored only where extraction succeeded, so a
	// filter necessarily excludes listings whose count is unknown — that is
	// the correct behaviour for a minimum-size requirement.
	if params.MinBedrooms > 0 {
		filter["bedrooms"] = bson.M{"$gte": params.MinBedrooms}
	}
	if params.MinSleeps > 0 {
		filter["sleeps"] = bson.M{"$gte": params.MinSleeps}
	}

	// Property type filter
	if params.PropertyType != "" {
		filter["property_type"] = params.PropertyType
	}

	return filter
}

// buildListingSort maps the public sort keys to Mongo sort documents,
// defaulting to newest-first for empty or unrecognised values.
func buildListingSort(sortBy string) bson.M {
	switch sortBy {
	case "price_asc":
		return bson.M{"price_numeric": 1}
	case "price_desc":
		return bson.M{"price_numeric": -1}
	case "rating_desc":
		return bson.M{"rating_numeric": -1}
	case "newest":
		return bson.M{"created_at": -1}
	}
	return bson.M{"created_at": -1}
}

func parsePrice(price string) float64 {
	// Remove currency symbols and non-numeric characters
	re := regexp.MustCompile(`[\d.]+`)
	match := re.FindString(strings.ReplaceAll(price, ",", ""))
	if match == "" {
		return 0
	}
	val, _ := strconv.ParseFloat(match, 64)
	return val
}

// parseRating extracts numeric value from rating string like "4.89"
func parseRating(rating string) float64 {
	re := regexp.MustCompile(`[\d.]+`)
	match := re.FindString(rating)
	if match == "" {
		return 0
	}
	val, _ := strconv.ParseFloat(match, 64)
	return val
}
