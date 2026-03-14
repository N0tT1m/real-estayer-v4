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
// Coordinates, IATA codes, and country data come from Amadeus; only the metadata below is static.
var seedCities = []struct {
	Name       string
	Region     string
	Categories []string
	ImageID    string // Unsplash photo ID
}{
	// Europe
	{"Paris", "Europe", []string{"city", "romantic", "cultural"}, "1502602898657-3e91760cbb34"},
	{"London", "Europe", []string{"city", "cultural", "historical"}, "1513635269975-59663e0ac1ad"},
	{"Rome", "Europe", []string{"city", "cultural", "historical"}, "1552832230-c0197dd311b5"},
	{"Barcelona", "Europe", []string{"city", "beach", "cultural"}, "1583422409516-2895a77efded"},
	{"Amsterdam", "Europe", []string{"city", "cultural", "romantic"}, "1534351590666-13e3e96b5017"},
	{"Prague", "Europe", []string{"city", "historical", "romantic"}, "1519677100203-a0e668c92439"},
	{"Lisbon", "Europe", []string{"city", "cultural", "beach"}, "1555881400-74d7acaacd47"},
	{"Istanbul", "Europe", []string{"city", "cultural", "historical"}, "1524231757912-21f4fe3a7200"},
	{"Vienna", "Europe", []string{"city", "cultural", "romantic"}, "1516550135131-5b4b2f5c5b5e"},
	{"Athens", "Europe", []string{"city", "historical", "cultural"}, "1555993539-1732b0258f8a"},
	// Asia
	{"Tokyo", "Asia", []string{"city", "cultural", "food"}, "1540959733332-eab4deabeeaf"},
	{"Bangkok", "Asia", []string{"city", "cultural", "food"}, "1508009603885-50cf7c579365"},
	{"Bali", "Asia", []string{"beach", "nature", "spiritual"}, "1537996194471-e657df975ab4"},
	{"Singapore", "Asia", []string{"city", "luxury", "food"}, "1525625293386-3f8f99389edd"},
	{"Seoul", "Asia", []string{"city", "cultural", "food"}, "1517154421773-0855dc628f9f"},
	{"Kuala Lumpur", "Asia", []string{"city", "cultural", "food"}, "1596422846543-cb4fc8fc79e7"},
	{"Hong Kong", "Asia", []string{"city", "cultural", "luxury"}, "1536599018102-9f803c140fc1"},
	{"Mumbai", "Asia", []string{"city", "cultural", "food"}, "1570168007204-dfb528c6958f"},
	// North America
	{"New York", "North America", []string{"city", "cultural", "entertainment"}, "1496442226666-8d4d0e62e6e9"},
	{"Los Angeles", "North America", []string{"city", "beach", "entertainment"}, "1444723121867-7a241cacace9"},
	{"Miami", "North America", []string{"beach", "city", "nightlife"}, "1533106497176-45ae19e68ba2"},
	{"Chicago", "North America", []string{"city", "cultural", "food"}, "1477959858617-67f85cf4f1df"},
	{"Toronto", "North America", []string{"city", "cultural", "food"}, "1517935706615-2717063c2225"},
	{"Vancouver", "North America", []string{"nature", "city", "adventure"}, "1559511260-66a654ae982a"},
	{"Mexico City", "North America", []string{"city", "cultural", "food"}, "1518105779142-d975f22f1b0a"},
	{"Las Vegas", "North America", []string{"city", "entertainment", "luxury"}, "1506665523900-b94e4cf4a6ea"},
	// South America
	{"Rio de Janeiro", "South America", []string{"beach", "city", "cultural"}, "1483729558449-99ef09a8c325"},
	{"Buenos Aires", "South America", []string{"city", "cultural", "food"}, "1585208798174-6cedd86e019a"},
	{"Bogota", "South America", []string{"city", "cultural", "adventure"}, "1597635742116-5bb30c1d2d85"},
	{"Lima", "South America", []string{"city", "cultural", "food"}, "1609962234813-aa7e8286c4a8"},
	// Africa
	{"Cape Town", "Africa", []string{"nature", "beach", "adventure"}, "1580060839134-75a5edca2e99"},
	{"Marrakech", "Africa", []string{"cultural", "adventure", "city"}, "1539020140153-e479b8c22e70"},
	{"Cairo", "Africa", []string{"historical", "cultural", "city"}, "1572252009286-96463070674e"},
	{"Nairobi", "Africa", []string{"nature", "adventure", "city"}, "1611348586804-61bf6c080437"},
	// Middle East
	{"Dubai", "Middle East", []string{"city", "luxury", "shopping"}, "1512453979798-5ea266f8880c"},
	{"Abu Dhabi", "Middle East", []string{"city", "luxury", "cultural"}, "1604928141064-207cea6f571f"},
	{"Doha", "Middle East", []string{"city", "luxury", "cultural"}, "1570197788417-0e82375c9371"},
	// Oceania
	{"Sydney", "Oceania", []string{"city", "beach", "nature"}, "1506973035872-a4ec16b8e8d9"},
	{"Melbourne", "Oceania", []string{"city", "cultural", "food"}, "1545044846-351ba102b6d5"},
	{"Auckland", "Oceania", []string{"city", "nature", "adventure"}, "1507699622322-13fe651affd3"},
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
		ImageURL:       unsplashFallback(name),
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
// Existing documents are updated with the latest metadata (categories, images, etc.).
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

		cityName := formatCityName(result.Name, city.Name)
		countryName := formatCountryName(result.CountryName)
		dest := &models.Destination{
			Name:            cityName,
			Country:         countryName,
			CountryCode:     result.CountryCode,
			Region:          region,
			AirportCode:     result.IataCode,
			Latitude:        result.Latitude,
			Longitude:       result.Longitude,
			Description:     fmt.Sprintf("Discover %s, a vibrant destination in %s with unforgettable experiences awaiting every traveller.", cityName, countryName),
			ImageURL:        unsplashURL(city.ImageID),
			Categories:      city.Categories,
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

// unsplashURL returns a direct Unsplash image URL from a known photo ID.
func unsplashURL(photoID string) string {
	return fmt.Sprintf("https://images.unsplash.com/photo-%s?w=800&q=80&fit=crop", photoID)
}

// unsplashFallback returns a generic travel image for cities without a known photo ID.
func unsplashFallback(city string) string {
	slug := strings.ToLower(strings.ReplaceAll(city, " ", "+"))
	// Use a stable curated travel photo as fallback
	_ = slug
	return "https://images.unsplash.com/photo-1488646953014-85cb44e25828?w=800&q=80&fit=crop"
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
