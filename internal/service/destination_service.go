package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/realestayer/v3/internal/models"
	"github.com/realestayer/v3/internal/provider/amadeus"
	"github.com/realestayer/v3/internal/repository"
)

// seedCities is the list of popular destinations used to seed MongoDB via the Amadeus API.
// Only city names + known regions are hardcoded; all other data comes from Amadeus.
var seedCities = []struct {
	Name   string
	Region string
}{
	// Europe
	{"Paris", "Europe"},
	{"London", "Europe"},
	{"Rome", "Europe"},
	{"Barcelona", "Europe"},
	{"Amsterdam", "Europe"},
	{"Prague", "Europe"},
	{"Lisbon", "Europe"},
	{"Istanbul", "Europe"},
	{"Vienna", "Europe"},
	{"Athens", "Europe"},
	// Asia
	{"Tokyo", "Asia"},
	{"Bangkok", "Asia"},
	{"Bali", "Asia"},
	{"Singapore", "Asia"},
	{"Seoul", "Asia"},
	{"Kuala Lumpur", "Asia"},
	{"Hong Kong", "Asia"},
	{"Mumbai", "Asia"},
	// North America
	{"New York", "North America"},
	{"Los Angeles", "North America"},
	{"Miami", "North America"},
	{"Chicago", "North America"},
	{"Toronto", "North America"},
	{"Vancouver", "North America"},
	{"Mexico City", "North America"},
	{"Las Vegas", "North America"},
	// South America
	{"Rio de Janeiro", "South America"},
	{"Buenos Aires", "South America"},
	{"Bogota", "South America"},
	{"Lima", "South America"},
	// Africa
	{"Cape Town", "Africa"},
	{"Marrakech", "Africa"},
	{"Cairo", "Africa"},
	{"Nairobi", "Africa"},
	// Middle East
	{"Dubai", "Middle East"},
	{"Abu Dhabi", "Middle East"},
	{"Doha", "Middle East"},
	// Oceania
	{"Sydney", "Oceania"},
	{"Melbourne", "Oceania"},
	{"Auckland", "Oceania"},
}

// regionBudgets holds a typical average daily budget (USD) per region used when seeding.
var regionBudgets = map[string]float64{
	"Europe":        150,
	"Asia":          90,
	"North America": 200,
	"South America": 80,
	"Africa":        85,
	"Middle East":   180,
	"Oceania":       170,
}

type DestinationService struct {
	repo          *repository.DestinationRepository
	amadeusClient *amadeus.Client
}

func NewDestinationService(repo *repository.DestinationRepository, amadeusClient *amadeus.Client) *DestinationService {
	return &DestinationService{repo: repo, amadeusClient: amadeusClient}
}

func (s *DestinationService) GetDestination(ctx context.Context, id string) (*models.Destination, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	return s.repo.FindByID(ctx, oid)
}

func (s *DestinationService) GetDestinationByName(ctx context.Context, name string) (*models.Destination, error) {
	dest, err := s.repo.FindByName(ctx, name)
	if err == nil {
		return dest, nil
	}

	// Not in DB — look up live from Amadeus and cache it.
	if s.amadeusClient == nil {
		return nil, err
	}

	result, amErr := s.amadeusClient.SearchCity(ctx, name)
	if amErr != nil {
		return nil, err // return original not-found error
	}

	region := regionForCountry(result.CountryCode)
	budget := regionBudgets[region]
	if budget == 0 {
		budget = 100
	}

	dest = &models.Destination{
		Name:           formatCityName(result.Name, name),
		Country:        formatCountryName(result.CountryName),
		CountryCode:    result.CountryCode,
		Region:         region,
		AirportCode:    result.IataCode,
		Latitude:       result.Latitude,
		Longitude:      result.Longitude,
		Description:    fmt.Sprintf("Discover %s, a vibrant destination in %s with unforgettable experiences awaiting every traveller.", formatCityName(result.Name, name), formatCountryName(result.CountryName)),
		ImageURL:       unsplashURL(name),
		AvgDailyBudget: budget,
		Currency:       "USD",
		PopularityScore: 70,
	}

	_ = s.repo.Upsert(ctx, dest)
	return dest, nil
}

