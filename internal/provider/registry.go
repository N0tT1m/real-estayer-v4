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
	mu              sync.RWMutex
	flightProviders map[string]FlightProvider
	defaultFlight   string
}

// NewRegistry creates a new provider registry
func NewRegistry() *Registry {
	return &Registry{
		flightProviders: make(map[string]FlightProvider),
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
