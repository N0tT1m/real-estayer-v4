package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/realestayer/v3/internal/models"
	"github.com/realestayer/v3/internal/provider/amadeus"
	"github.com/realestayer/v3/internal/provider/wikipedia"
	"github.com/realestayer/v3/internal/repository"
)

// seedCities is a minimal config: just the city name, country code, region, and categories.
// All descriptions and images come from Wikipedia at seed time — nothing is hardcoded here.
var seedCities = []struct {
	Name        string
	CountryCode string
	Region      string
	Categories  []string
}{
	// Europe
	{"Paris", "FR", "Europe", []string{"city", "romantic", "cultural"}},
	{"London", "GB", "Europe", []string{"city", "cultural", "historical"}},
	{"Rome", "IT", "Europe", []string{"city", "cultural", "historical"}},
	{"Barcelona", "ES", "Europe", []string{"city", "beach", "cultural"}},
	{"Amsterdam", "NL", "Europe", []string{"city", "cultural", "romantic"}},
	{"Prague", "CZ", "Europe", []string{"city", "historical", "romantic"}},
	{"Lisbon", "PT", "Europe", []string{"city", "cultural", "beach"}},
	{"Istanbul", "TR", "Europe", []string{"city", "cultural", "historical"}},
	{"Vienna", "AT", "Europe", []string{"city", "cultural", "romantic"}},
	{"Athens", "GR", "Europe", []string{"city", "historical", "cultural"}},
	{"Berlin", "DE", "Europe", []string{"city", "cultural", "nightlife"}},
	{"Madrid", "ES", "Europe", []string{"city", "cultural", "food"}},
	{"Florence", "IT", "Europe", []string{"city", "cultural", "romantic"}},
	{"Dubrovnik", "HR", "Europe", []string{"city", "beach", "historical"}},
	{"Santorini", "GR", "Europe", []string{"island", "romantic", "beach"}},
	{"Reykjavik", "IS", "Europe", []string{"adventure", "nature", "city"}},
	{"Edinburgh", "GB", "Europe", []string{"city", "historical", "cultural"}},
	{"Budapest", "HU", "Europe", []string{"city", "historical", "romantic"}},
	{"Zurich", "CH", "Europe", []string{"city", "luxury", "nature"}},
	{"Copenhagen", "DK", "Europe", []string{"city", "cultural", "food"}},
	{"Stockholm", "SE", "Europe", []string{"city", "cultural", "nature"}},
	{"Porto", "PT", "Europe", []string{"city", "cultural", "food"}},
	{"Bruges", "BE", "Europe", []string{"city", "historical", "romantic"}},
	{"Mykonos", "GR", "Europe", []string{"island", "beach", "nightlife"}},
	{"Nice", "FR", "Europe", []string{"city", "beach", "luxury"}},
	// Asia
	{"Tokyo", "JP", "Asia", []string{"city", "cultural", "food"}},
	{"Bangkok", "TH", "Asia", []string{"city", "cultural", "food"}},
	{"Bali", "ID", "Asia", []string{"beach", "nature", "spiritual"}},
	{"Singapore", "SG", "Asia", []string{"city", "luxury", "food"}},
	{"Seoul", "KR", "Asia", []string{"city", "cultural", "food"}},
	{"Kuala Lumpur", "MY", "Asia", []string{"city", "cultural", "food"}},
	{"Hong Kong", "HK", "Asia", []string{"city", "cultural", "luxury"}},
	{"Mumbai", "IN", "Asia", []string{"city", "cultural", "food"}},
	{"Kyoto", "JP", "Asia", []string{"city", "cultural", "historical"}},
	{"Phuket", "TH", "Asia", []string{"beach", "island", "nightlife"}},
	{"Hanoi", "VN", "Asia", []string{"city", "cultural", "food"}},
	{"Ho Chi Minh City", "VN", "Asia", []string{"city", "cultural", "food"}},
	{"Taipei", "TW", "Asia", []string{"city", "cultural", "food"}},
	{"Maldives", "MV", "Asia", []string{"island", "beach", "luxury"}},
	{"Colombo", "LK", "Asia", []string{"city", "cultural", "beach"}},
	{"Kathmandu", "NP", "Asia", []string{"city", "historical", "adventure"}},
	{"Chiang Mai", "TH", "Asia", []string{"city", "cultural", "nature"}},
	{"Osaka", "JP", "Asia", []string{"city", "food", "cultural"}},
	{"Delhi", "IN", "Asia", []string{"city", "historical", "cultural"}},
	{"Goa", "IN", "Asia", []string{"beach", "nature", "nightlife"}},
	{"Yangon", "MM", "Asia", []string{"city", "historical", "cultural"}},
	// North America
	{"New York City", "US", "North America", []string{"city", "cultural", "entertainment"}},
	{"Los Angeles", "US", "North America", []string{"city", "beach", "entertainment"}},
	{"Miami", "US", "North America", []string{"beach", "city", "nightlife"}},
	{"Chicago", "US", "North America", []string{"city", "cultural", "food"}},
	{"Toronto", "CA", "North America", []string{"city", "cultural", "food"}},
	{"Vancouver", "CA", "North America", []string{"nature", "city", "adventure"}},
	{"Mexico City", "MX", "North America", []string{"city", "cultural", "food"}},
	{"Las Vegas", "US", "North America", []string{"city", "entertainment", "luxury"}},
	{"Cancun", "MX", "North America", []string{"beach", "island", "nightlife"}},
	{"New Orleans", "US", "North America", []string{"city", "cultural", "food"}},
	{"San Francisco", "US", "North America", []string{"city", "cultural", "nature"}},
	{"Montreal", "CA", "North America", []string{"city", "cultural", "food"}},
	{"Havana", "CU", "North America", []string{"city", "historical", "cultural"}},
	{"San Jose", "CR", "North America", []string{"city", "nature", "adventure"}},
	{"Seattle", "US", "North America", []string{"city", "nature", "food"}},
	{"Boston", "US", "North America", []string{"city", "historical", "cultural"}},
	// South America
	{"Rio de Janeiro", "BR", "South America", []string{"beach", "city", "cultural"}},
	{"Buenos Aires", "AR", "South America", []string{"city", "cultural", "food"}},
	{"Bogota", "CO", "South America", []string{"city", "cultural", "adventure"}},
	{"Lima", "PE", "South America", []string{"city", "cultural", "food"}},
	{"Cartagena", "CO", "South America", []string{"city", "beach", "historical"}},
	{"Cusco", "PE", "South America", []string{"historical", "adventure", "cultural"}},
	{"Santiago", "CL", "South America", []string{"city", "cultural", "nature"}},
	{"Montevideo", "UY", "South America", []string{"city", "cultural", "beach"}},
	{"Quito", "EC", "South America", []string{"city", "historical", "adventure"}},
	{"Medellin", "CO", "South America", []string{"city", "cultural", "nature"}},
	// Africa
	{"Cape Town", "ZA", "Africa", []string{"nature", "beach", "adventure"}},
	{"Marrakech", "MA", "Africa", []string{"cultural", "adventure", "city"}},
	{"Cairo", "EG", "Africa", []string{"historical", "cultural", "city"}},
	{"Nairobi", "KE", "Africa", []string{"nature", "adventure", "city"}},
	{"Zanzibar", "TZ", "Africa", []string{"island", "beach", "cultural"}},
	{"Casablanca", "MA", "Africa", []string{"city", "cultural", "historical"}},
	{"Accra", "GH", "Africa", []string{"city", "cultural", "beach"}},
	{"Tunis", "TN", "Africa", []string{"city", "historical", "cultural"}},
	{"Johannesburg", "ZA", "Africa", []string{"city", "cultural", "adventure"}},
	{"Mauritius", "MU", "Africa", []string{"island", "beach", "luxury"}},
	// Middle East
	{"Dubai", "AE", "Middle East", []string{"city", "luxury", "shopping"}},
	{"Abu Dhabi", "AE", "Middle East", []string{"city", "luxury", "cultural"}},
	{"Doha", "QA", "Middle East", []string{"city", "luxury", "cultural"}},
	{"Petra", "JO", "Middle East", []string{"historical", "adventure", "cultural"}},
	{"Muscat", "OM", "Middle East", []string{"city", "cultural", "beach"}},
	{"Amman", "JO", "Middle East", []string{"city", "historical", "cultural"}},
	{"Beirut", "LB", "Middle East", []string{"city", "cultural", "food"}},
	{"Tel Aviv", "IL", "Middle East", []string{"city", "beach", "cultural"}},
	// Oceania
	{"Sydney", "AU", "Oceania", []string{"city", "beach", "nature"}},
	{"Melbourne", "AU", "Oceania", []string{"city", "cultural", "food"}},
	{"Auckland", "NZ", "Oceania", []string{"city", "nature", "adventure"}},
	{"Brisbane", "AU", "Oceania", []string{"city", "beach", "nature"}},
	{"Queenstown", "NZ", "Oceania", []string{"adventure", "nature", "romantic"}},
	{"Fiji", "FJ", "Oceania", []string{"island", "beach", "luxury"}},
	{"Bora Bora", "PF", "Oceania", []string{"island", "beach", "romantic"}},
}

