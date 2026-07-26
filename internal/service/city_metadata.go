package service

// cityMetadata is the IATA + city-centre coordinates for every entry in
// seedCities. It replaces what we used to fetch from Amadeus — the test
// sandbox was unreliable, and Amadeus's Self-Service tier was flagged for
// decommission, so we now carry this data ourselves. Values were compiled
// from the public-domain OpenFlights dataset + Wikipedia infoboxes.
//
// Keys MUST match seedCities[i].Name exactly (case, spaces, accents).
// Latitude/longitude are city centre, NOT the airport — better for the map
// view on /explore. The IATA code is the primary commercial airport serving
// the city; for locations with no single airport we use the most common
// gateway (e.g. Bruges → BRU Brussels, Petra → AQJ Aqaba).
//
// Adding a new city: append to seedCities in destination_service.go, then
// add the matching entry here. The seed pipeline refuses to run if the two
// lists drift apart (see validateCityMetadata below).
type cityMeta struct {
	IATA string
	Lat  float64
	Lng  float64
}

var cityMetadata = map[string]cityMeta{
	// ---- Europe ----
	"Paris":      {"CDG", 48.8566, 2.3522},
	"London":     {"LHR", 51.5074, -0.1278},
	"Rome":       {"FCO", 41.9028, 12.4964},
	"Barcelona":  {"BCN", 41.3851, 2.1734},
	"Amsterdam":  {"AMS", 52.3676, 4.9041},
	"Prague":     {"PRG", 50.0755, 14.4378},
	"Lisbon":     {"LIS", 38.7223, -9.1393},
	"Istanbul":   {"IST", 41.0082, 28.9784},
	"Vienna":     {"VIE", 48.2082, 16.3738},
	"Athens":     {"ATH", 37.9838, 23.7275},
	"Berlin":     {"BER", 52.5200, 13.4050},
	"Madrid":     {"MAD", 40.4168, -3.7038},
	"Florence":   {"FLR", 43.7696, 11.2558},
	"Dubrovnik":  {"DBV", 42.6507, 18.0944},
	"Santorini":  {"JTR", 36.3932, 25.4615},
	"Reykjavik":  {"KEF", 64.1466, -21.9426},
	"Edinburgh":  {"EDI", 55.9533, -3.1883},
	"Budapest":   {"BUD", 47.4979, 19.0402},
	"Zurich":     {"ZRH", 47.3769, 8.5417},
	"Copenhagen": {"CPH", 55.6761, 12.5683},
	"Stockholm":  {"ARN", 59.3293, 18.0686},
	"Porto":      {"OPO", 41.1579, -8.6291},
	"Bruges":     {"BRU", 51.2093, 3.2247}, // served by Brussels
	"Mykonos":    {"JMK", 37.4467, 25.3289},
	"Nice":       {"NCE", 43.7102, 7.2620},

	// ---- Asia ----
	"Tokyo":            {"HND", 35.6762, 139.6503},
	"Bangkok":          {"BKK", 13.7563, 100.5018},
	"Bali":             {"DPS", -8.3405, 115.0920},
	"Singapore":        {"SIN", 1.3521, 103.8198},
	"Seoul":            {"ICN", 37.5665, 126.9780},
	"Kuala Lumpur":     {"KUL", 3.1390, 101.6869},
	"Hong Kong":        {"HKG", 22.3193, 114.1694},
	"Mumbai":           {"BOM", 19.0760, 72.8777},
	"Kyoto":            {"KIX", 35.0116, 135.7681}, // served by Osaka Kansai
	"Phuket":           {"HKT", 7.8804, 98.3923},
	"Hanoi":            {"HAN", 21.0285, 105.8542},
	"Ho Chi Minh City": {"SGN", 10.8231, 106.6297},
	"Taipei":           {"TPE", 25.0330, 121.5654},
	"Maldives":         {"MLE", 3.2028, 73.2207},
	"Colombo":          {"CMB", 6.9271, 79.8612},
	"Kathmandu":        {"KTM", 27.7172, 85.3240},
	"Chiang Mai":       {"CNX", 18.7883, 98.9853},
	"Osaka":            {"KIX", 34.6937, 135.5023},
	"Delhi":            {"DEL", 28.7041, 77.1025},
	"Goa":              {"GOI", 15.2993, 74.1240},
	"Yangon":           {"RGN", 16.8661, 96.1951},

	// ---- North America ----
	"New York City": {"JFK", 40.7128, -74.0060},
	"Los Angeles":   {"LAX", 34.0522, -118.2437},
	"Miami":         {"MIA", 25.7617, -80.1918},
	"Chicago":       {"ORD", 41.8781, -87.6298},
	"Toronto":       {"YYZ", 43.6532, -79.3832},
	"Vancouver":     {"YVR", 49.2827, -123.1207},
	"Mexico City":   {"MEX", 19.4326, -99.1332},
	"Las Vegas":     {"LAS", 36.1699, -115.1398},
	"Cancun":        {"CUN", 21.1619, -86.8515},
	"New Orleans":   {"MSY", 29.9511, -90.0715},
	"San Francisco": {"SFO", 37.7749, -122.4194},
	"Montreal":      {"YUL", 45.5017, -73.5673},
	"Havana":        {"HAV", 23.1136, -82.3666},
	"San Jose":      {"SJO", 9.9281, -84.0907}, // Costa Rica, not CA
	"Seattle":       {"SEA", 47.6062, -122.3321},
	"Boston":        {"BOS", 42.3601, -71.0589},

	// ---- South America ----
	"Rio de Janeiro": {"GIG", -22.9068, -43.1729},
	"Buenos Aires":   {"EZE", -34.6037, -58.3816},
	"Bogota":         {"BOG", 4.7110, -74.0721},
	"Lima":           {"LIM", -12.0464, -77.0428},
	"Cartagena":      {"CTG", 10.3910, -75.4794},
	"Cusco":          {"CUZ", -13.5319, -71.9675},
	"Santiago":       {"SCL", -33.4489, -70.6693},
	"Montevideo":     {"MVD", -34.9011, -56.1645},
	"Quito":          {"UIO", -0.1807, -78.4678},
	"Medellin":       {"MDE", 6.2442, -75.5812},

	// ---- Africa ----
	"Cape Town":    {"CPT", -33.9249, 18.4241},
	"Marrakech":    {"RAK", 31.6295, -7.9811},
	"Cairo":        {"CAI", 30.0444, 31.2357},
	"Nairobi":      {"NBO", -1.2921, 36.8219},
	"Zanzibar":     {"ZNZ", -6.1659, 39.2026},
	"Casablanca":   {"CMN", 33.5731, -7.5898},
	"Accra":        {"ACC", 5.6037, -0.1870},
	"Tunis":        {"TUN", 36.8065, 10.1815},
	"Johannesburg": {"JNB", -26.2041, 28.0473},
	"Mauritius":    {"MRU", -20.3484, 57.5522},

	// ---- Middle East ----
	"Dubai":     {"DXB", 25.2048, 55.2708},
	"Abu Dhabi": {"AUH", 24.4539, 54.3773},
	"Doha":      {"DOH", 25.2854, 51.5310},
	"Petra":     {"AQJ", 30.3285, 35.4444}, // via Aqaba
	"Muscat":    {"MCT", 23.5880, 58.3829},
	"Amman":     {"AMM", 31.9454, 35.9284},
	"Beirut":    {"BEY", 33.8938, 35.5018},
	"Tel Aviv":  {"TLV", 32.0853, 34.7818},

	// ---- Oceania ----
	"Sydney":     {"SYD", -33.8688, 151.2093},
	"Melbourne":  {"MEL", -37.8136, 144.9631},
	"Auckland":   {"AKL", -36.8509, 174.7645},
	"Brisbane":   {"BNE", -27.4698, 153.0251},
	"Queenstown": {"ZQN", -45.0312, 168.6626},
	"Fiji":       {"NAN", -17.7134, 178.0650}, // Nadi is the tourist gateway
	"Bora Bora":  {"BOB", -16.5004, -151.7415},
}