func (s *DestinationService) SearchDestinations(ctx context.Context, filter models.DestinationFilter) ([]models.Destination, int64, error) {
	return s.repo.Find(ctx, filter)
}

func (s *DestinationService) GetFeaturedDestinations(ctx context.Context, limit int) ([]models.Destination, error) {
	if limit <= 0 {
		limit = 6
	}
	return s.repo.FindFeatured(ctx, limit)
}

func (s *DestinationService) GetPopularDestinations(ctx context.Context, limit int) ([]models.Destination, error) {
	if limit <= 0 {
		limit = 12
	}
	return s.repo.FindPopular(ctx, limit)
}

func (s *DestinationService) GetDestinationsByCategory(ctx context.Context, category string, limit int) ([]models.Destination, error) {
	if limit <= 0 {
		limit = 8
	}
	return s.repo.FindByCategory(ctx, category, limit)
}

func (s *DestinationService) GetCategories(ctx context.Context) ([]string, error) {
	return s.repo.GetCategories(ctx)
}

func (s *DestinationService) GetRegions(ctx context.Context) ([]string, error) {
	return s.repo.GetRegions(ctx)
}

// SeedFromAmadeus populates MongoDB with destinations from the Amadeus city search API.
// It is idempotent — cities that already exist are skipped.
func (s *DestinationService) SeedFromAmadeus(ctx context.Context) error {
	if s.amadeusClient == nil {
		return fmt.Errorf("amadeus client not configured")
	}

	seeded := 0
	for _, city := range seedCities {
		result, err := s.amadeusClient.SearchCity(ctx, city.Name)
		if err != nil {
			slog.Warn("destination seed: city lookup failed", "city", city.Name, "error", err)
			continue
		}

		region := regionForCountry(result.CountryCode)
		if region == "Other" {
			region = city.Region // fall back to seed hint
		}
		budget := regionBudgets[region]
		if budget == 0 {
			budget = 100
		}

		dest := &models.Destination{
			Name:            formatCityName(result.Name, city.Name),
			Country:         formatCountryName(result.CountryName),
			CountryCode:     result.CountryCode,
			Region:          region,
			AirportCode:     result.IataCode,
			Latitude:        result.Latitude,
			Longitude:       result.Longitude,
			Description:     fmt.Sprintf("Discover %s, a vibrant destination in %s with unforgettable experiences awaiting every traveller.", formatCityName(result.Name, city.Name), formatCountryName(result.CountryName)),
			ImageURL:        unsplashURL(city.Name),
			AvgDailyBudget:  budget,
			Currency:        "USD",
			PopularityScore: 70,
			Featured:        false,
		}

		if err := s.repo.Upsert(ctx, dest); err != nil {
			slog.Warn("destination seed: upsert failed", "city", city.Name, "error", err)
			continue
		}
		seeded++
	}

	slog.Info("destination seed complete", "seeded", seeded, "total", len(seedCities))
	return nil
}

// GetHighlights fetches live Points of Interest from Amadeus for a destination's coordinates
// and converts them into destination highlights.
func (s *DestinationService) GetHighlights(ctx context.Context, latitude, longitude float64) ([]models.DestinationHighlight, error) {
	if s.amadeusClient == nil {
		return nil, nil
	}

	pois, err := s.amadeusClient.GetPointsOfInterest(ctx, latitude, longitude)
	if err != nil {
		return nil, err
	}

	highlights := make([]models.DestinationHighlight, 0, 3)
	for _, poi := range pois {
		if len(highlights) >= 3 {
			break
		}
		highlights = append(highlights, models.DestinationHighlight{
			Title:       poi.Name,
			Description: categoryLabel(poi.Category),
			Icon:        categoryIcon(poi.Category),
		})
	}
	return highlights, nil
}

