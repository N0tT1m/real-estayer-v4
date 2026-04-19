package service

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"go.mongodb.org/mongo-driver/bson/primitive"

	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/provider/amadeus"
	"github.com/realestayer/v4/internal/provider/wikipedia"
	"github.com/realestayer/v4/internal/repository"
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

// isoCountryNames maps ISO 3166-1 alpha-2 codes to their English country names.
// Used during seeding so we don't rely on Amadeus returning the correct country name.
var isoCountryNames = map[string]string{
	"AE": "United Arab Emirates", "AR": "Argentina", "AT": "Austria",
	"AU": "Australia", "BE": "Belgium", "BH": "Bahrain",
	"BO": "Bolivia", "BR": "Brazil", "CA": "Canada",
	"CH": "Switzerland", "CL": "Chile", "CN": "China",
	"CO": "Colombia", "CR": "Costa Rica", "CU": "Cuba",
	"CZ": "Czech Republic", "DE": "Germany", "DK": "Denmark",
	"DO": "Dominican Republic", "EC": "Ecuador", "EG": "Egypt",
	"ES": "Spain", "FI": "Finland", "FJ": "Fiji",
	"FR": "France", "GB": "United Kingdom", "GH": "Ghana",
	"GR": "Greece", "HK": "Hong Kong", "HR": "Croatia",
	"HU": "Hungary", "ID": "Indonesia", "IL": "Israel",
	"IN": "India", "IS": "Iceland", "IT": "Italy",
	"JM": "Jamaica", "JO": "Jordan", "JP": "Japan",
	"KE": "Kenya", "KR": "South Korea", "KW": "Kuwait",
	"LB": "Lebanon", "LK": "Sri Lanka", "MA": "Morocco",
	"MM": "Myanmar", "MU": "Mauritius", "MV": "Maldives",
	"MX": "Mexico", "MY": "Malaysia", "NL": "Netherlands",
	"NO": "Norway", "NP": "Nepal", "NZ": "New Zealand",
	"OM": "Oman", "PA": "Panama", "PE": "Peru",
	"PF": "French Polynesia", "PG": "Papua New Guinea", "PH": "Philippines",
	"PL": "Poland", "PT": "Portugal", "PY": "Paraguay",
	"QA": "Qatar", "RO": "Romania", "SA": "Saudi Arabia",
	"SE": "Sweden", "SG": "Singapore", "SK": "Slovakia",
	"SN": "Senegal", "TH": "Thailand", "TN": "Tunisia",
	"TR": "Turkey", "TW": "Taiwan", "TZ": "Tanzania",
	"US": "United States", "UY": "Uruguay", "VN": "Vietnam",
	"ZA": "South Africa",
}

