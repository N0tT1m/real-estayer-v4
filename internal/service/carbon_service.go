package service

import (
	"math"

	"github.com/realestayer/v4/internal/models"
)

// CarbonService estimates CO2e for a trip. Formulas are deliberately simple
// and public-domain:
//
//   Flights: distance-based tiers approximating the UK DEFRA 2023 figures
//     short-haul (<1,500 km):   0.255 kg CO2e / passenger-km
//     long-haul  (>=1,500 km):  0.195 kg CO2e / passenger-km
//     No radiative-forcing multiplier applied — most offset calculators
//     quote the plain number; we do the same for transparency.
//
//   Cars:    0.171 kg CO2e / vehicle-km (EU average 2023)
//            We split across passengers when size is known.
//
//   Stays:   12 kg CO2e / room-night (global hotel average, Cornell HCMI)
//            short-term rentals tend lower — we use 8 kg/night for
//            TripItemTypeListing.
//
// The service is intentionally stateless. A future upgrade could pull per-
// airline emissions from the Travel-Tech IATA API.
type CarbonService struct{}

func NewCarbonService() *CarbonService { return &CarbonService{} }

// Estimate is a single line item in the rollup.
type Estimate struct {
	ItemID     string  `json:"item_id,omitempty"`
	Type       string  `json:"type"`
	Title      string  `json:"title"`
	Kilometres float64 `json:"kilometres,omitempty"`
	Nights     int     `json:"nights,omitempty"`
	KgCO2e     float64 `json:"kg_co2e"`
}

// TripEstimate is the full rollup.
type TripEstimate struct {
	Items   []Estimate `json:"items"`
	TotalKg float64    `json:"total_kg"`
	Equivalents Equivalents `json:"equivalents"`
}

// Equivalents expresses the total in relatable units so the UI can render
// something more evocative than "432 kg CO2e".
type Equivalents struct {
	KmInAverageCar int `json:"km_in_average_car"`
	MonthsAvgPerson int `json:"months_avg_person"` // global average 4 t CO2/yr/person
	TreesForOneYear int `json:"trees_for_one_year"` // one tree absorbs ~22 kg/yr
}

// EstimateTrip walks trip items and aggregates emissions. Flight kilometres
// are pulled from either Details["distance_km"] (preferred) or computed from
// Details["from_lat"/"from_lng"/"to_lat"/"to_lng"]. Car distance reads
// Details["distance_km"]; stays read Trip start/end for nights.
func (s *CarbonService) EstimateTrip(trip *models.Trip) *TripEstimate {
	if trip == nil {
		return &TripEstimate{}
	}
	out := &TripEstimate{}
	nights := 0
	if !trip.EndDate.IsZero() && !trip.StartDate.IsZero() && trip.EndDate.After(trip.StartDate) {
		nights = int(trip.EndDate.Sub(trip.StartDate).Hours()/24) + 1
	}
	if nights < 1 {
		nights = 1
	}

	for _, it := range trip.Items {
		est := Estimate{ItemID: it.ID.Hex(), Type: string(it.Type), Title: it.Title}
		switch it.Type {
		case models.TripItemTypeFlight:
			km := detailFloat(it.Details, "distance_km")
			if km == 0 {
				km = distanceFromDetails(it.Details)
			}
			if km > 0 {
				est.Kilometres = km
				est.KgCO2e = flightEmissionsKg(km)
			}
		case models.TripItemTypeCar:
			km := detailFloat(it.Details, "distance_km")
			passengers := detailInt(it.Details, "passengers")
			if passengers < 1 {
				passengers = 1
			}
			if km > 0 {
				est.Kilometres = km
				est.KgCO2e = (0.171 * km) / float64(passengers)
			}
		case models.TripItemTypeHotel:
			est.Nights = nights
			est.KgCO2e = 12 * float64(nights)
		case models.TripItemTypeListing:
			est.Nights = nights
			est.KgCO2e = 8 * float64(nights)
		}
		if est.KgCO2e > 0 {
			out.Items = append(out.Items, est)
			out.TotalKg += est.KgCO2e
		}
	}

	out.Equivalents = Equivalents{
		KmInAverageCar:  int(math.Round(out.TotalKg / 0.171)),
		MonthsAvgPerson: int(math.Round(out.TotalKg / (4000.0 / 12))),
		TreesForOneYear: int(math.Round(out.TotalKg / 22)),
	}
	out.TotalKg = round1(out.TotalKg)
	return out
}

func detailFloat(m map[string]any, k string) float64 {
	if m == nil {
		return 0
	}
	switch v := m[k].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int32:
		return float64(v)
	case int64:
		return float64(v)
	}
	return 0
}

func detailInt(m map[string]any, k string) int {
	return int(detailFloat(m, k))
}

// distanceFromDetails pulls from_/to_ lat/lng out of a flight item's Details
// map and returns the great-circle distance in km. Returns 0 if any field is
// missing.
func distanceFromDetails(m map[string]any) float64 {
	fromLat := detailFloat(m, "from_lat")
	fromLng := detailFloat(m, "from_lng")
	toLat := detailFloat(m, "to_lat")
	toLng := detailFloat(m, "to_lng")
	if fromLat == 0 && fromLng == 0 && toLat == 0 && toLng == 0 {
		return 0
	}
	return haversineKm(fromLat, fromLng, toLat, toLng)
}

// haversineKm is the standard great-circle distance formula.
func haversineKm(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371.0
	rad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := rad(lat2 - lat1)
	dLng := rad(lng2 - lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(rad(lat1))*math.Cos(rad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	c := 2 * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
	return R * c
}

func flightEmissionsKg(km float64) float64 {
	if km < 1500 {
		return 0.255 * km
	}
	return 0.195 * km
}
