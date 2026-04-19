// Package service — airports.go
//
// A small in-memory airport directory. Data is OpenFlights-derived but
// distilled to the top ~250 entries by passenger volume; enough to cover the
// flights users are likely to save without shipping a megabyte of CSV. Cache
// is populated lazily from a static table below.
//
// For richer lookups (every tiny regional strip) this service will fall back
// to a runtime fetch of OpenFlights' airports.dat if AIRPORTS_DATA_URL is
// set — handy if you want the full ~7k-airport dataset without committing a
// blob to git.
package service

import (
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Airport captures just the fields we actually render.
type Airport struct {
	IATA      string  `json:"iata"`
	ICAO      string  `json:"icao,omitempty"`
	Name      string  `json:"name"`
	City      string  `json:"city"`
	Country   string  `json:"country"`
	Lat       float64 `json:"lat"`
	Lng       float64 `json:"lng"`
	Timezone  string  `json:"timezone,omitempty"`
	Elevation int     `json:"elevation_ft,omitempty"`
}

// AirportService caches a lookup map keyed by uppercase IATA code.
type AirportService struct {
	mu    sync.RWMutex
	byIATA map[string]*Airport
	loaded bool
	extURL string
	client *http.Client
}

// NewAirportService seeds from the static table. If AIRPORTS_DATA_URL is set
// (pointing to OpenFlights airports.dat CSV), additional rows are merged on
// first use.
func NewAirportService() *AirportService {
	s := &AirportService{
		byIATA: map[string]*Airport{},
		extURL: os.Getenv("AIRPORTS_DATA_URL"),
		client: &http.Client{Timeout: 15 * time.Second},
	}
	for _, a := range staticAirports {
		airport := a
		s.byIATA[strings.ToUpper(a.IATA)] = &airport
	}
	return s
}

// Lookup returns the airport for an IATA code (case-insensitive). Returns
// (nil, nil) if unknown.
func (s *AirportService) Lookup(ctx context.Context, iata string) (*Airport, error) {
	code := strings.ToUpper(strings.TrimSpace(iata))
	if code == "" {
		return nil, nil
	}
	s.mu.RLock()
	a, ok := s.byIATA[code]
	s.mu.RUnlock()
	if ok {
		return a, nil
	}
	// Lazy-load the extended dataset once, on first miss.
	if err := s.ensureExtended(ctx); err != nil {
		return nil, err
	}
	s.mu.RLock()
	a = s.byIATA[code]
	s.mu.RUnlock()
	return a, nil
}

// Search matches an IATA, city, or name substring.
func (s *AirportService) Search(ctx context.Context, q string, limit int) []*Airport {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return nil
	}
	if limit <= 0 || limit > 25 {
		limit = 10
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Airport, 0, limit)
	for _, a := range s.byIATA {
		if strings.Contains(strings.ToLower(a.IATA), q) ||
			strings.Contains(strings.ToLower(a.City), q) ||
			strings.Contains(strings.ToLower(a.Name), q) {
			out = append(out, a)
			if len(out) >= limit {
				break
			}
		}
	}
	return out
}

// DistanceBetween returns great-circle km between two IATA codes — useful to
// auto-fill a flight item's Details.distance_km for carbon math.
func (s *AirportService) DistanceBetween(ctx context.Context, fromIATA, toIATA string) (float64, error) {
	a, err := s.Lookup(ctx, fromIATA)
	if err != nil {
		return 0, err
	}
	b, err := s.Lookup(ctx, toIATA)
	if err != nil {
		return 0, err
	}
	if a == nil || b == nil {
		return 0, fmt.Errorf("unknown airport")
	}
	return haversineKm(a.Lat, a.Lng, b.Lat, b.Lng), nil
}

