package amadeus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/realestayer/v4/internal/models"
)

// Hotel API response types

type hotelListResponse struct {
	Data []hotelData `json:"data"`
}

type hotelData struct {
	ChainCode string `json:"chainCode"`
	IataCode  string `json:"iataCode"`
	DupeId    int    `json:"dupeId"`
	Name      string `json:"name"`
	HotelId   string `json:"hotelId"`
	GeoCode   struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	} `json:"geoCode"`
	Address struct {
		CountryCode string `json:"countryCode"`
	} `json:"address"`
}

type hotelOffersResponse struct {
	Data []hotelOfferData `json:"data"`
}

type hotelOfferData struct {
	Type      string    `json:"type"`
	Hotel     hotelInfo `json:"hotel"`
	Available bool      `json:"available"`
	Offers    []offer   `json:"offers"`
}

type hotelInfo struct {
	Type      string `json:"type"`
	HotelId   string `json:"hotelId"`
	ChainCode string `json:"chainCode"`
	Name      string `json:"name"`
	Rating    string `json:"rating"`
	CityCode  string `json:"cityCode"`
	Latitude  float64 `json:"latitude"`
	Longitude float64 `json:"longitude"`
}

type offer struct {
	Id        string `json:"id"`
	CheckInDate  string `json:"checkInDate"`
	CheckOutDate string `json:"checkOutDate"`
	RoomQuantity int    `json:"roomQuantity"`
	RateCode  string `json:"rateCode"`
	Room      struct {
		Type         string `json:"type"`
		TypeEstimated struct {
			Category string `json:"category"`
			Beds     int    `json:"beds"`
			BedType  string `json:"bedType"`
		} `json:"typeEstimated"`
		Description struct {
			Text string `json:"text"`
		} `json:"description"`
	} `json:"room"`
	Guests struct {
		Adults int `json:"adults"`
	} `json:"guests"`
	Price struct {
		Currency string `json:"currency"`
		Base     string `json:"base"`
		Total    string `json:"total"`
	} `json:"price"`
	Policies struct {
		Cancellation struct {
			Description struct {
				Text string `json:"text"`
			} `json:"description"`
		} `json:"cancellation"`
		PaymentType string `json:"paymentType"`
	} `json:"policies"`
}

