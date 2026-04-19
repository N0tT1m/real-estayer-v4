package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

type TripJournalService struct {
	trips *TripService
	repo  *repository.TripJournalRepository
	users *repository.UserRepository
}

func NewTripJournalService(trips *TripService, repo *repository.TripJournalRepository, users *repository.UserRepository) *TripJournalService {
	return &TripJournalService{trips: trips, repo: repo, users: users}
}

func (s *TripJournalService) List(ctx context.Context, userID, tripID string) ([]models.JournalEntry, error) {
	trip, err := s.trips.GetByID(ctx, userID, tripID)
	if err != nil {
		return nil, err
	}
	return s.repo.ListForTrip(ctx, trip.ID)
}

type JournalInput struct {
	Title     string
	Body      string
	MediaURLs []string
	EntryDate *time.Time
}

func (s *TripJournalService) Add(ctx context.Context, userID, tripID string, in JournalInput) (*models.JournalEntry, error) {
	trip, err := s.trips.GetByID(ctx, userID, tripID)
	if err != nil {
		return nil, err
	}
	if !s.trips.CanEdit(userID, trip) {
		return nil, ErrTripAccessDenied
	}
	body := strings.TrimSpace(in.Body)
	if body == "" {
		return nil, errors.New("body cannot be empty")
	}
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errors.New("invalid user id")
	}
	name := ""
	if u, err := s.users.FindByID(ctx, oid); err == nil && u != nil {
		name = u.Name
	}
	when := time.Now()
	if in.EntryDate != nil {
		when = *in.EntryDate
	}
	urls := make([]string, 0, len(in.MediaURLs))
	for _, u := range in.MediaURLs {
		u = strings.TrimSpace(u)
		if strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://") {
			urls = append(urls, u)
		}
	}
	entry := &models.JournalEntry{
		TripID:     trip.ID,
		UserID:     oid,
		AuthorName: name,
		Title:      strings.TrimSpace(in.Title),
		Body:       body,
		MediaURLs:  urls,
		EntryDate:  when,
	}
	if err := s.repo.Create(ctx, entry); err != nil {
		return nil, err
	}
	return entry, nil
}

func (s *TripJournalService) Delete(ctx context.Context, userID, tripID, entryID string) error {
	trip, err := s.trips.GetByID(ctx, userID, tripID)
	if err != nil {
		return err
	}
	if !s.trips.CanEdit(userID, trip) {
		return ErrTripAccessDenied
	}
	id, err := primitive.ObjectIDFromHex(entryID)
	if err != nil {
		return errors.New("invalid entry id")
	}
	_ = trip
	return s.repo.Delete(ctx, id)
}