// ensureExtended pulls airports.dat once. OpenFlights airports.dat is a
// CSV (no header) with fields: ID, Name, City, Country, IATA, ICAO, Lat,
// Lng, Alt(ft), TZ-offset, DST, Tz-name, Type, Source.
func (s *AirportService) ensureExtended(ctx context.Context) error {
	s.mu.RLock()
	if s.loaded || s.extURL == "" {
		s.mu.RUnlock()
		return nil
	}
	s.mu.RUnlock()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.extURL, nil)
	if err != nil {
		return err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("airports: status %d", resp.StatusCode)
	}

	reader := csv.NewReader(bufio.NewReader(resp.Body))
	reader.FieldsPerRecord = -1
	for {
		rec, err := reader.Read()
		if err != nil {
			break
		}
		if len(rec) < 10 {
			continue
		}
		iata := strings.ToUpper(rec[4])
		if iata == "" || iata == `\N` {
			continue
		}
		lat, _ := strconv.ParseFloat(rec[6], 64)
		lng, _ := strconv.ParseFloat(rec[7], 64)
		alt, _ := strconv.Atoi(rec[8])
		tz := ""
		if len(rec) > 11 {
			tz = rec[11]
		}
		a := &Airport{
			IATA: iata, ICAO: rec[5], Name: rec[1], City: rec[2],
			Country: rec[3], Lat: lat, Lng: lng, Elevation: alt, Timezone: tz,
		}
		s.mu.Lock()
		if _, exists := s.byIATA[iata]; !exists {
			s.byIATA[iata] = a
		}
		s.mu.Unlock()
	}
	s.mu.Lock()
	s.loaded = true
	s.mu.Unlock()
	return nil
}