// regionBudgets holds a typical average daily budget (USD) per region.
var regionBudgets = map[string]float64{
	"Europe":        150,
	"Asia":          90,
	"North America": 200,
	"South America": 80,
	"Africa":        85,
	"Middle East":   180,
	"Oceania":       170,
}

// wikipediaNames maps seed city names to their Wikipedia article title when they differ.
var wikipediaNames = map[string]string{
	"New York City":    "New York City",
	"Bali":             "Bali",
	"Maldives":         "Maldives",
	"Ho Chi Minh City": "Ho Chi Minh City",
	"Santorini":        "Santorini",
	"Petra":            "Petra, Jordan",
	"Mauritius":        "Mauritius",
	"Fiji":             "Fiji",
	"Bora Bora":        "Bora Bora",
	"Goa":              "Goa",
	"Zanzibar":         "Zanzibar",
}

type DestinationService struct {
	repo          *repository.DestinationRepository
	amadeusClient *amadeus.Client
	wikiClient    *wikipedia.Client
}

func NewDestinationService(repo *repository.DestinationRepository, amadeusClient *amadeus.Client) *DestinationService {
	return &DestinationService{
		repo:          repo,
		amadeusClient: amadeusClient,
		wikiClient:    wikipedia.NewClient(),
	}
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

	// Not in DB — look up live and cache it.
	return s.fetchAndCache(ctx, name, "", "")
}

