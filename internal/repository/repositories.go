package repository

import (
	"github.com/realestayer/v3/internal/database"
)

// Repositories holds all repository instances
type Repositories struct {
	User      *UserRepository
	Session   *SessionRepository
	Listing   *ListingRepository
	Booking   *BookingRepository
	Trip      *TripRepository
	Watchlist *WatchlistRepository
}

// NewRepositories creates all repositories
func NewRepositories(db *database.DB) *Repositories {
	return &Repositories{
		User:      NewUserRepository(db),
		Session:   NewSessionRepository(db),
		Listing:   NewListingRepository(db),
		Booking:   NewBookingRepository(db),
		Trip:      NewTripRepository(db),
		Watchlist: NewWatchlistRepository(db),
	}
}
