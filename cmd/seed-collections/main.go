// seed-collections inserts a starter set of curated destination guides so the
// Collections feature has content out of the box. Safe to re-run — each
// collection is upserted by slug.
//
// Usage: go run ./cmd/seed-collections
package main

import (
	"context"
	"log/slog"
	"os"
	"time"

	"github.com/realestayer/v4/internal/config"
	"github.com/realestayer/v4/internal/database"
	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/repository"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	cfg, err := config.Load()
	if err != nil {
		slog.Error("config", "error", err)
		os.Exit(1)
	}
	db, err := database.Connect(cfg.MongoURI)
	if err != nil {
		slog.Error("db connect", "error", err)
		os.Exit(1)
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = db.Disconnect(ctx)
	}()

	repo := repository.NewCollectionRepository(db)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	for _, c := range starterCollections() {
		if err := repo.UpsertBySlug(ctx, &c); err != nil {
			slog.Error("upsert", "slug", c.Slug, "error", err)
			continue
		}
		slog.Info("seeded", "slug", c.Slug)
	}
}

func starterCollections() []models.Collection {
	return []models.Collection{
		{
			Slug: "12-hours-lisbon", Destination: "Lisbon",
			Title: "12 hours in Lisbon", Subtitle: "A one-day walking loop through Alfama, Baixa and Bairro Alto.",
			Description: "Start early before the heat, pace yourself on the hills, and finish with fado and a glass of vinho verde.",
			DurationHours: 12, Featured: true,
			Tags: []string{"walking", "food", "views"},
			Stops: []models.CollectionStop{
				{Title: "Pastéis de Belém", Category: "food", Description: "Legendary custard tarts — go before the queue builds.", DurationMin: 45, Lat: 38.6976, Lng: -9.2032},
				{Title: "Mosteiro dos Jerónimos", Category: "sight", Description: "Manueline cloisters next door; pair with the pastéis.", DurationMin: 75, Lat: 38.6979, Lng: -9.2065},
				{Title: "Time Out Market", Category: "food", Description: "Lunch — 30 of Lisbon's best cooks under one roof.", DurationMin: 60, Lat: 38.7066, Lng: -9.1457},
				{Title: "Alfama wander", Category: "walk", Description: "Narrow lanes, laundry overhead, tiles everywhere.", DurationMin: 90, Lat: 38.7123, Lng: -9.1303},
				{Title: "Miradouro de Santa Luzia", Category: "viewpoint", Description: "Sunset terrace looking out to the Tagus.", DurationMin: 30, Lat: 38.7118, Lng: -9.1299},
				{Title: "Dinner + fado in Bairro Alto", Category: "food", Description: "Small venue, bottle of wine, no phones.", DurationMin: 150, Lat: 38.7133, Lng: -9.1457},
			},
		},
		{
			Slug: "weekend-tokyo", Destination: "Tokyo",
			Title: "A first weekend in Tokyo", Subtitle: "Two days that sample the city's extremes without crossing town twice.",
			Description: "Shinjuku and Shibuya on Saturday, Asakusa and Ueno on Sunday. Eat as you walk; ride the trains everywhere.",
			DurationHours: 36, Featured: true,
			Tags: []string{"food", "city", "first-visit"},
			Stops: []models.CollectionStop{
				{Title: "Tsukiji Outer Market breakfast", Category: "food", DurationMin: 60, Lat: 35.6654, Lng: 139.7707},
				{Title: "Meiji Jingu", Category: "sight", DurationMin: 60, Lat: 35.6764, Lng: 139.6993},
				{Title: "Omotesando → Harajuku", Category: "walk", DurationMin: 90, Lat: 35.6702, Lng: 139.7020},
				{Title: "Shibuya Scramble + Sky", Category: "viewpoint", DurationMin: 75, Lat: 35.6580, Lng: 139.7016},
				{Title: "Izakaya in Golden Gai", Category: "food", DurationMin: 120, Lat: 35.6938, Lng: 139.7042},
				{Title: "Senso-ji at opening", Category: "sight", DurationMin: 90, Lat: 35.7148, Lng: 139.7967},
				{Title: "Ueno Park + museum", Category: "sight", DurationMin: 180, Lat: 35.7156, Lng: 139.7745},
			},
		},
		{
			Slug: "24-hours-barcelona", Destination: "Barcelona",
			Title: "24 hours in Barcelona", Subtitle: "Modernista architecture, tapas, and the waterfront.",
			DurationHours: 24, Featured: true,
			Tags: []string{"architecture", "food"},
			Stops: []models.CollectionStop{
				{Title: "Sagrada Família (pre-book)", Category: "sight", DurationMin: 90, Lat: 41.4036, Lng: 2.1744},
				{Title: "Casa Batlló", Category: "sight", DurationMin: 60, Lat: 41.3917, Lng: 2.1649},
				{Title: "El Nacional for lunch", Category: "food", DurationMin: 75, Lat: 41.3908, Lng: 2.1669},
				{Title: "Barri Gòtic wander", Category: "walk", DurationMin: 90, Lat: 41.3830, Lng: 2.1770},
				{Title: "Barceloneta beach stroll", Category: "walk", DurationMin: 60, Lat: 41.3781, Lng: 2.1917},
				{Title: "Tapas at Quimet & Quimet", Category: "food", DurationMin: 90, Lat: 41.3740, Lng: 2.1624},
			},
		},

		// More one-liners — one per destination, light on stops but enough
		// to make the rail feel populated. Expand per-city from here.
		{Slug: "48-hours-paris", Destination: "Paris", Title: "48 hours in Paris", Subtitle: "The essentials without hating yourself.", DurationHours: 48, Featured: true, Tags: []string{"classic", "walking"},
			Stops: []models.CollectionStop{
				{Title: "Musée d'Orsay (book 9am)", Category: "sight", DurationMin: 120, Lat: 48.8600, Lng: 2.3266},
				{Title: "Tuileries → Louvre exterior", Category: "walk", DurationMin: 60, Lat: 48.8634, Lng: 2.3285},
				{Title: "Lunch in Le Marais", Category: "food", DurationMin: 75, Lat: 48.8573, Lng: 2.3595},
				{Title: "Notre-Dame exterior + Île de la Cité", Category: "sight", DurationMin: 60, Lat: 48.8530, Lng: 2.3499},
				{Title: "Montmartre at sunset", Category: "viewpoint", DurationMin: 90, Lat: 48.8867, Lng: 2.3431},
				{Title: "Le Comptoir du Relais dinner", Category: "food", DurationMin: 120, Lat: 48.8536, Lng: 2.3382},
			}},
		{Slug: "weekend-rome", Destination: "Rome", Title: "A weekend in Rome", DurationHours: 48, Featured: true, Tags: []string{"classic", "food"},
			Stops: []models.CollectionStop{
				{Title: "Colosseum + Forum", Category: "sight", DurationMin: 180, Lat: 41.8902, Lng: 12.4922},
				{Title: "Pantheon at noon", Category: "sight", DurationMin: 45, Lat: 41.8986, Lng: 12.4769},
				{Title: "Trastevere for dinner", Category: "food", DurationMin: 150, Lat: 41.8896, Lng: 12.4690},
				{Title: "Vatican Museums (book early)", Category: "sight", DurationMin: 210, Lat: 41.9065, Lng: 12.4536},
				{Title: "Gelato at Giolitti", Category: "food", DurationMin: 30, Lat: 41.9011, Lng: 12.4794},
			}},
		{Slug: "72-hours-london", Destination: "London", Title: "Three days in London", DurationHours: 72, Featured: true, Tags: []string{"museums", "walking"},
			Stops: []models.CollectionStop{
				{Title: "British Museum", Category: "sight", DurationMin: 180, Lat: 51.5194, Lng: -0.1270},
				{Title: "Borough Market lunch", Category: "food", DurationMin: 75, Lat: 51.5055, Lng: -0.0909},
				{Title: "Tate Modern + Millennium Bridge", Category: "sight", DurationMin: 150, Lat: 51.5076, Lng: -0.0994},
				{Title: "Southbank walk + Globe", Category: "walk", DurationMin: 90, Lat: 51.5074, Lng: -0.0972},
				{Title: "Hyde Park + V&A", Category: "sight", DurationMin: 180, Lat: 51.5074, Lng: -0.1657},
				{Title: "Dishoom dinner", Category: "food", DurationMin: 120, Lat: 51.5123, Lng: -0.1270},
			}},
		{Slug: "weekend-nyc", Destination: "New York", Title: "A weekend in NYC", DurationHours: 60, Featured: true, Tags: []string{"classic", "city"},
			Stops: []models.CollectionStop{
				{Title: "High Line + Chelsea", Category: "walk", DurationMin: 90, Lat: 40.7480, Lng: -74.0048},
				{Title: "MoMA", Category: "sight", DurationMin: 180, Lat: 40.7614, Lng: -73.9776},
				{Title: "Katz's Deli", Category: "food", DurationMin: 60, Lat: 40.7223, Lng: -73.9874},
				{Title: "Brooklyn Bridge at dusk", Category: "walk", DurationMin: 60, Lat: 40.7061, Lng: -73.9969},
				{Title: "Williamsburg food tour", Category: "food", DurationMin: 120, Lat: 40.7081, Lng: -73.9571},
				{Title: "Central Park loop", Category: "walk", DurationMin: 120, Lat: 40.7829, Lng: -73.9654},
			}},
		{Slug: "weekend-chicago", Destination: "Chicago", Title: "A perfect weekend in Chicago", DurationHours: 48, Featured: true, Tags: []string{"architecture", "food"},
			Stops: []models.CollectionStop{
				{Title: "Architecture river cruise", Category: "sight", DurationMin: 90, Lat: 41.8883, Lng: -87.6263},
				{Title: "Art Institute", Category: "sight", DurationMin: 150, Lat: 41.8796, Lng: -87.6237},
				{Title: "Portillo's for lunch", Category: "food", DurationMin: 45, Lat: 41.8896, Lng: -87.6314},
				{Title: "The Bean + Millennium Park", Category: "sight", DurationMin: 45, Lat: 41.8826, Lng: -87.6226},
				{Title: "Lou Malnati's pizza dinner", Category: "food", DurationMin: 90, Lat: 41.8985, Lng: -87.6333},
			}},
		{Slug: "weekend-sf", Destination: "San Francisco", Title: "A weekend in San Francisco", DurationHours: 48, Featured: true, Tags: []string{"classic"},
			Stops: []models.CollectionStop{
				{Title: "Ferry Building breakfast", Category: "food", DurationMin: 60, Lat: 37.7956, Lng: -122.3933},
				{Title: "Cable car to Nob Hill", Category: "sight", DurationMin: 45, Lat: 37.7920, Lng: -122.4102},
				{Title: "Golden Gate Park + de Young", Category: "sight", DurationMin: 180, Lat: 37.7694, Lng: -122.4862},
				{Title: "Swan Oyster Depot lunch", Category: "food", DurationMin: 60, Lat: 37.7914, Lng: -122.4192},
				{Title: "Mission District taqueria crawl", Category: "food", DurationMin: 120, Lat: 37.7599, Lng: -122.4148},
				{Title: "Sunset at Lands End", Category: "viewpoint", DurationMin: 75, Lat: 37.7826, Lng: -122.5060},
			}},
		{Slug: "48-hours-amsterdam", Destination: "Amsterdam", Title: "48 hours in Amsterdam", DurationHours: 48, Featured: true, Tags: []string{"biking", "museums"},
			Stops: []models.CollectionStop{
				{Title: "Rijksmuseum", Category: "sight", DurationMin: 180, Lat: 52.3600, Lng: 4.8852},
				{Title: "Van Gogh Museum", Category: "sight", DurationMin: 120, Lat: 52.3584, Lng: 4.8811},
				{Title: "Bike the canals", Category: "walk", DurationMin: 120, Lat: 52.3676, Lng: 4.9041},
				{Title: "Foodhallen", Category: "food", DurationMin: 90, Lat: 52.3649, Lng: 4.8712},
				{Title: "Anne Frank House (book weeks ahead)", Category: "sight", DurationMin: 90, Lat: 52.3752, Lng: 4.8840},
			}},
		{Slug: "weekend-berlin", Destination: "Berlin", Title: "A weekend in Berlin", DurationHours: 48, Featured: true, Tags: []string{"history", "food"},
			Stops: []models.CollectionStop{
				{Title: "Brandenburg Gate + Reichstag", Category: "sight", DurationMin: 60, Lat: 52.5163, Lng: 13.3777},
				{Title: "Holocaust Memorial", Category: "sight", DurationMin: 45, Lat: 52.5140, Lng: 13.3784},
				{Title: "Markthalle Neun street food", Category: "food", DurationMin: 90, Lat: 52.5039, Lng: 13.4320},
				{Title: "Berlin Wall memorial", Category: "sight", DurationMin: 90, Lat: 52.5354, Lng: 13.3903},
				{Title: "Prenzlauer Berg dinner", Category: "food", DurationMin: 150, Lat: 52.5386, Lng: 13.4234},
			}},
		{Slug: "48-hours-prague", Destination: "Prague", Title: "48 hours in Prague", DurationHours: 48, Featured: true, Tags: []string{"architecture"},
			Stops: []models.CollectionStop{
				{Title: "Charles Bridge + Old Town", Category: "sight", DurationMin: 90, Lat: 50.0865, Lng: 14.4114},
				{Title: "Prague Castle", Category: "sight", DurationMin: 180, Lat: 50.0907, Lng: 14.4003},
				{Title: "Lunch at Lokál", Category: "food", DurationMin: 60, Lat: 50.0888, Lng: 14.4266},
				{Title: "Kampa Island walk", Category: "walk", DurationMin: 60, Lat: 50.0837, Lng: 14.4082},
				{Title: "Dinner in Vinohrady", Category: "food", DurationMin: 120, Lat: 50.0753, Lng: 14.4389},
			}},
		{Slug: "weekend-istanbul", Destination: "Istanbul", Title: "A weekend in Istanbul", DurationHours: 48, Featured: true, Tags: []string{"history", "food"},
			Stops: []models.CollectionStop{
				{Title: "Hagia Sophia", Category: "sight", DurationMin: 90, Lat: 41.0086, Lng: 28.9802},
				{Title: "Blue Mosque", Category: "sight", DurationMin: 45, Lat: 41.0054, Lng: 28.9768},
				{Title: "Grand Bazaar", Category: "sight", DurationMin: 120, Lat: 41.0106, Lng: 28.9681},
				{Title: "Bosphorus ferry to Kadıköy", Category: "sight", DurationMin: 75, Lat: 40.9923, Lng: 29.0244},
				{Title: "Kebap at Çiya Sofrası", Category: "food", DurationMin: 90, Lat: 40.9892, Lng: 29.0263},
			}},
		{Slug: "weekend-athens", Destination: "Athens", Title: "A weekend in Athens", DurationHours: 48, Featured: true, Tags: []string{"ruins", "food"},
			Stops: []models.CollectionStop{
				{Title: "Acropolis at opening", Category: "sight", DurationMin: 150, Lat: 37.9715, Lng: 23.7267},
				{Title: "Acropolis Museum", Category: "sight", DurationMin: 90, Lat: 37.9683, Lng: 23.7286},
				{Title: "Plaka wander + lunch", Category: "walk", DurationMin: 120, Lat: 37.9733, Lng: 23.7291},
				{Title: "Lycabettus sunset", Category: "viewpoint", DurationMin: 60, Lat: 37.9847, Lng: 23.7439},
			}},
		{Slug: "weekend-dublin", Destination: "Dublin", Title: "A weekend in Dublin", DurationHours: 48, Featured: true, Tags: []string{"pubs"},
			Stops: []models.CollectionStop{
				{Title: "Trinity College + Book of Kells", Category: "sight", DurationMin: 90, Lat: 53.3438, Lng: -6.2546},
				{Title: "Guinness Storehouse", Category: "sight", DurationMin: 120, Lat: 53.3419, Lng: -6.2867},
				{Title: "Lunch at Bunsen", Category: "food", DurationMin: 60, Lat: 53.3439, Lng: -6.2683},
				{Title: "Temple Bar music crawl", Category: "food", DurationMin: 180, Lat: 53.3455, Lng: -6.2636},
			}},
		{Slug: "weekend-copenhagen", Destination: "Copenhagen", Title: "A weekend in Copenhagen", DurationHours: 48, Featured: true, Tags: []string{"design", "food"},
			Stops: []models.CollectionStop{
				{Title: "Nyhavn + Little Mermaid", Category: "sight", DurationMin: 60, Lat: 55.6797, Lng: 12.5906},
				{Title: "Torvehallerne food hall", Category: "food", DurationMin: 75, Lat: 55.6836, Lng: 12.5691},
				{Title: "Design Museum Danmark", Category: "sight", DurationMin: 90, Lat: 55.6857, Lng: 12.5926},
				{Title: "Christiania walk", Category: "walk", DurationMin: 75, Lat: 55.6733, Lng: 12.5946},
			}},
		{Slug: "weekend-vienna", Destination: "Vienna", Title: "A weekend in Vienna", DurationHours: 48, Featured: true, Tags: []string{"classical"},
			Stops: []models.CollectionStop{
				{Title: "Stephansdom", Category: "sight", DurationMin: 45, Lat: 48.2089, Lng: 16.3733},
				{Title: "Belvedere (Klimt's Kiss)", Category: "sight", DurationMin: 120, Lat: 48.1913, Lng: 16.3808},
				{Title: "Café Central coffee + cake", Category: "food", DurationMin: 60, Lat: 48.2101, Lng: 16.3664},
				{Title: "Schönbrunn Palace", Category: "sight", DurationMin: 180, Lat: 48.1847, Lng: 16.3119},
			}},
		{Slug: "weekend-madrid", Destination: "Madrid", Title: "A weekend in Madrid", DurationHours: 48, Featured: true, Tags: []string{"art", "food"},
			Stops: []models.CollectionStop{
				{Title: "Prado Museum", Category: "sight", DurationMin: 180, Lat: 40.4138, Lng: -3.6921},
				{Title: "Retiro Park stroll", Category: "walk", DurationMin: 90, Lat: 40.4152, Lng: -3.6844},
				{Title: "Mercado San Miguel", Category: "food", DurationMin: 60, Lat: 40.4155, Lng: -3.7090},
				{Title: "Tapas crawl in La Latina", Category: "food", DurationMin: 180, Lat: 40.4116, Lng: -3.7095},
			}},
		{Slug: "72-hours-kyoto", Destination: "Kyoto", Title: "Three days in Kyoto", DurationHours: 72, Featured: true, Tags: []string{"temples", "gardens"},
			Stops: []models.CollectionStop{
				{Title: "Fushimi Inari at dawn", Category: "sight", DurationMin: 120, Lat: 34.9671, Lng: 135.7727},
				{Title: "Gion walking route", Category: "walk", DurationMin: 90, Lat: 35.0037, Lng: 135.7778},
				{Title: "Nishiki Market", Category: "food", DurationMin: 75, Lat: 35.0053, Lng: 135.7648},
				{Title: "Kinkaku-ji (Golden Pavilion)", Category: "sight", DurationMin: 60, Lat: 35.0394, Lng: 135.7292},
				{Title: "Arashiyama bamboo grove", Category: "sight", DurationMin: 120, Lat: 35.0170, Lng: 135.6717},
				{Title: "Ryoan-ji rock garden", Category: "sight", DurationMin: 45, Lat: 35.0344, Lng: 135.7183},
			}},
		{Slug: "48-hours-seoul", Destination: "Seoul", Title: "48 hours in Seoul", DurationHours: 48, Featured: true, Tags: []string{"food", "shopping"},
			Stops: []models.CollectionStop{
				{Title: "Gyeongbokgung Palace", Category: "sight", DurationMin: 90, Lat: 37.5796, Lng: 126.9770},
				{Title: "Bukchon Hanok Village", Category: "walk", DurationMin: 75, Lat: 37.5826, Lng: 126.9833},
				{Title: "Gwangjang street food", Category: "food", DurationMin: 90, Lat: 37.5703, Lng: 126.9999},
				{Title: "N Seoul Tower", Category: "viewpoint", DurationMin: 90, Lat: 37.5512, Lng: 126.9882},
				{Title: "Myeongdong night shopping", Category: "shop", DurationMin: 90, Lat: 37.5636, Lng: 126.9832},
			}},
		{Slug: "weekend-singapore", Destination: "Singapore", Title: "A weekend in Singapore", DurationHours: 48, Featured: true, Tags: []string{"food"},
			Stops: []models.CollectionStop{
				{Title: "Gardens by the Bay", Category: "sight", DurationMin: 150, Lat: 1.2816, Lng: 103.8636},
				{Title: "Maxwell Hawker Centre", Category: "food", DurationMin: 60, Lat: 1.2806, Lng: 103.8448},
				{Title: "Marina Bay Sands skypark", Category: "viewpoint", DurationMin: 60, Lat: 1.2834, Lng: 103.8607},
				{Title: "Little India + Tekka", Category: "food", DurationMin: 90, Lat: 1.3066, Lng: 103.8497},
			}},
		{Slug: "weekend-bangkok", Destination: "Bangkok", Title: "A weekend in Bangkok", DurationHours: 48, Featured: true, Tags: []string{"food", "temples"},
			Stops: []models.CollectionStop{
				{Title: "Grand Palace + Wat Phra Kaew", Category: "sight", DurationMin: 150, Lat: 13.7500, Lng: 100.4913},
				{Title: "Wat Pho reclining Buddha", Category: "sight", DurationMin: 60, Lat: 13.7465, Lng: 100.4930},
				{Title: "Boat noodles at Victory Monument", Category: "food", DurationMin: 45, Lat: 13.7649, Lng: 100.5370},
				{Title: "Chatuchak weekend market", Category: "shop", DurationMin: 180, Lat: 13.7999, Lng: 100.5503},
				{Title: "Rooftop sunset at Vertigo", Category: "viewpoint", DurationMin: 90, Lat: 13.7280, Lng: 100.5410},
			}},
		{Slug: "48-hours-mexico-city", Destination: "Mexico City", Title: "48 hours in Mexico City", DurationHours: 48, Featured: true, Tags: []string{"food", "history"},
			Stops: []models.CollectionStop{
				{Title: "Frida Kahlo Museum (book ahead)", Category: "sight", DurationMin: 120, Lat: 19.3551, Lng: -99.1624},
				{Title: "Zócalo + Catedral", Category: "sight", DurationMin: 60, Lat: 19.4326, Lng: -99.1332},
				{Title: "Teotihuacán pyramids (half day)", Category: "sight", DurationMin: 300, Lat: 19.6925, Lng: -98.8438},
				{Title: "Tacos al pastor at El Huequito", Category: "food", DurationMin: 45, Lat: 19.4298, Lng: -99.1519},
				{Title: "Roma/Condesa dinner", Category: "food", DurationMin: 150, Lat: 19.4130, Lng: -99.1672},
			}},
		{Slug: "weekend-rio", Destination: "Rio de Janeiro", Title: "A weekend in Rio", DurationHours: 48, Featured: true, Tags: []string{"beaches", "views"},
			Stops: []models.CollectionStop{
				{Title: "Christ the Redeemer at dawn", Category: "sight", DurationMin: 120, Lat: -22.9519, Lng: -43.2105},
				{Title: "Sugarloaf Mountain", Category: "viewpoint", DurationMin: 150, Lat: -22.9492, Lng: -43.1545},
				{Title: "Copacabana beach walk", Category: "walk", DurationMin: 90, Lat: -22.9711, Lng: -43.1822},
				{Title: "Churrascaria dinner in Leblon", Category: "food", DurationMin: 120, Lat: -22.9857, Lng: -43.2225},
				{Title: "Samba in Lapa", Category: "food", DurationMin: 180, Lat: -22.9139, Lng: -43.1798},
			}},
		{Slug: "weekend-cape-town", Destination: "Cape Town", Title: "A weekend in Cape Town", DurationHours: 48, Featured: true, Tags: []string{"nature"},
			Stops: []models.CollectionStop{
				{Title: "Table Mountain cable car", Category: "viewpoint", DurationMin: 180, Lat: -33.9628, Lng: 18.4098},
				{Title: "V&A Waterfront", Category: "walk", DurationMin: 90, Lat: -33.9033, Lng: 18.4197},
				{Title: "Cape Point drive", Category: "sight", DurationMin: 300, Lat: -34.3560, Lng: 18.4965},
				{Title: "Dinner in Bo-Kaap", Category: "food", DurationMin: 120, Lat: -33.9196, Lng: 18.4098},
			}},
		{Slug: "72-hours-iceland", Destination: "Reykjavík", Title: "Three days in Iceland", DurationHours: 72, Featured: true, Tags: []string{"nature", "hot-springs"},
			Stops: []models.CollectionStop{
				{Title: "Blue Lagoon on arrival", Category: "sight", DurationMin: 180, Lat: 63.8804, Lng: -22.4495},
				{Title: "Golden Circle loop", Category: "sight", DurationMin: 480, Lat: 64.3274, Lng: -20.1199},
				{Title: "Whale watching tour", Category: "sight", DurationMin: 180, Lat: 64.1559, Lng: -21.9383},
				{Title: "Dinner at Matur og Drykkur", Category: "food", DurationMin: 120, Lat: 64.1498, Lng: -21.9475},
				{Title: "Aurora chase (winter only)", Category: "sight", DurationMin: 240, Lat: 64.1466, Lng: -21.9426},
			}},
		{Slug: "weekend-marrakech", Destination: "Marrakech", Title: "A weekend in Marrakech", DurationHours: 48, Featured: true, Tags: []string{"souk", "riads"},
			Stops: []models.CollectionStop{
				{Title: "Jemaa el-Fnaa at dusk", Category: "sight", DurationMin: 90, Lat: 31.6258, Lng: -7.9891},
				{Title: "Medina souk walk", Category: "shop", DurationMin: 180, Lat: 31.6295, Lng: -7.9811},
				{Title: "Jardin Majorelle", Category: "sight", DurationMin: 90, Lat: 31.6417, Lng: -8.0033},
				{Title: "Bahia Palace", Category: "sight", DurationMin: 75, Lat: 31.6218, Lng: -7.9836},
				{Title: "Dinner at Nomad", Category: "food", DurationMin: 120, Lat: 31.6308, Lng: -7.9864},
			}},
	}
}