// countryRegions maps ISO country codes to display region names.
var countryRegions = map[string]string{
	// Europe
	"FR": "Europe", "GB": "Europe", "DE": "Europe", "IT": "Europe",
	"ES": "Europe", "NL": "Europe", "PT": "Europe", "GR": "Europe",
	"AT": "Europe", "CH": "Europe", "BE": "Europe", "SE": "Europe",
	"NO": "Europe", "DK": "Europe", "FI": "Europe", "PL": "Europe",
	"CZ": "Europe", "HU": "Europe", "HR": "Europe", "IS": "Europe",
	"TR": "Europe", "RO": "Europe", "BG": "Europe", "SK": "Europe",
	// Asia
	"JP": "Asia", "CN": "Asia", "IN": "Asia", "TH": "Asia",
	"ID": "Asia", "MY": "Asia", "SG": "Asia", "VN": "Asia",
	"PH": "Asia", "KR": "Asia", "HK": "Asia", "TW": "Asia",
	"MV": "Asia", "LK": "Asia", "NP": "Asia", "MM": "Asia",
	// North America
	"US": "North America", "CA": "North America", "MX": "North America",
	"CR": "North America", "PA": "North America", "CU": "North America",
	"JM": "North America", "DO": "North America",
	// South America
	"BR": "South America", "AR": "South America", "CO": "South America",
	"PE": "South America", "CL": "South America", "EC": "South America",
	"BO": "South America", "UY": "South America", "PY": "South America",
	// Africa
	"ZA": "Africa", "MA": "Africa", "EG": "Africa", "KE": "Africa",
	"TZ": "Africa", "GH": "Africa", "NG": "Africa", "ET": "Africa",
	"SN": "Africa", "TN": "Africa", "MU": "Africa", "CI": "Africa",
	// Middle East
	"AE": "Middle East", "SA": "Middle East", "QA": "Middle East",
	"BH": "Middle East", "KW": "Middle East", "OM": "Middle East",
	"IL": "Middle East", "JO": "Middle East", "LB": "Middle East",
	// Oceania
	"AU": "Oceania", "NZ": "Oceania", "FJ": "Oceania", "PG": "Oceania",
}

// regionForCountry returns the display region for an ISO country code.
func regionForCountry(countryCode string) string {
	if r, ok := countryRegions[countryCode]; ok {
		return r
	}
	return "Other"
}

// unsplashURL returns a free Unsplash source image URL for the given city name.
func unsplashURL(city string) string {
	slug := strings.ToLower(strings.ReplaceAll(city, " ", "-"))
	return fmt.Sprintf("https://source.unsplash.com/featured/800x600/?%s,travel,city", slug)
}

// formatCityName returns the proper-cased city name, preferring the seed name when the
// Amadeus response is all-caps (e.g. "PARIS" → "Paris").
func formatCityName(amadeusName, seedName string) string {
	if amadeusName == strings.ToUpper(amadeusName) && len(seedName) > 0 {
		return seedName
	}
	return amadeusName
}

// formatCountryName title-cases an all-caps country name from Amadeus.
func formatCountryName(name string) string {
	if name == strings.ToUpper(name) {
		return strings.Title(strings.ToLower(name))
	}
	return name
}

// categoryLabel returns a human-readable label for an Amadeus POI category.
func categoryLabel(category string) string {
	labels := map[string]string{
		"SIGHTS":      "Popular attraction",
		"BEACH_PARK":  "Beach or park",
		"HISTORICAL":  "Historic site",
		"NIGHTLIFE":   "Nightlife spot",
		"RESTAURANT":  "Dining destination",
		"SHOPPING":    "Shopping destination",
	}
	if l, ok := labels[category]; ok {
		return l
	}
	return "Local highlight"
}

// categoryIcon maps an Amadeus POI category to a generic icon name used by the template.
func categoryIcon(category string) string {
	icons := map[string]string{
		"SIGHTS":      "landmark",
		"BEACH_PARK":  "umbrella-beach",
		"HISTORICAL":  "monument",
		"NIGHTLIFE":   "music",
		"RESTAURANT":  "utensils",
		"SHOPPING":    "shopping-bag",
	}
	if i, ok := icons[category]; ok {
		return i
	}
	return "star"
}
