package provider

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/realestayer/v4/internal/models"
)

// Registry manages multiple booking providers
type Registry struct {
	mu             sync.RWMutex
	flightProviders map[string]FlightProvider
	hotelProviders  map[string]HotelProvider
	carProviders    map[string]CarProvider
	defaultFlight   string
	defaultHotel    string
	defaultCar      string
}

// NewRegistry creates a new provider registry
func NewRegistry() *Registry {
	return &Registry{
		flightProviders: make(map[string]FlightProvider),
		hotelProviders:  make(map[string]HotelProvider),
		carProviders:    make(map[string]CarProvider),
	}
}

// RegisterFlight adds a flight provider
func (r *Registry) RegisterFlight(name string, provider FlightProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.flightProviders[name] = provider
	if r.defaultFlight == "" {
		r.defaultFlight = name
	}
}

// RegisterHotel adds a hotel provider
func (r *Registry) RegisterHotel(name string, provider HotelProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.hotelProviders[name] = provider
	if r.defaultHotel == "" {
		r.defaultHotel = name
	}
}

// RegisterCar adds a car rental provider
func (r *Registry) RegisterCar(name string, provider CarProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.carProviders[name] = provider
	if r.defaultCar == "" {
		r.defaultCar = name
	}
}

// GetFlightProvider returns a flight provider by name
func (r *Registry) GetFlightProvider(name string) (FlightProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if name == "" {
		name = r.defaultFlight
	}

	provider, ok := r.flightProviders[name]
	if !ok {
		return nil, fmt.Errorf("flight provider not found: %s", name)
	}
	return provider, nil
}

// GetHotelProvider returns a hotel provider by name
func (r *Registry) GetHotelProvider(name string) (HotelProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if name == "" {
		name = r.defaultHotel
	}

	provider, ok := r.hotelProviders[name]
	if !ok {
		return nil, fmt.Errorf("hotel provider not found: %s", name)
	}
	return provider, nil
}

// GetCarProvider returns a car provider by name
func (r *Registry) GetCarProvider(name string) (CarProvider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if name == "" {
		name = r.defaultCar
	}

	provider, ok := r.carProviders[name]
	if !ok {
		return nil, fmt.Errorf("car provider not found: %s", name)
	}
	return provider, nil
}

// SearchFlightsAll searches across all flight providers. Individual provider
// failures are logged and returned alongside any collected offers so the caller
// can decide whether to surface a partial-result warning. An error is only
// returned when every registered provider failed.
func (r *Registry) SearchFlightsAll(ctx context.Context, req models.FlightSearchRequest) ([]models.FlightOffer, error) {
	r.mu.RLock()
	providers := make(map[string]FlightProvider, len(r.flightProviders))
	for name, p := range r.flightProviders {
		providers[name] = p
	}
	r.mu.RUnlock()

	var allOffers []models.FlightOffer
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failures int

	for name, provider := range providers {
		wg.Add(1)
		go func(name string, p FlightProvider) {
			defer wg.Done()
			offers, err := p.SearchFlights(ctx, req)
			if err != nil {
				slog.Warn("flight provider failed", "provider", name, "error", err)
				mu.Lock()
				failures++
				mu.Unlock()
				return
			}
			mu.Lock()
			allOffers = append(allOffers, offers...)
			mu.Unlock()
		}(name, provider)
	}

	wg.Wait()
	if len(providers) > 0 && failures == len(providers) {
		return allOffers, fmt.Errorf("all %d flight providers failed", failures)
	}
	return allOffers, nil
}

// SearchHotelsAll searches across all hotel providers (see SearchFlightsAll).
func (r *Registry) SearchHotelsAll(ctx context.Context, req models.HotelSearchRequest) ([]models.HotelOffer, error) {
	r.mu.RLock()
	providers := make(map[string]HotelProvider, len(r.hotelProviders))
	for name, p := range r.hotelProviders {
		providers[name] = p
	}
	r.mu.RUnlock()

	var allOffers []models.HotelOffer
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failures int

	for name, provider := range providers {
		wg.Add(1)
		go func(name string, p HotelProvider) {
			defer wg.Done()
			offers, err := p.SearchHotels(ctx, req)
			if err != nil {
				slog.Warn("hotel provider failed", "provider", name, "error", err)
				mu.Lock()
				failures++
				mu.Unlock()
				return
			}
			mu.Lock()
			allOffers = append(allOffers, offers...)
			mu.Unlock()
		}(name, provider)
	}

	wg.Wait()
	if len(providers) > 0 && failures == len(providers) {
		return allOffers, fmt.Errorf("all %d hotel providers failed", failures)
	}
	return allOffers, nil
}

// SearchCarsAll searches across all car providers (see SearchFlightsAll).
func (r *Registry) SearchCarsAll(ctx context.Context, req models.CarSearchRequest) ([]models.CarOffer, error) {
	r.mu.RLock()
	providers := make(map[string]CarProvider, len(r.carProviders))
	for name, p := range r.carProviders {
		providers[name] = p
	}
	r.mu.RUnlock()

	var allOffers []models.CarOffer
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failures int

	for name, provider := range providers {
		wg.Add(1)
		go func(name string, p CarProvider) {
			defer wg.Done()
			offers, err := p.SearchCars(ctx, req)
			if err != nil {
				slog.Warn("car provider failed", "provider", name, "error", err)
				mu.Lock()
				failures++
				mu.Unlock()
				return
			}
			mu.Lock()
			allOffers = append(allOffers, offers...)
			mu.Unlock()
		}(name, provider)
	}

	wg.Wait()
	if len(providers) > 0 && failures == len(providers) {
		return allOffers, fmt.Errorf("all %d car providers failed", failures)
	}
	return allOffers, nil
}

// ListFlightProviders returns names of registered flight providers
func (r *Registry) ListFlightProviders() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.flightProviders))
	for name := range r.flightProviders {
		names = append(names, name)
	}
	return names
}

// ListHotelProviders returns names of registered hotel providers
func (r *Registry) ListHotelProviders() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.hotelProviders))
	for name := range r.hotelProviders {
		names = append(names, name)
	}
	return names
}

// ListCarProviders returns names of registered car providers
func (r *Registry) ListCarProviders() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.carProviders))
	for name := range r.carProviders {
		names = append(names, name)
	}
	return names
}