// SearchHotels searches for available hotels
func (c *Client) SearchHotels(ctx context.Context, req models.HotelSearchRequest) ([]models.HotelOffer, error) {
	// Step 1: Get hotel IDs by city
	hotelIDs, err := c.getHotelsByCity(ctx, req.CityCode, req.Radius, req.StarRatings, req.ChainCodes)
	if err != nil {
		return nil, fmt.Errorf("failed to get hotels by city: %w", err)
	}

	if len(hotelIDs) == 0 {
		return []models.HotelOffer{}, nil
	}

	// Limit to first 50 hotels
	if len(hotelIDs) > 50 {
		hotelIDs = hotelIDs[:50]
	}

	// Step 2: Get offers for these hotels
	params := url.Values{}
	for _, id := range hotelIDs {
		params.Add("hotelIds", id)
	}
	params.Set("adults", strconv.Itoa(req.Adults))
	params.Set("checkInDate", req.CheckIn.Format("2006-01-02"))
	params.Set("checkOutDate", req.CheckOut.Format("2006-01-02"))
	if req.Rooms > 0 {
		params.Set("roomQuantity", strconv.Itoa(req.Rooms))
	}
	if req.Currency != "" {
		params.Set("currency", req.Currency)
	}

	resp, err := c.get(ctx, "/v3/shopping/hotel-offers?"+params.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var result hotelOffersResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return c.convertHotelOffers(result.Data), nil
}

// getHotelsByCity retrieves hotel IDs in a city
func (c *Client) getHotelsByCity(ctx context.Context, cityCode string, radius int, ratings []int, chainCodes []string) ([]string, error) {
	params := url.Values{}
	params.Set("cityCode", cityCode)
	if radius > 0 {
		params.Set("radius", strconv.Itoa(radius))
		params.Set("radiusUnit", "KM")
	}
	for _, r := range ratings {
		params.Add("ratings", strconv.Itoa(r))
	}
	for _, cc := range chainCodes {
		params.Add("chainCodes", cc)
	}

	resp, err := c.get(ctx, "/v1/reference-data/locations/hotels/by-city?"+params.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var result hotelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	ids := make([]string, len(result.Data))
	for i, h := range result.Data {
		ids[i] = h.HotelId
	}

	return ids, nil
}

// GetHotelDetails retrieves detailed hotel information
func (c *Client) GetHotelDetails(ctx context.Context, hotelID string) (*models.HotelInfo, error) {
	params := url.Values{}
	params.Set("hotelIds", hotelID)

	resp, err := c.get(ctx, "/v1/reference-data/locations/hotels/by-hotels?"+params.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var result hotelListResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("hotel not found: %s", hotelID)
	}

	h := result.Data[0]
	return &models.HotelInfo{
		HotelID:   h.HotelId,
		Name:      h.Name,
		ChainCode: h.ChainCode,
		Coordinates: &models.Coordinates{
			Lat: h.GeoCode.Latitude,
			Lng: h.GeoCode.Longitude,
		},
	}, nil
}

// GetRoomAvailability gets available rooms for a hotel
func (c *Client) GetRoomAvailability(ctx context.Context, hotelID string, checkIn, checkOut string, guests int) ([]models.RoomOffer, error) {
	params := url.Values{}
	params.Set("hotelIds", hotelID)
	params.Set("adults", strconv.Itoa(guests))
	params.Set("checkInDate", checkIn)
	params.Set("checkOutDate", checkOut)

	resp, err := c.get(ctx, "/v3/shopping/hotel-offers?"+params.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var result hotelOffersResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(result.Data) == 0 || len(result.Data[0].Offers) == 0 {
		return []models.RoomOffer{}, nil
	}

	return convertRoomOffers(result.Data[0].Offers), nil
}

// BookHotel creates a hotel booking
func (c *Client) BookHotel(ctx context.Context, req models.HotelBookingRequest) (*models.BookingConfirmation, error) {
	guests := make([]map[string]interface{}, len(req.Guests))
	for i, g := range req.Guests {
		guests[i] = map[string]interface{}{
			"tid":       i + 1,
			"title":     g.Title,
			"firstName": g.FirstName,
			"lastName":  g.LastName,
			"phone":     g.Phone,
			"email":     g.Email,
		}
	}

	bookingReq := map[string]interface{}{
		"data": map[string]interface{}{
			"type":   "hotel-order",
			"guests": guests,
			"travelAgent": map[string]interface{}{
				"contact": map[string]string{
					"email": req.Contact.Email,
				},
			},
			"roomAssociations": []map[string]interface{}{
				{
					"guestReferences": []map[string]string{
						{"guestReference": "1"},
					},
					"hotelOfferId": req.OfferID,
				},
			},
			"payment": map[string]interface{}{
				"method": "CREDIT_CARD",
				"paymentCard": map[string]interface{}{
					"paymentCardInfo": map[string]string{
						"vendorCode": req.PaymentInfo.CardType,
						"cardNumber": req.PaymentInfo.CardNumber,
						"expiryDate": req.PaymentInfo.ExpiryYear + "-" + req.PaymentInfo.ExpiryMonth,
						"holderName": req.PaymentInfo.CardholderName,
					},
				},
			},
		},
	}

	resp, err := c.post(ctx, "/v2/booking/hotel-orders", bookingReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var result struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode booking response: %w", err)
	}

	return &models.BookingConfirmation{
		Provider:    "amadeus",
		Reference:   result.Data.ID,
		Status:      "confirmed",
		ConfirmedAt: time.Now(),
	}, nil
}

// SearchCities searches for cities by keyword
func (c *Client) SearchCities(ctx context.Context, keyword string) ([]models.City, error) {
	params := url.Values{}
	params.Set("subType", "CITY")
	params.Set("keyword", keyword)
	params.Set("page[limit]", "10")

	resp, err := c.get(ctx, "/v1/reference-data/locations?"+params.Encode())
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, parseError(resp)
	}

	var result struct {
		Data []struct {
			IataCode string `json:"iataCode"`
			Name     string `json:"name"`
			Address  struct {
				CountryName string `json:"countryName"`
			} `json:"address"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	cities := make([]models.City, len(result.Data))
	for i, c := range result.Data {
		cities[i] = models.City{
			Code:    c.IataCode,
			Name:    c.Name,
			Country: c.Address.CountryName,
		}
	}

	return cities, nil
}

// convertHotelOffers converts Amadeus response to our model
func (c *Client) convertHotelOffers(data []hotelOfferData) []models.HotelOffer {
	offers := make([]models.HotelOffer, 0, len(data))

	for _, d := range data {
		if !d.Available || len(d.Offers) == 0 {
			continue
		}

		rating, _ := strconv.Atoi(d.Hotel.Rating)

		offer := models.HotelOffer{
			ID:       d.Hotel.HotelId,
			Provider: "amadeus",
			Hotel: models.HotelInfo{
				HotelID:   d.Hotel.HotelId,
				Name:      d.Hotel.Name,
				ChainCode: d.Hotel.ChainCode,
				Rating:    rating,
				Coordinates: &models.Coordinates{
					Lat: d.Hotel.Latitude,
					Lng: d.Hotel.Longitude,
				},
			},
			Offers:    convertRoomOffers(d.Offers),
			Available: d.Available,
		}

		offers = append(offers, offer)
	}

	return offers
}

func convertRoomOffers(data []offer) []models.RoomOffer {
	rooms := make([]models.RoomOffer, len(data))

	for i, o := range data {
		total, _ := strconv.ParseFloat(o.Price.Total, 64)
		base, _ := strconv.ParseFloat(o.Price.Base, 64)

		rooms[i] = models.RoomOffer{
			ID:          o.Id,
			RoomType:    o.Room.TypeEstimated.Category,
			Description: o.Room.Description.Text,
			BedType:     o.Room.TypeEstimated.BedType,
			Guests:      o.Guests.Adults,
			Price: models.Price{
				Base:     base,
				Total:    total,
				Currency: o.Price.Currency,
			},
			CancellationPolicy: o.Policies.Cancellation.Description.Text,
			PaymentType:        o.Policies.PaymentType,
		}
	}

	return rooms
}
