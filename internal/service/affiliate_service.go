package service

import (
	"fmt"
	"net/url"
	"strings"
	"time"
)

// AffiliateService builds deep-links to well-known metasearch and booking
// sites. No API calls — just URL construction — but centralising it means
// trackers (utm_source, partner IDs) can be swapped in one place if we
// later sign up for affiliate programs.
type AffiliateService struct {
	UTMSource string
}

func NewAffiliateService(utmSource string) *AffiliateService {
	if utmSource == "" {
		utmSource = "realestayer"
	}
	return &AffiliateService{UTMSource: utmSource}
}

// QuickLinks is a small bundle used on destination and trip pages.
type QuickLinks struct {
	Stays      []QuickLink `json:"stays"`
	Flights    []QuickLink `json:"flights"`
	Activities []QuickLink `json:"activities"`
	Cars       []QuickLink `json:"cars"`
	Trains     []QuickLink `json:"trains"`
	Info       []QuickLink `json:"info"`
}

type QuickLink struct {
	Provider string `json:"provider"`
	Label    string `json:"label"`
	URL      string `json:"url"`
}

// ForDestination returns quick-search links targeted at a destination
// name. Dates are optional — passing them in tightens the initial search.
func (s *AffiliateService) ForDestination(destination string, checkIn, checkOut *time.Time, origin string) QuickLinks {
	dest := strings.TrimSpace(destination)
	q := url.QueryEscape(dest)

	in, out := "", ""
	if checkIn != nil {
		in = checkIn.Format("2006-01-02")
	}
	if checkOut != nil {
		out = checkOut.Format("2006-01-02")
	}

	stays := []QuickLink{
		{"Booking.com", "Browse hotels on Booking.com", fmt.Sprintf("https://www.booking.com/searchresults.html?ss=%s&checkin=%s&checkout=%s&utm_source=%s", q, in, out, s.UTMSource)},
		{"Airbnb", "Search stays on Airbnb", fmt.Sprintf("https://www.airbnb.com/s/%s/homes%s%s", q, qsDate("checkin", in), qsDate("checkout", out))},
		{"Hostelworld", "Hostels on Hostelworld", fmt.Sprintf("https://www.hostelworld.com/findabed.php?search_keywords=%s", q)},
		{"VRBO", "Rentals on VRBO", fmt.Sprintf("https://www.vrbo.com/search?destination=%s", q)},
	}
	flights := []QuickLink{
		{"Skyscanner", "Flights via Skyscanner", fmt.Sprintf("https://www.skyscanner.com/transport/flights/%s/%s/", safeLower(origin), safeLower(dest))},
		{"Google Flights", "Search Google Flights", fmt.Sprintf("https://www.google.com/travel/flights?q=Flights+to+%s", q)},
		{"Kiwi", "Browse Kiwi.com", fmt.Sprintf("https://www.kiwi.com/en/search/results/%s/anywhere/anytime/anytime", q)},
		{"Kayak", "Search Kayak", fmt.Sprintf("https://www.kayak.com/flights/%s-%s", safeLower(origin), safeLower(dest))},
	}
	activities := []QuickLink{
		{"GetYourGuide", "Tours via GetYourGuide", fmt.Sprintf("https://www.getyourguide.com/s/?q=%s&partner_id=%s", q, s.UTMSource)},
		{"Viator", "Experiences on Viator", fmt.Sprintf("https://www.viator.com/searchResults/all?text=%s", q)},
		{"Klook", "Activities on Klook", fmt.Sprintf("https://www.klook.com/en-US/search/result/?query=%s", q)},
		{"Tiqets", "Tickets on Tiqets", fmt.Sprintf("https://www.tiqets.com/en/search?q=%s", q)},
	}
	cars := []QuickLink{
		{"Rentalcars", "Compare on Rentalcars", fmt.Sprintf("https://www.rentalcars.com/SearchResults.do?city=%s", q)},
		{"Discover Cars", "Discover Cars", fmt.Sprintf("https://www.discovercars.com/?pickup=%s", q)},
		{"Turo", "Rent local on Turo", fmt.Sprintf("https://turo.com/us/en/search?location=%s", q)},
	}
	trains := []QuickLink{
		{"Omio", "Trains / buses via Omio", fmt.Sprintf("https://www.omio.com/search-frontend/?destinationId=&destination=%s", q)},
		{"Trainline", "Europe trains on Trainline", fmt.Sprintf("https://www.thetrainline.com/en/book/search?destination=%s", q)},
		{"Rome2Rio", "How to get there — Rome2Rio", fmt.Sprintf("https://www.rome2rio.com/s/%s", q)},
	}
	info := []QuickLink{
		{"Wikivoyage", "Travel guide on Wikivoyage", fmt.Sprintf("https://en.wikivoyage.org/wiki/%s", strings.ReplaceAll(dest, " ", "_"))},
		{"Wikipedia", "Wikipedia article", fmt.Sprintf("https://en.wikipedia.org/wiki/%s", strings.ReplaceAll(dest, " ", "_"))},
		{"OpenStreetMap", "See the map on OSM", fmt.Sprintf("https://www.openstreetmap.org/search?query=%s", q)},
		{"U.S. State Dept", "Travel advisory (US State Dept)", "https://travel.state.gov/content/travel/en/traveladvisories/traveladvisories.html"},
		{"UK FCDO", "Travel advice (UK FCDO)", "https://www.gov.uk/foreign-travel-advice"},
	}
	return QuickLinks{Stays: stays, Flights: flights, Activities: activities, Cars: cars, Trains: trains, Info: info}
}

func qsDate(key, v string) string {
	if v == "" {
		return ""
	}
	return "&" + key + "=" + v
}

func safeLower(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "anywhere"
	}
	return strings.ToLower(strings.ReplaceAll(s, " ", "-"))
}