// seedImages provides a curated Unsplash fallback image for each seed city.
// Used when Wikipedia returns no thumbnail.
var seedImages = map[string]string{
	"Paris":            "https://images.unsplash.com/photo-1502602898657-3e91760cbb34?w=800&q=80&fit=crop",
	"London":           "https://images.unsplash.com/photo-1513635269975-59663e0ac1ad?w=800&q=80&fit=crop",
	"Rome":             "https://images.unsplash.com/photo-1552832230-c0197dd311b5?w=800&q=80&fit=crop",
	"Barcelona":        "https://images.unsplash.com/photo-1539037116277-4db20889f2d4?w=800&q=80&fit=crop",
	"Amsterdam":        "https://images.unsplash.com/photo-1512470876302-972faa2aa9a4?w=800&q=80&fit=crop",
	"Prague":           "https://images.unsplash.com/photo-1519677100203-a0e668c92439?w=800&q=80&fit=crop",
	"Lisbon":           "https://images.unsplash.com/photo-1558370781-d6196949e317?w=800&q=80&fit=crop",
	"Istanbul":         "https://images.unsplash.com/photo-1524231757912-21f4fe3a7200?w=800&q=80&fit=crop",
	"Vienna":           "https://images.unsplash.com/photo-1546877625-cb8c71916608?w=800&q=80&fit=crop",
	"Athens":           "https://images.unsplash.com/photo-1555993539-1732b0258235?w=800&q=80&fit=crop",
	"Berlin":           "https://images.unsplash.com/photo-1560969184-10fe8719e047?w=800&q=80&fit=crop",
	"Madrid":           "https://images.unsplash.com/photo-1543783207-ec64e4d95325?w=800&q=80&fit=crop",
	"Florence":         "https://images.unsplash.com/photo-1541370976299-4d24be67e9c5?w=800&q=80&fit=crop",
	"Dubrovnik":        "https://images.unsplash.com/photo-1555990538-1e8d0b5a9d7e?w=800&q=80&fit=crop",
	"Santorini":        "https://images.unsplash.com/photo-1570077188670-e3a8d69ac5ff?w=800&q=80&fit=crop",
	"Reykjavik":        "https://images.unsplash.com/photo-1504893524553-b855bce32c67?w=800&q=80&fit=crop",
	"Edinburgh":        "https://images.unsplash.com/photo-1506377247377-2a5b3b417ebb?w=800&q=80&fit=crop",
	"Budapest":         "https://images.unsplash.com/photo-1549060279-7e168fcee0c2?w=800&q=80&fit=crop",
	"Zurich":           "https://images.unsplash.com/photo-1515488764276-beab7607c1e6?w=800&q=80&fit=crop",
	"Copenhagen":       "https://images.unsplash.com/photo-1513622470522-26c3c8a854bc?w=800&q=80&fit=crop",
	"Stockholm":        "https://images.unsplash.com/photo-1509356843151-3e7d96241e11?w=800&q=80&fit=crop",
	"Porto":            "https://images.unsplash.com/photo-1555881400-74d7acaacd8b?w=800&q=80&fit=crop",
	"Bruges":           "https://images.unsplash.com/photo-1491557345352-5929e343eb89?w=800&q=80&fit=crop",
	"Mykonos":          "https://images.unsplash.com/photo-1601581875309-fafbf2d3ed3a?w=800&q=80&fit=crop",
	"Nice":             "https://images.unsplash.com/photo-1491166617655-0723a0489ae3?w=800&q=80&fit=crop",
	"Tokyo":            "https://images.unsplash.com/photo-1540959733332-eab4deabeeaf?w=800&q=80&fit=crop",
	"Bangkok":          "https://images.unsplash.com/photo-1508009603885-50cf7c579365?w=800&q=80&fit=crop",
	"Bali":             "https://images.unsplash.com/photo-1537996194471-e657df975ab4?w=800&q=80&fit=crop",
	"Singapore":        "https://images.unsplash.com/photo-1525625293386-3f8f99389edd?w=800&q=80&fit=crop",
	"Seoul":            "https://images.unsplash.com/photo-1517154421773-0529f29ea451?w=800&q=80&fit=crop",
	"Kuala Lumpur":     "https://images.unsplash.com/photo-1596422846543-75c6fc197f07?w=800&q=80&fit=crop",
	"Hong Kong":        "https://images.unsplash.com/photo-1559628233-100c798642d8?w=800&q=80&fit=crop",
	"Mumbai":           "https://images.unsplash.com/photo-1570168007204-dfb528c6958f?w=800&q=80&fit=crop",
	"Kyoto":            "https://images.unsplash.com/photo-1493976040374-85c8e12f0c0e?w=800&q=80&fit=crop",
	"Phuket":           "https://images.unsplash.com/photo-1589394815804-964ed0be2eb5?w=800&q=80&fit=crop",
	"Hanoi":            "https://images.unsplash.com/photo-1509030450996-dd1a26dda07a?w=800&q=80&fit=crop",
	"Ho Chi Minh City": "https://images.unsplash.com/photo-1583417319070-4a69db38a482?w=800&q=80&fit=crop",
	"Taipei":           "https://images.unsplash.com/photo-1508700929628-666bc8bd84ea?w=800&q=80&fit=crop",
	"Maldives":         "https://images.unsplash.com/photo-1514282401047-d79a71a590e8?w=800&q=80&fit=crop",
	"Colombo":          "https://images.unsplash.com/photo-1606293926075-69a00dbfde81?w=800&q=80&fit=crop",
	"Kathmandu":        "https://images.unsplash.com/photo-1558618666-fcd25c85cd64?w=800&q=80&fit=crop",
	"Chiang Mai":       "https://images.unsplash.com/photo-1528181304800-259b08848526?w=800&q=80&fit=crop",
	"Osaka":            "https://images.unsplash.com/photo-1590559899731-a382839e5549?w=800&q=80&fit=crop",
	"Delhi":            "https://images.unsplash.com/photo-1587474260584-136574528ed5?w=800&q=80&fit=crop",
	"Goa":              "https://images.unsplash.com/photo-1512343879784-a960bf40e7f2?w=800&q=80&fit=crop",
	"Yangon":           "https://images.unsplash.com/photo-1558008258-3256797b43f3?w=800&q=80&fit=crop",
	"New York City":    "https://images.unsplash.com/photo-1496442226666-8d4d0e62e6e9?w=800&q=80&fit=crop",
	"Los Angeles":      "https://images.unsplash.com/photo-1580655653885-65763b2597d0?w=800&q=80&fit=crop",
	"Miami":            "https://images.unsplash.com/photo-1506905925346-21bda4d32df4?w=800&q=80&fit=crop",
	"Chicago":          "https://images.unsplash.com/photo-1477959858617-67f85cf4f1df?w=800&q=80&fit=crop",
	"Toronto":          "https://images.unsplash.com/photo-1517090504586-fde19ea6066f?w=800&q=80&fit=crop",
	"Vancouver":        "https://images.unsplash.com/photo-1559511260-66a654ae982a?w=800&q=80&fit=crop",
	"Mexico City":      "https://images.unsplash.com/photo-1518105779142-d975f22f1b0a?w=800&q=80&fit=crop",
	"Las Vegas":        "https://images.unsplash.com/photo-1581351721010-8cf859cb14a4?w=800&q=80&fit=crop",
	"Cancun":           "https://images.unsplash.com/photo-1552074284-5e88ef1aef18?w=800&q=80&fit=crop",
	"New Orleans":      "https://images.unsplash.com/photo-1568695191071-a4dbb1b27a93?w=800&q=80&fit=crop",
	"San Francisco":    "https://images.unsplash.com/photo-1501594907352-04cda38ebc29?w=800&q=80&fit=crop",
	"Montreal":         "https://images.unsplash.com/photo-1553603227-2358aabe8e24?w=800&q=80&fit=crop",
	"Havana":           "https://images.unsplash.com/photo-1519754088135-3a56b7d2dce8?w=800&q=80&fit=crop",
	"San Jose":         "https://images.unsplash.com/photo-1602568567109-f6f461c93cf2?w=800&q=80&fit=crop",
	"Seattle":          "https://images.unsplash.com/photo-1502175353174-a7a70e73b362?w=800&q=80&fit=crop",
	"Boston":           "https://images.unsplash.com/photo-1501979376754-de51f69b41bf?w=800&q=80&fit=crop",
	"Rio de Janeiro":   "https://images.unsplash.com/photo-1483729558449-99ef09a8c325?w=800&q=80&fit=crop",
	"Buenos Aires":     "https://images.unsplash.com/photo-1589909202802-8f4aadce1849?w=800&q=80&fit=crop",
	"Bogota":           "https://images.unsplash.com/photo-1593550546961-c5cfdb0d4e71?w=800&q=80&fit=crop",
	"Lima":             "https://images.unsplash.com/photo-1531968455001-5c5272a41129?w=800&q=80&fit=crop",
	"Cartagena":        "https://images.unsplash.com/photo-1583285757773-b5d9e5d53d18?w=800&q=80&fit=crop",
	"Cusco":            "https://images.unsplash.com/photo-1567566621266-2c0b8dd4e3e4?w=800&q=80&fit=crop",
	"Santiago":         "https://images.unsplash.com/photo-1564419320461-6870880221ad?w=800&q=80&fit=crop",
	"Montevideo":       "https://images.unsplash.com/photo-1576177219-ea11fbf4ddba?w=800&q=80&fit=crop",
	"Quito":            "https://images.unsplash.com/photo-1579448587919-a7a71b2a4f36?w=800&q=80&fit=crop",
	"Medellin":         "https://images.unsplash.com/photo-1589909202802-8f4aadce1849?w=800&q=80&fit=crop",
	"Cape Town":        "https://images.unsplash.com/photo-1580060839134-75a5edca2e99?w=800&q=80&fit=crop",
	"Marrakech":        "https://images.unsplash.com/photo-1539020140153-e479b8c22e70?w=800&q=80&fit=crop",
	"Cairo":            "https://images.unsplash.com/photo-1568322445389-f64ac2515020?w=800&q=80&fit=crop",
	"Nairobi":          "https://images.unsplash.com/photo-1611348586804-61bf6c080437?w=800&q=80&fit=crop",
	"Zanzibar":         "https://images.unsplash.com/photo-1590523277543-a94d2e4eb00b?w=800&q=80&fit=crop",
	"Casablanca":       "https://images.unsplash.com/photo-1561328399-f94d2de78605?w=800&q=80&fit=crop",
	"Accra":            "https://images.unsplash.com/photo-1598387993441-a364f854cfds?w=800&q=80&fit=crop",
	"Tunis":            "https://images.unsplash.com/photo-1600093463592-8e36ae95ef56?w=800&q=80&fit=crop",
	"Johannesburg":     "https://images.unsplash.com/photo-1577948000111-9c970dfe3743?w=800&q=80&fit=crop",
	"Mauritius":        "https://images.unsplash.com/photo-1588867702719-969c8ac3e204?w=800&q=80&fit=crop",
	"Dubai":            "https://images.unsplash.com/photo-1512453979798-5ea266f8880c?w=800&q=80&fit=crop",
	"Abu Dhabi":        "https://images.unsplash.com/photo-1512632578888-169bbbc64f33?w=800&q=80&fit=crop",
	"Doha":             "https://images.unsplash.com/photo-1571893544028-06b07af6dade?w=800&q=80&fit=crop",
	"Petra":            "https://images.unsplash.com/photo-1563177978-4c5afe2d69cc?w=800&q=80&fit=crop",
	"Muscat":           "https://images.unsplash.com/photo-1586006739291-0f3f87c5ba96?w=800&q=80&fit=crop",
	"Amman":            "https://images.unsplash.com/photo-1580834341580-8c17a3a630ca?w=800&q=80&fit=crop",
	"Beirut":           "https://images.unsplash.com/photo-1597770012461-1c0879b1d3ac?w=800&q=80&fit=crop",
	"Tel Aviv":         "https://images.unsplash.com/photo-1544535830-9df3f56fff6a?w=800&q=80&fit=crop",
	"Sydney":           "https://images.unsplash.com/photo-1506973035872-a4ec16b8e8d9?w=800&q=80&fit=crop",
	"Melbourne":        "https://images.unsplash.com/photo-1546268060-2592ff93ee24?w=800&q=80&fit=crop",
	"Auckland":         "https://images.unsplash.com/photo-1507699622108-4be3abd695ad?w=800&q=80&fit=crop",
	"Brisbane":         "https://images.unsplash.com/photo-1529180979161-06b8b6d6f2be?w=800&q=80&fit=crop",
	"Queenstown":       "https://images.unsplash.com/photo-1507699622108-4be3abd695ad?w=800&q=80&fit=crop",
	"Fiji":             "https://images.unsplash.com/photo-1590523741831-ab7e8b8f9c7f?w=800&q=80&fit=crop",
	"Bora Bora":        "https://images.unsplash.com/photo-1559128010-7c1ad6e1b6a5?w=800&q=80&fit=crop",
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
	// Use seed data's country code if available so we don't get wrong cities
	// (e.g. Cairo, IL instead of Cairo, Egypt).
	countryCode, region := "", ""
	for _, sc := range seedCities {
		if strings.EqualFold(sc.Name, name) {
			countryCode = sc.CountryCode
			region = sc.Region
			break
		}
	}
	return s.fetchAndCache(ctx, name, countryCode, region)
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
		countryName := isoCountryNames[city.CountryCode]
		if countryName == "" {
			countryName = formatCountryName(amResult.CountryName)
		}

		description, imageURL := s.enrichFromWikipedia(ctx, city.Name)
		if imageURL == "" {
			imageURL = seedImages[city.Name]
		}

		budget := regionBudgets[city.Region]
		if budget == 0 {
			budget = 100
		}

		dest := &models.Destination{
			Name:            cityName,
			Country:         countryName,
			CountryCode:     city.CountryCode,
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
	// Prefer the caller-supplied country code over Amadeus result to avoid
	// mismatches (e.g. Cairo, IL instead of Cairo, Egypt).
	storedCode := amResult.CountryCode
	if countryCode != "" {
		storedCode = countryCode
	}
	countryName := isoCountryNames[storedCode]
	if countryName == "" {
		countryName = formatCountryName(amResult.CountryName)
	}
	description, imageURL := s.enrichFromWikipedia(ctx, name)
	if imageURL == "" {
		imageURL = seedImages[name]
	}

	budget := regionBudgets[region]
	if budget == 0 {
		budget = 100
	}

	dest := &models.Destination{
		Name:            cityName,
		Country:         countryName,
		CountryCode:     storedCode,
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

	if err := s.repo.Upsert(ctx, dest); err != nil {
		slog.Warn("destination: upsert fallback failed", "name", dest.Name, "error", err)
	}
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
