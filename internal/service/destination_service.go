package service

import (
	"context"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/realestayer/v3/internal/models"
	"github.com/realestayer/v3/internal/repository"
)

type DestinationService struct {
	repo *repository.DestinationRepository
}

func NewDestinationService(repo *repository.DestinationRepository) *DestinationService {
	return &DestinationService{repo: repo}
}

func (s *DestinationService) GetDestination(ctx context.Context, id string) (*models.Destination, error) {
	oid, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, err
	}
	return s.repo.FindByID(ctx, oid)
}

func (s *DestinationService) GetDestinationByName(ctx context.Context, name string) (*models.Destination, error) {
	return s.repo.FindByName(ctx, name)
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

// GetDefaultDestinations returns hardcoded popular destinations when DB is empty
func (s *DestinationService) GetDefaultDestinations() []models.Destination {
	return []models.Destination{
		{
			Name:        "Paris",
			Country:     "France",
			CountryCode: "FR",
			Region:      "Europe",
			Description: "The City of Light captivates with its iconic landmarks, world-class cuisine, and romantic ambiance.",
			ImageURL:    "https://images.unsplash.com/photo-1502602898657-3e91760cbb34?w=800",
			AirportCode: "CDG",
			Categories:  []string{"city", "romantic", "cultural"},
			BestFor:     []string{"couples", "solo", "culture-lovers"},
			BestMonths:  []int{4, 5, 6, 9, 10},
			AvgDailyBudget: 180,
			Currency:    "EUR",
			PopularityScore: 98,
			Featured:    true,
			Highlights: []models.DestinationHighlight{
				{Title: "Eiffel Tower", Description: "Iconic iron lattice tower", Icon: "landmark"},
				{Title: "Louvre Museum", Description: "World's largest art museum", Icon: "museum"},
				{Title: "French Cuisine", Description: "Michelin-starred dining", Icon: "utensils"},
			},
		},
		{
			Name:        "Tokyo",
			Country:     "Japan",
			CountryCode: "JP",
			Region:      "Asia",
			Description: "A mesmerizing blend of ancient traditions and cutting-edge technology in the world's largest metropolis.",
			ImageURL:    "https://images.unsplash.com/photo-1540959733332-eab4deabeeaf?w=800",
			AirportCode: "NRT",
			Categories:  []string{"city", "cultural", "food"},
			BestFor:     []string{"solo", "couples", "foodies"},
			BestMonths:  []int{3, 4, 10, 11},
			AvgDailyBudget: 150,
			Currency:    "JPY",
			PopularityScore: 95,
			Featured:    true,
			Highlights: []models.DestinationHighlight{
				{Title: "Shibuya Crossing", Description: "World's busiest intersection", Icon: "road"},
				{Title: "Ancient Temples", Description: "Historic shrines and gardens", Icon: "temple"},
				{Title: "Sushi & Ramen", Description: "Authentic Japanese cuisine", Icon: "utensils"},
			},
		},
		{
			Name:        "Bali",
			Country:     "Indonesia",
			CountryCode: "ID",
			Region:      "Asia",
			Description: "Tropical paradise with stunning beaches, ancient temples, and lush rice terraces.",
			ImageURL:    "https://images.unsplash.com/photo-1537996194471-e657df975ab4?w=800",
			AirportCode: "DPS",
			Categories:  []string{"beach", "spiritual", "nature"},
			BestFor:     []string{"couples", "solo", "wellness"},
			BestMonths:  []int{4, 5, 6, 7, 8, 9},
			AvgDailyBudget: 80,
			Currency:    "IDR",
			PopularityScore: 92,
			Featured:    true,
			Highlights: []models.DestinationHighlight{
				{Title: "Rice Terraces", Description: "Iconic Tegallalang views", Icon: "leaf"},
				{Title: "Beach Clubs", Description: "World-famous sunset spots", Icon: "umbrella-beach"},
				{Title: "Yoga & Wellness", Description: "Spiritual retreat center", Icon: "spa"},
			},
		},
		{
			Name:        "New York City",
			Country:     "United States",
			CountryCode: "US",
			Region:      "North America",
			Description: "The city that never sleeps offers world-class entertainment, dining, and endless energy.",
			ImageURL:    "https://images.unsplash.com/photo-1496442226666-8d4d0e62e6e9?w=800",
			AirportCode: "JFK",
			Categories:  []string{"city", "cultural", "entertainment"},
			BestFor:     []string{"solo", "couples", "families"},
			BestMonths:  []int{4, 5, 6, 9, 10, 12},
			AvgDailyBudget: 250,
			Currency:    "USD",
			PopularityScore: 96,
			Featured:    true,
			Highlights: []models.DestinationHighlight{
				{Title: "Times Square", Description: "The crossroads of the world", Icon: "city"},
				{Title: "Central Park", Description: "843 acres of urban oasis", Icon: "tree"},
				{Title: "Broadway", Description: "World-renowned theater", Icon: "theater-masks"},
			},
		},
		{
			Name:        "Santorini",
			Country:     "Greece",
			CountryCode: "GR",
			Region:      "Europe",
			Description: "Breathtaking sunsets, whitewashed villages, and crystal-clear waters on this volcanic island.",
			ImageURL:    "https://images.unsplash.com/photo-1570077188670-e3a8d69ac5ff?w=800",
			AirportCode: "JTR",
			Categories:  []string{"beach", "romantic", "island"},
			BestFor:     []string{"couples", "honeymooners"},
			BestMonths:  []int{5, 6, 9, 10},
			AvgDailyBudget: 200,
			Currency:    "EUR",
			PopularityScore: 90,
			Featured:    true,
			Highlights: []models.DestinationHighlight{
				{Title: "Oia Sunset", Description: "Most photographed sunset", Icon: "sun"},
				{Title: "Caldera Views", Description: "Volcanic crater panoramas", Icon: "mountain"},
				{Title: "Wine Tasting", Description: "Unique volcanic wines", Icon: "wine-glass"},
			},
		},
		{
			Name:        "Dubai",
			Country:     "United Arab Emirates",
			CountryCode: "AE",
			Region:      "Middle East",
			Description: "Futuristic skyscrapers, luxury shopping, and desert adventures in this modern marvel.",
			ImageURL:    "https://images.unsplash.com/photo-1512453979798-5ea266f8880c?w=800",
			AirportCode: "DXB",
			Categories:  []string{"city", "luxury", "shopping"},
			BestFor:     []string{"families", "couples", "luxury-seekers"},
			BestMonths:  []int{11, 12, 1, 2, 3},
			AvgDailyBudget: 300,
			Currency:    "AED",
			PopularityScore: 88,
			Featured:    true,
			Highlights: []models.DestinationHighlight{
				{Title: "Burj Khalifa", Description: "World's tallest building", Icon: "building"},
				{Title: "Desert Safari", Description: "Dune bashing adventure", Icon: "car"},
				{Title: "Dubai Mall", Description: "Luxury shopping paradise", Icon: "shopping-bag"},
			},
		},
		{
			Name:        "Barcelona",
			Country:     "Spain",
			CountryCode: "ES",
			Region:      "Europe",
			Description: "Gaudí's architectural wonders, vibrant nightlife, and Mediterranean beaches.",
			ImageURL:    "https://images.unsplash.com/photo-1583422409516-2895a77efded?w=800",
			AirportCode: "BCN",
			Categories:  []string{"city", "beach", "cultural"},
			BestFor:     []string{"couples", "solo", "foodies"},
			BestMonths:  []int{5, 6, 9, 10},
			AvgDailyBudget: 140,
			Currency:    "EUR",
			PopularityScore: 89,
			Featured:    false,
			Highlights: []models.DestinationHighlight{
				{Title: "Sagrada Família", Description: "Gaudí's masterpiece", Icon: "church"},
				{Title: "La Rambla", Description: "Famous tree-lined street", Icon: "road"},
				{Title: "Tapas & Wine", Description: "Spanish culinary delights", Icon: "utensils"},
			},
		},
		{
			Name:        "Maldives",
			Country:     "Maldives",
			CountryCode: "MV",
			Region:      "Asia",
			Description: "Pristine overwater villas, turquoise lagoons, and world-class diving in paradise.",
			ImageURL:    "https://images.unsplash.com/photo-1514282401047-d79a71a590e8?w=800",
			AirportCode: "MLE",
			Categories:  []string{"beach", "luxury", "island"},
			BestFor:     []string{"honeymooners", "couples", "luxury-seekers"},
			BestMonths:  []int{1, 2, 3, 4, 11, 12},
			AvgDailyBudget: 500,
			Currency:    "USD",
			PopularityScore: 87,
			Featured:    false,
			Highlights: []models.DestinationHighlight{
				{Title: "Overwater Villas", Description: "Iconic luxury stays", Icon: "home"},
				{Title: "Snorkeling", Description: "Vibrant coral reefs", Icon: "fish"},
				{Title: "Private Islands", Description: "Exclusive resort experiences", Icon: "umbrella-beach"},
			},
		},
		{
			Name:        "Machu Picchu",
			Country:     "Peru",
			CountryCode: "PE",
			Region:      "South America",
			Description: "Ancient Incan citadel set high in the Andes Mountains, a wonder of the world.",
			ImageURL:    "https://images.unsplash.com/photo-1587595431973-160d0d94add1?w=800",
			AirportCode: "CUZ",
			Categories:  []string{"adventure", "cultural", "nature"},
			BestFor:     []string{"adventurers", "solo", "history-buffs"},
			BestMonths:  []int{4, 5, 6, 7, 8, 9, 10},
			AvgDailyBudget: 100,
			Currency:    "PEN",
			PopularityScore: 85,
			Featured:    false,
			Highlights: []models.DestinationHighlight{
				{Title: "Inca Trail", Description: "Famous 4-day trek", Icon: "hiking"},
				{Title: "Sacred Valley", Description: "Ancient Incan sites", Icon: "monument"},
				{Title: "Cusco", Description: "Historic capital city", Icon: "city"},
			},
		},
		{
			Name:        "Sydney",
			Country:     "Australia",
			CountryCode: "AU",
			Region:      "Oceania",
			Description: "Iconic harbor, stunning beaches, and vibrant culture in Australia's largest city.",
			ImageURL:    "https://images.unsplash.com/photo-1506973035872-a4ec16b8e8d9?w=800",
			AirportCode: "SYD",
			Categories:  []string{"city", "beach", "nature"},
			BestFor:     []string{"families", "couples", "adventurers"},
			BestMonths:  []int{9, 10, 11, 12, 1, 2, 3},
			AvgDailyBudget: 180,
			Currency:    "AUD",
			PopularityScore: 86,
			Featured:    false,
			Highlights: []models.DestinationHighlight{
				{Title: "Opera House", Description: "Architectural icon", Icon: "landmark"},
				{Title: "Bondi Beach", Description: "World-famous surf spot", Icon: "umbrella-beach"},
				{Title: "Blue Mountains", Description: "Stunning natural beauty", Icon: "mountain"},
			},
		},
		{
			Name:        "Reykjavik",
			Country:     "Iceland",
			CountryCode: "IS",
			Region:      "Europe",
			Description: "Gateway to dramatic landscapes, Northern Lights, and geothermal wonders.",
			ImageURL:    "https://images.unsplash.com/photo-1504829857797-ddff29c27927?w=800",
			AirportCode: "KEF",
			Categories:  []string{"adventure", "nature", "unique"},
			BestFor:     []string{"adventurers", "couples", "photographers"},
			BestMonths:  []int{6, 7, 8, 9, 10, 11, 12, 1, 2, 3},
			AvgDailyBudget: 220,
			Currency:    "ISK",
			PopularityScore: 82,
			Featured:    false,
			Highlights: []models.DestinationHighlight{
				{Title: "Northern Lights", Description: "Aurora Borealis viewing", Icon: "star"},
				{Title: "Blue Lagoon", Description: "Geothermal spa", Icon: "spa"},
				{Title: "Golden Circle", Description: "Famous day tour", Icon: "road"},
			},
		},
		{
			Name:        "Cape Town",
			Country:     "South Africa",
			CountryCode: "ZA",
			Region:      "Africa",
			Description: "Where mountains meet the ocean, with stunning nature and rich culture.",
			ImageURL:    "https://images.unsplash.com/photo-1580060839134-75a5edca2e99?w=800",
			AirportCode: "CPT",
			Categories:  []string{"nature", "beach", "adventure"},
			BestFor:     []string{"adventurers", "couples", "families"},
			BestMonths:  []int{10, 11, 12, 1, 2, 3, 4},
			AvgDailyBudget: 120,
			Currency:    "ZAR",
			PopularityScore: 83,
			Featured:    false,
			Highlights: []models.DestinationHighlight{
				{Title: "Table Mountain", Description: "Iconic flat-topped peak", Icon: "mountain"},
				{Title: "Wine Country", Description: "World-class vineyards", Icon: "wine-glass"},
				{Title: "Beaches", Description: "Stunning coastal scenery", Icon: "umbrella-beach"},
			},
		},
	}
}
