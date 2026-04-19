package repository

import (
	"github.com/realestayer/v4/internal/database"
)

// Repositories holds all repository instances
type Repositories struct {
	User          *UserRepository
	Session       *SessionRepository
	Listing       *ListingRepository
	Booking       *BookingRepository
	Trip          *TripRepository
	TripComment   *TripCommentRepository
	TripExpense   *TripExpenseRepository
	TripJournal   *TripJournalRepository
	TripReview    *TripReviewRepository
	Watchlist     *WatchlistRepository
	PasswordReset *PasswordResetRepository
	PriceHistory  *PriceHistoryRepository
	SavedSearch   *SavedSearchRepository
	Collection    *CollectionRepository
	Poll          *PollRepository
	Audit         *AuditRepository
}

// NewRepositories creates all repositories
func NewRepositories(db *database.DB) *Repositories {
	return &Repositories{
		User:          NewUserRepository(db),
		Session:       NewSessionRepository(db),
		Listing:       NewListingRepository(db),
		Booking:       NewBookingRepository(db),
		Trip:          NewTripRepository(db),
		TripComment:   NewTripCommentRepository(db),
		TripExpense:   NewTripExpenseRepository(db),
		TripJournal:   NewTripJournalRepository(db),
		TripReview:    NewTripReviewRepository(db),
		Watchlist:     NewWatchlistRepository(db),
		PasswordReset: NewPasswordResetRepository(db),
		PriceHistory:  NewPriceHistoryRepository(db),
		SavedSearch:   NewSavedSearchRepository(db),
		Collection:    NewCollectionRepository(db),
		Poll:          NewPollRepository(db),
		Audit:         NewAuditRepository(db),
	}
}