// AddDestination looks up a city by name from Amadeus + Wikipedia and stores it in MongoDB.
// This is used by the admin API to add any new city without touching code.
func (s *DestinationService) AddDestination(ctx context.Context, cityName, countryCode, region string) (*models.Destination, error) {
	return s.fetchAndCache(ctx, cityName, countryCode, region)
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

// SeedFromAmadeus seeds/refreshes all destinations in MongoDB.
// Amadeus provides location data; Wikipedia provides descriptions and images.
func (s *DestinationService) SeedFromAmadeus(ctx context.Context) error {
	if s.amadeusClient == nil {
		return fmt.Errorf("amadeus client not configured")
	}

	seeded := 0
	for _, city := range seedCities {
		amResult, err := s.amadeusClient.SearchCity(ctx, city.Name, city.CountryCode)
		if err != nil {
			slog.Warn("destination seed: amadeus lookup failed", "city", city.Name, "error", err)
			continue
		}

		cityName := formatCityName(amResult.Name, city.Name)
		countryName := formatCountryName(amResult.CountryName)

		description, imageURL := s.enrichFromWikipedia(ctx, city.Name)

		budget := regionBudgets[city.Region]
		if budget == 0 {
			budget = 100
		}

		dest := &models.Destination{
			Name:            cityName,
			Country:         countryName,
			CountryCode:     amResult.CountryCode,
			Region:          city.Region,
			AirportCode:     amResult.IataCode,
			Latitude:        amResult.Latitude,
			Longitude:       amResult.Longitude,
			Description:     description,
			ImageURL:        imageURL,
			Categories:      city.Categories,
			AvgDailyBudget:  budget,
			Currency:        "USD",
			PopularityScore: 70,
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

// GetHighlights fetches live Points of Interest from Amadeus for a destination.
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

// fetchAndCache looks up a city from Amadeus + Wikipedia and stores it in MongoDB.
func (s *DestinationService) fetchAndCache(ctx context.Context, name, countryCode, region string) (*models.Destination, error) {
	if s.amadeusClient == nil {
		return nil, fmt.Errorf("amadeus client not configured")
	}

	amResult, err := s.amadeusClient.SearchCity(ctx, name, countryCode)
	if err != nil {
		return nil, fmt.Errorf("city not found: %s", name)
	}

	if region == "" {
		region = regionForCountry(amResult.CountryCode)
	}

	cityName := formatCityName(amResult.Name, name)
	countryName := formatCountryName(amResult.CountryName)
	description, imageURL := s.enrichFromWikipedia(ctx, name)

	budget := regionBudgets[region]
	if budget == 0 {
		budget = 100
	}

	dest := &models.Destination{
		Name:            cityName,
		Country:         countryName,
		CountryCode:     amResult.CountryCode,
		Region:          region,
		AirportCode:     amResult.IataCode,
		Latitude:        amResult.Latitude,
		Longitude:       amResult.Longitude,
		Description:     description,
		ImageURL:        imageURL,
		AvgDailyBudget:  budget,
		Currency:        "USD",
		PopularityScore: 70,
	}

	_ = s.repo.Upsert(ctx, dest)
	return dest, nil
}

// enrichFromWikipedia fetches a city description and image from Wikipedia.
// Falls back to a generic description if Wikipedia is unavailable.
func (s *DestinationService) enrichFromWikipedia(ctx context.Context, cityName string) (description, imageURL string) {
	wikiTitle := cityName
	if mapped, ok := wikipediaNames[cityName]; ok {
		wikiTitle = mapped
	}

	info, err := s.wikiClient.GetCitySummary(ctx, wikiTitle)
	if err != nil {
		slog.Warn("destination seed: wikipedia lookup failed", "city", cityName, "error", err)
		description = fmt.Sprintf("%s is a popular travel destination with unique culture, history, and experiences for every type of traveller.", cityName)
		imageURL = "https://images.unsplash.com/photo-1488646953014-85cb44e25828?w=800&q=80&fit=crop"
		return
	}

	description = info.Description
	imageURL = info.ImageURL
	return
}

// --- helpers ---

var countryRegions = map[string]string{
	"FR": "Europe", "GB": "Europe", "DE": "Europe", "IT": "Europe",
	"ES": "Europe", "NL": "Europe", "PT": "Europe", "GR": "Europe",
	"AT": "Europe", "CH": "Europe", "BE": "Europe", "SE": "Europe",
	"NO": "Europe", "DK": "Europe", "FI": "Europe", "PL": "Europe",
	"CZ": "Europe", "HU": "Europe", "HR": "Europe", "IS": "Europe",
	"TR": "Europe", "RO": "Europe", "BG": "Europe", "SK": "Europe",
	"JP": "Asia", "CN": "Asia", "IN": "Asia", "TH": "Asia",
	"ID": "Asia", "MY": "Asia", "SG": "Asia", "VN": "Asia",
	"PH": "Asia", "KR": "Asia", "HK": "Asia", "TW": "Asia",
	"MV": "Asia", "LK": "Asia", "NP": "Asia", "MM": "Asia",
	"US": "North America", "CA": "North America", "MX": "North America",
	"CR": "North America", "PA": "North America", "CU": "North America",
	"JM": "North America", "DO": "North America",
	"BR": "South America", "AR": "South America", "CO": "South America",
	"PE": "South America", "CL": "South America", "EC": "South America",
	"BO": "South America", "UY": "South America", "PY": "South America",
	"ZA": "Africa", "MA": "Africa", "EG": "Africa", "KE": "Africa",
	"TZ": "Africa", "GH": "Africa", "NG": "Africa", "ET": "Africa",
	"SN": "Africa", "TN": "Africa", "MU": "Africa", "CI": "Africa",
	"AE": "Middle East", "SA": "Middle East", "QA": "Middle East",
	"BH": "Middle East", "KW": "Middle East", "OM": "Middle East",
	"IL": "Middle East", "JO": "Middle East", "LB": "Middle East",
	"AU": "Oceania", "NZ": "Oceania", "FJ": "Oceania", "PG": "Oceania",
	"PF": "Oceania",
}

func regionForCountry(countryCode string) string {
	if r, ok := countryRegions[countryCode]; ok {
		return r
	}
	return "Other"
}

func formatCityName(amadeusName, seedName string) string {
	if amadeusName == strings.ToUpper(amadeusName) && len(seedName) > 0 {
		return seedName
	}
	return amadeusName
}

func formatCountryName(name string) string {
	if name == strings.ToUpper(name) {
		return strings.Title(strings.ToLower(name))
	}
	return name
}

func categoryLabel(category string) string {
	labels := map[string]string{
		"SIGHTS":     "Popular attraction",
		"BEACH_PARK": "Beach or park",
		"HISTORICAL": "Historic site",
		"NIGHTLIFE":  "Nightlife spot",
		"RESTAURANT": "Dining destination",
		"SHOPPING":   "Shopping destination",
	}
	if l, ok := labels[category]; ok {
		return l
	}
	return "Local highlight"
}

func categoryIcon(category string) string {
	icons := map[string]string{
		"SIGHTS":     "landmark",
		"BEACH_PARK": "umbrella-beach",
		"HISTORICAL": "monument",
		"NIGHTLIFE":  "music",
		"RESTAURANT": "utensils",
		"SHOPPING":   "shopping-bag",
	}
	if i, ok := icons[category]; ok {
		return i
	}
	return "star"
}
