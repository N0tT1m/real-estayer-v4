package service

import (
	"strings"

	"github.com/realestayer/v4/internal/models"
)

// SuggestPackingList builds a starter packing list tailored to the trip.
// Nights drive quantities; destinations/categories hint at climate; item
// types on the itinerary add context (e.g. a flight ⇒ "Passport / ID").
//
// The function is deliberately deterministic so two users planning similar
// trips see similar starter lists and can diff easily.
func SuggestPackingList(trip *models.Trip) []models.PackingItem {
	if trip == nil {
		return nil
	}
	nights := daysBetween(trip)
	out := []models.PackingItem{
		pack("Essentials", "Phone + charger", 1),
		pack("Essentials", "Wallet", 1),
		pack("Essentials", "Keys", 1),
		pack("Documents", "ID or driver's license", 1),
		pack("Tech", "Headphones", 1),
		pack("Tech", "Battery pack", 1),
		pack("Toiletries", "Toothbrush + paste", 1),
		pack("Toiletries", "Deodorant", 1),
	}
	// Clothing scaled to nights (clamped to 10 so long trips don't propose 30 t-shirts).
	undergarments := nights + 1
	if undergarments > 10 {
		undergarments = 10
	}
	tshirts := nights
	if tshirts > 8 {
		tshirts = 8
	}
	if tshirts < 2 {
		tshirts = 2
	}
	out = append(out,
		pack("Clothing", "Underwear", undergarments),
		pack("Clothing", "Socks", undergarments),
		pack("Clothing", "T-shirts / tops", tshirts),
		pack("Clothing", "Pants / shorts", max(2, nights/3)),
		pack("Clothing", "Sleepwear", 1),
	)

	climate := climateHints(trip)
	if climate.warm {
		out = append(out,
			pack("Clothing", "Sunglasses", 1),
			pack("Toiletries", "Sunscreen", 1),
		)
	}
	if climate.cold {
		out = append(out,
			pack("Clothing", "Warm jacket", 1),
			pack("Clothing", "Gloves + hat", 1),
			pack("Clothing", "Thermal base layer", 1),
		)
	}
	if climate.rainy {
		out = append(out,
			pack("Clothing", "Rain jacket / umbrella", 1),
		)
	}
	if climate.beach {
		out = append(out,
			pack("Clothing", "Swimwear", 2),
			pack("Beach", "Flip-flops", 1),
			pack("Beach", "Beach towel", 1),
		)
	}
	if climate.hike {
		out = append(out,
			pack("Outdoor", "Hiking boots", 1),
			pack("Outdoor", "Daypack", 1),
			pack("Outdoor", "Reusable water bottle", 1),
		)
	}

	// Item-type rules.
	hasFlight := false
	hasCar := false
	for _, it := range trip.Items {
		switch it.Type {
		case models.TripItemTypeFlight:
			hasFlight = true
		case models.TripItemTypeCar:
			hasCar = true
		}
	}
	if hasFlight {
		out = append(out,
			pack("Documents", "Passport", 1),
			pack("Documents", "Boarding pass / confirmation", 1),
			pack("Tech", "Universal travel adapter", 1),
		)
	}
	if hasCar {
		out = append(out,
			pack("Documents", "Driver's license", 1),
			pack("Car", "Rental confirmation", 1),
		)
	}

	// Mark everything auto-suggested so users can prune them easily.
	for i := range out {
		out[i].AutoSuggested = true
	}
	return out
}

// SuggestChecklist returns pre-trip to-dos tailored to item types.
func SuggestChecklist(trip *models.Trip) []models.ChecklistItem {
	if trip == nil {
		return nil
	}
	out := []models.ChecklistItem{
		{Title: "Confirm trip dates with travel companions"},
		{Title: "Check passport validity (6+ months past return date)"},
		{Title: "Buy travel insurance"},
	}
	hasFlight := false
	hasHotel := false
	hasInternational := false
	for _, it := range trip.Items {
		switch it.Type {
		case models.TripItemTypeFlight:
			hasFlight = true
			hasInternational = true // assume most flights are worth the check
		case models.TripItemTypeHotel, models.TripItemTypeListing:
			hasHotel = true
		}
	}
	if hasFlight {
		out = append(out,
			models.ChecklistItem{Title: "Check-in online 24h before flight"},
			models.ChecklistItem{Title: "Arrange airport transfer"},
		)
	}
	if hasHotel {
		out = append(out, models.ChecklistItem{Title: "Save booking confirmation offline"})
	}
	if hasInternational {
		out = append(out,
			models.ChecklistItem{Title: "Check visa / ESTA / eTA requirements"},
			models.ChecklistItem{Title: "Notify bank of travel dates"},
			models.ChecklistItem{Title: "Download offline maps for destination"},
		)
	}
	out = append(out,
		models.ChecklistItem{Title: "Arrange pet/plant care"},
		models.ChecklistItem{Title: "Forward mail / pause deliveries"},
	)
	for i := range out {
		out[i].AutoSuggested = true
	}
	return out
}

type climateSignal struct {
	warm, cold, rainy, beach, hike bool
}

func climateHints(trip *models.Trip) climateSignal {
	// Very rough — look at destination categories we already store.
	c := climateSignal{}
	// Month-based (northern hemisphere bias; good enough as a starter).
	m := trip.StartDate.Month()
	switch {
	case m >= 6 && m <= 8:
		c.warm = true
	case m == 12, m <= 2:
		c.cold = true
	case m >= 3 && m <= 5, m >= 9 && m <= 11:
		c.rainy = true
	}
	for _, d := range trip.Destinations {
		name := strings.ToLower(d.Name)
		if strings.Contains(name, "beach") || strings.Contains(name, "bali") || strings.Contains(name, "hawaii") || strings.Contains(name, "miami") {
			c.beach = true
			c.warm = true
		}
		if strings.Contains(name, "alps") || strings.Contains(name, "iceland") || strings.Contains(name, "norway") {
			c.cold = true
		}
		if strings.Contains(name, "park") || strings.Contains(name, "patagonia") {
			c.hike = true
		}
	}
	return c
}

func pack(cat, name string, qty int) models.PackingItem {
	if qty < 1 {
		qty = 1
	}
	return models.PackingItem{Category: cat, Name: name, Quantity: qty}
}

func daysBetween(trip *models.Trip) int {
	if trip.EndDate.Before(trip.StartDate) {
		return 1
	}
	days := int(trip.EndDate.Sub(trip.StartDate).Hours()/24) + 1
	if days < 1 {
		return 1
	}
	return days
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
