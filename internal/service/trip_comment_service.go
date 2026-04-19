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

type TripCommentService struct {
	trips    *TripService
	comments *repository.TripCommentRepository
	users    *repository.UserRepository
}

func NewTripCommentService(trips *TripService, comments *repository.TripCommentRepository, users *repository.UserRepository) *TripCommentService {
	return &TripCommentService{trips: trips, comments: comments, users: users}
}

// List returns all comments on a trip. Any user with read access can see them.
func (s *TripCommentService) List(ctx context.Context, userID, tripID string) ([]models.TripComment, error) {
	trip, err := s.trips.GetByID(ctx, userID, tripID)
	if err != nil {
		return nil, err
	}
	return s.comments.ListForTrip(ctx, trip.ID)
}

// Add posts a new comment. Any reader can comment; editors aren't required.
func (s *TripCommentService) Add(ctx context.Context, userID, tripID, itemID, body string) (*models.TripComment, error) {
	trip, err := s.trips.GetByID(ctx, userID, tripID)
	if err != nil {
		return nil, err
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return nil, errors.New("comment cannot be empty")
	}
	if len(body) > 2000 {
		return nil, errors.New("comment too long")
	}
	oid, err := primitive.ObjectIDFromHex(userID)
	if err != nil {
		return nil, errors.New("invalid user id")
	}
	var itemOID primitive.ObjectID
	if itemID != "" {
		iid, err := primitive.ObjectIDFromHex(itemID)
		if err != nil {
			return nil, errors.New("invalid item id")
		}
		itemOID = iid
	}

	authorName := ""
	if u, err := s.users.FindByID(ctx, oid); err == nil && u != nil {
		authorName = u.Name
	}

	c := &models.TripComment{
		TripID:     trip.ID,
		ItemID:     itemOID,
		UserID:     oid,
		AuthorName: authorName,
		Body:       body,
		CreatedAt:  time.Now(),
	}
	if err := s.comments.Create(ctx, c); err != nil {
		return nil, err
	}
	return c, nil
}

// Delete removes a comment. Anyone can delete their own; trip owners can
// delete any comment.
func (s *TripCommentService) Delete(ctx context.Context, userID, tripID, commentID string) error {
	trip, err := s.trips.GetByID(ctx, userID, tripID)
	if err != nil {
		return err
	}
	id, err := primitive.ObjectIDFromHex(commentID)
	if err != nil {
		return errors.New("invalid comment id")
	}
	comments, err := s.comments.ListForTrip(ctx, trip.ID)
	if err != nil {
		return err
	}
	var target *models.TripComment
	for i, c := range comments {
		if c.ID == id {
			target = &comments[i]
			break
		}
	}
	if target == nil {
		return errors.New("comment not found")
	}
	if trip.UserID.Hex() != userID && target.UserID.Hex() != userID {
		return ErrTripAccessDenied
	}
	return s.comments.Delete(ctx, id)
}