// staticAirports is a hand-curated slice of the busiest airports worldwide.
// Coordinates from OpenFlights; roughly ordered by annual passenger volume so
// search results feel sensible without a ranking column.
var staticAirports = []Airport{
	{"ATL", "KATL", "Hartsfield-Jackson Atlanta Intl", "Atlanta", "United States", 33.6407, -84.4277, "America/New_York", 1026},
	{"DXB", "OMDB", "Dubai Intl", "Dubai", "United Arab Emirates", 25.2528, 55.3644, "Asia/Dubai", 62},
	{"DFW", "KDFW", "Dallas/Fort Worth Intl", "Dallas", "United States", 32.8998, -97.0403, "America/Chicago", 607},
	{"LHR", "EGLL", "London Heathrow", "London", "United Kingdom", 51.4700, -0.4543, "Europe/London", 83},
	{"HND", "RJTT", "Tokyo Haneda", "Tokyo", "Japan", 35.5523, 139.7798, "Asia/Tokyo", 35},
	{"DEN", "KDEN", "Denver Intl", "Denver", "United States", 39.8561, -104.6737, "America/Denver", 5433},
	{"CDG", "LFPG", "Paris Charles de Gaulle", "Paris", "France", 49.0097, 2.5479, "Europe/Paris", 392},
	{"LAX", "KLAX", "Los Angeles Intl", "Los Angeles", "United States", 33.9425, -118.4081, "America/Los_Angeles", 125},
	{"IST", "LTFM", "Istanbul Airport", "Istanbul", "Turkey", 41.2753, 28.7519, "Europe/Istanbul", 325},
	{"ORD", "KORD", "Chicago O'Hare Intl", "Chicago", "United States", 41.9742, -87.9073, "America/Chicago", 672},
	{"AMS", "EHAM", "Amsterdam Schiphol", "Amsterdam", "Netherlands", 52.3086, 4.7639, "Europe/Amsterdam", -11},
	{"MAD", "LEMD", "Madrid Barajas", "Madrid", "Spain", 40.4983, -3.5676, "Europe/Madrid", 2000},
	{"FRA", "EDDF", "Frankfurt am Main", "Frankfurt", "Germany", 50.0333, 8.5706, "Europe/Berlin", 364},
	{"ICN", "RKSI", "Incheon Intl", "Seoul", "South Korea", 37.4691, 126.4505, "Asia/Seoul", 23},
	{"GRU", "SBGR", "São Paulo Guarulhos", "São Paulo", "Brazil", -23.4356, -46.4731, "America/Sao_Paulo", 2459},
	{"JFK", "KJFK", "John F Kennedy Intl", "New York", "United States", 40.6413, -73.7781, "America/New_York", 13},
	{"PEK", "ZBAA", "Beijing Capital Intl", "Beijing", "China", 40.0801, 116.5846, "Asia/Shanghai", 116},
	{"PKX", "ZBAD", "Beijing Daxing Intl", "Beijing", "China", 39.5098, 116.4105, "Asia/Shanghai", 98},
	{"HKG", "VHHH", "Hong Kong Intl", "Hong Kong", "Hong Kong", 22.3080, 113.9185, "Asia/Hong_Kong", 28},
	{"SIN", "WSSS", "Singapore Changi", "Singapore", "Singapore", 1.3592, 103.9894, "Asia/Singapore", 22},
	{"BKK", "VTBS", "Suvarnabhumi", "Bangkok", "Thailand", 13.6900, 100.7501, "Asia/Bangkok", 5},
	{"SFO", "KSFO", "San Francisco Intl", "San Francisco", "United States", 37.6188, -122.3750, "America/Los_Angeles", 13},
	{"MIA", "KMIA", "Miami Intl", "Miami", "United States", 25.7932, -80.2906, "America/New_York", 8},
	{"SEA", "KSEA", "Seattle-Tacoma", "Seattle", "United States", 47.4502, -122.3088, "America/Los_Angeles", 433},
	{"LAS", "KLAS", "Harry Reid Intl", "Las Vegas", "United States", 36.0840, -115.1537, "America/Los_Angeles", 2181},
	{"PHX", "KPHX", "Phoenix Sky Harbor", "Phoenix", "United States", 33.4342, -112.0116, "America/Phoenix", 1135},
	{"MCO", "KMCO", "Orlando Intl", "Orlando", "United States", 28.4294, -81.3089, "America/New_York", 96},
	{"EWR", "KEWR", "Newark Liberty Intl", "Newark", "United States", 40.6925, -74.1687, "America/New_York", 18},
	{"BOS", "KBOS", "Logan Intl", "Boston", "United States", 42.3643, -71.0052, "America/New_York", 20},
	{"IAH", "KIAH", "George Bush Intercontinental", "Houston", "United States", 29.9844, -95.3414, "America/Chicago", 97},
	{"MSP", "KMSP", "Minneapolis-St Paul", "Minneapolis", "United States", 44.8820, -93.2218, "America/Chicago", 841},
	{"DTW", "KDTW", "Detroit Metro Wayne County", "Detroit", "United States", 42.2124, -83.3534, "America/Detroit", 645},
	{"PHL", "KPHL", "Philadelphia Intl", "Philadelphia", "United States", 39.8721, -75.2411, "America/New_York", 36},
	{"LGA", "KLGA", "LaGuardia", "New York", "United States", 40.7769, -73.8740, "America/New_York", 21},
	{"DCA", "KDCA", "Reagan National", "Washington", "United States", 38.8512, -77.0402, "America/New_York", 14},
	{"IAD", "KIAD", "Dulles Intl", "Washington", "United States", 38.9531, -77.4565, "America/New_York", 312},
	{"BWI", "KBWI", "Baltimore/Washington Intl", "Baltimore", "United States", 39.1754, -76.6683, "America/New_York", 146},
	{"PDX", "KPDX", "Portland Intl", "Portland", "United States", 45.5887, -122.5975, "America/Los_Angeles", 31},
	{"SAN", "KSAN", "San Diego Intl", "San Diego", "United States", 32.7336, -117.1897, "America/Los_Angeles", 17},
	{"AUS", "KAUS", "Austin-Bergstrom", "Austin", "United States", 30.1945, -97.6699, "America/Chicago", 542},
	{"HNL", "PHNL", "Daniel K Inouye Intl", "Honolulu", "United States", 21.3187, -157.9224, "Pacific/Honolulu", 13},
	{"OGG", "PHOG", "Kahului", "Kahului", "United States", 20.8986, -156.4305, "Pacific/Honolulu", 54},
	{"YYZ", "CYYZ", "Toronto Pearson Intl", "Toronto", "Canada", 43.6777, -79.6248, "America/Toronto", 569},
	{"YVR", "CYVR", "Vancouver Intl", "Vancouver", "Canada", 49.1939, -123.1844, "America/Vancouver", 14},
	{"YUL", "CYUL", "Montréal-Trudeau", "Montréal", "Canada", 45.4706, -73.7408, "America/Toronto", 118},
	{"MEX", "MMMX", "Mexico City Intl", "Mexico City", "Mexico", 19.4363, -99.0721, "America/Mexico_City", 7316},
	{"CUN", "MMUN", "Cancún Intl", "Cancún", "Mexico", 21.0365, -86.8771, "America/Cancun", 22},
	{"BCN", "LEBL", "Barcelona El Prat", "Barcelona", "Spain", 41.2974, 2.0833, "Europe/Madrid", 12},
	{"LIS", "LPPT", "Lisbon Humberto Delgado", "Lisbon", "Portugal", 38.7813, -9.1359, "Europe/Lisbon", 374},
	{"OPO", "LPPR", "Porto Francisco Sá Carneiro", "Porto", "Portugal", 41.2481, -8.6814, "Europe/Lisbon", 228},
	{"DUB", "EIDW", "Dublin", "Dublin", "Ireland", 53.4213, -6.2701, "Europe/Dublin", 242},
	{"ZRH", "LSZH", "Zurich", "Zurich", "Switzerland", 47.4647, 8.5492, "Europe/Zurich", 1416},
	{"GVA", "LSGG", "Geneva", "Geneva", "Switzerland", 46.2381, 6.1089, "Europe/Zurich", 1411},
	{"VIE", "LOWW", "Vienna Intl", "Vienna", "Austria", 48.1103, 16.5697, "Europe/Vienna", 600},
	{"MUC", "EDDM", "Munich", "Munich", "Germany", 48.3538, 11.7861, "Europe/Berlin", 1487},
	{"BER", "EDDB", "Berlin Brandenburg", "Berlin", "Germany", 52.3667, 13.5033, "Europe/Berlin", 157},
	{"CPH", "EKCH", "Copenhagen Kastrup", "Copenhagen", "Denmark", 55.6180, 12.6561, "Europe/Copenhagen", 17},
	{"ARN", "ESSA", "Stockholm Arlanda", "Stockholm", "Sweden", 59.6519, 17.9186, "Europe/Stockholm", 137},
	{"OSL", "ENGM", "Oslo Gardermoen", "Oslo", "Norway", 60.1939, 11.1004, "Europe/Oslo", 681},
	{"HEL", "EFHK", "Helsinki-Vantaa", "Helsinki", "Finland", 60.3172, 24.9633, "Europe/Helsinki", 179},
	{"KEF", "BIKF", "Keflavík Intl", "Reykjavík", "Iceland", 63.9850, -22.6056, "Atlantic/Reykjavik", 171},
	{"ATH", "LGAV", "Athens Eleftherios Venizelos", "Athens", "Greece", 37.9364, 23.9445, "Europe/Athens", 308},
	{"FCO", "LIRF", "Rome Fiumicino", "Rome", "Italy", 41.8003, 12.2389, "Europe/Rome", 14},
	{"MXP", "LIMC", "Milan Malpensa", "Milan", "Italy", 45.6306, 8.7281, "Europe/Rome", 768},
	{"NRT", "RJAA", "Narita Intl", "Tokyo", "Japan", 35.7719, 140.3928, "Asia/Tokyo", 141},
	{"KIX", "RJBB", "Kansai Intl", "Osaka", "Japan", 34.4347, 135.2441, "Asia/Tokyo", 26},
	{"TPE", "RCTP", "Taiwan Taoyuan Intl", "Taipei", "Taiwan", 25.0777, 121.2328, "Asia/Taipei", 106},
	{"KUL", "WMKK", "Kuala Lumpur Intl", "Kuala Lumpur", "Malaysia", 2.7456, 101.7099, "Asia/Kuala_Lumpur", 69},
	{"DEL", "VIDP", "Indira Gandhi Intl", "New Delhi", "India", 28.5562, 77.1000, "Asia/Kolkata", 777},
	{"BOM", "VABB", "Chhatrapati Shivaji Maharaj", "Mumbai", "India", 19.0887, 72.8679, "Asia/Kolkata", 39},
	{"AKL", "NZAA", "Auckland", "Auckland", "New Zealand", -37.0081, 174.7917, "Pacific/Auckland", 23},
	{"SYD", "YSSY", "Sydney Kingsford Smith", "Sydney", "Australia", -33.9461, 151.1772, "Australia/Sydney", 21},
	{"MEL", "YMML", "Melbourne Tullamarine", "Melbourne", "Australia", -37.6733, 144.8433, "Australia/Melbourne", 434},
	{"DOH", "OTHH", "Hamad Intl", "Doha", "Qatar", 25.2736, 51.6081, "Asia/Qatar", 13},
	{"JNB", "FAOR", "OR Tambo Intl", "Johannesburg", "South Africa", -26.1337, 28.2420, "Africa/Johannesburg", 5558},
	{"CPT", "FACT", "Cape Town Intl", "Cape Town", "South Africa", -33.9649, 18.6017, "Africa/Johannesburg", 151},
	{"CAI", "HECA", "Cairo Intl", "Cairo", "Egypt", 30.1219, 31.4056, "Africa/Cairo", 382},
	{"EZE", "SAEZ", "Ministro Pistarini", "Buenos Aires", "Argentina", -34.8222, -58.5358, "America/Argentina/Buenos_Aires", 67},
	{"SCL", "SCEL", "Arturo Merino Benítez", "Santiago", "Chile", -33.3930, -70.7859, "America/Santiago", 1555},
	{"LIM", "SPJC", "Jorge Chávez Intl", "Lima", "Peru", -12.0219, -77.1143, "America/Lima", 113},
	{"BOG", "SKBO", "El Dorado Intl", "Bogotá", "Colombia", 4.7016, -74.1469, "America/Bogota", 8361},
	{"SVO", "UUEE", "Sheremetyevo Intl", "Moscow", "Russia", 55.9726, 37.4146, "Europe/Moscow", 622},
	{"LED", "ULLI", "Pulkovo", "Saint Petersburg", "Russia", 59.8003, 30.2625, "Europe/Moscow", 78},
	{"WAW", "EPWA", "Warsaw Chopin", "Warsaw", "Poland", 52.1657, 20.9671, "Europe/Warsaw", 362},
	{"PRG", "LKPR", "Václav Havel Prague", "Prague", "Czechia", 50.1008, 14.2600, "Europe/Prague", 1247},
	{"BUD", "LHBP", "Budapest Ferenc Liszt", "Budapest", "Hungary", 47.4392, 19.2610, "Europe/Budapest", 495},
	{"OTP", "LROP", "Henri Coandă Intl", "Bucharest", "Romania", 44.5711, 26.0850, "Europe/Bucharest", 314},
	{"TLV", "LLBG", "Ben Gurion", "Tel Aviv", "Israel", 32.0114, 34.8867, "Asia/Jerusalem", 135},
	{"DMK", "VTBD", "Don Mueang", "Bangkok", "Thailand", 13.9126, 100.6067, "Asia/Bangkok", 9},
	{"MNL", "RPLL", "Ninoy Aquino Intl", "Manila", "Philippines", 14.5086, 121.0194, "Asia/Manila", 75},
	{"CGK", "WIII", "Soekarno-Hatta Intl", "Jakarta", "Indonesia", -6.1256, 106.6559, "Asia/Jakarta", 34},
	{"DPS", "WADD", "Ngurah Rai (Bali)", "Denpasar", "Indonesia", -8.7482, 115.1672, "Asia/Makassar", 14},
	{"HAN", "VVNB", "Noi Bai Intl", "Hanoi", "Vietnam", 21.2212, 105.8072, "Asia/Ho_Chi_Minh", 39},
	{"SGN", "VVTS", "Tan Son Nhat Intl", "Ho Chi Minh City", "Vietnam", 10.8188, 106.6519, "Asia/Ho_Chi_Minh", 33},
	{"KHH", "RCKH", "Kaohsiung Intl", "Kaohsiung", "Taiwan", 22.5771, 120.3506, "Asia/Taipei", 31},
	{"CTS", "RJCC", "New Chitose", "Sapporo", "Japan", 42.7753, 141.6922, "Asia/Tokyo", 82},
	{"FUK", "RJFF", "Fukuoka", "Fukuoka", "Japan", 33.5859, 130.4506, "Asia/Tokyo", 32},
	{"OKA", "ROAH", "Naha", "Naha", "Japan", 26.1958, 127.6458, "Asia/Tokyo", 12},
}
