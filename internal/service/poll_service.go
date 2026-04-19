package service

import (
	"context"
	"crypto/rand"
	"encoding/base32"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/realestayer/v4/internal/models"
	"github.com/realestayer/v4/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// PollService wraps the availability-poll repository and handles slug
// generation, access checks, and result tallying.
type PollService struct {
	repo  *repository.PollRepository
	users *repository.UserRepository
}

func NewPollService(repo *repository.PollRepository, users *repository.UserRepository) *PollService {
	return &PollService{repo: repo, users: users}
}

// CreatePollInput is the handler payload.
type CreatePollInput struct {
	Title       string
	Description string
	Options     []models.PollOption
	TripID      string // hex, optional
	ClosesAt    *time.Time
}

func (s *PollService) Create(ctx context.Context, userIDHex string, in CreatePollInput) (*models.AvailabilityPoll, error) {
	uid, err := primitive.ObjectIDFromHex(userIDHex)
	if err != nil {
		return nil, errors.New("invalid user id")
	}
	if strings.TrimSpace(in.Title) == "" {
		return nil, errors.New("title required")
	}
	if len(in.Options) == 0 {
		return nil, errors.New("at least one option required")
	}
	slug, err := randomPollSlug()
	if err != nil {
		return nil, err
	}
	poll := &models.AvailabilityPoll{
		CreatedBy:   uid,
		Title:       strings.TrimSpace(in.Title),
		Description: strings.TrimSpace(in.Description),
		Options:     in.Options,
		Slug:        slug,
		ClosesAt:    in.ClosesAt,
	}
	if in.TripID != "" {
		tripOID, err := primitive.ObjectIDFromHex(in.TripID)
		if err != nil {
			return nil, errors.New("invalid trip id")
		}
		poll.TripID = &tripOID
	}
	if err := s.repo.Create(ctx, poll); err != nil {
		return nil, err
	}
	return poll, nil
}

func (s *PollService) GetBySlug(ctx context.Context, slug string) (*models.AvailabilityPoll, error) {
	return s.repo.FindBySlug(ctx, slug)
}

// VoteInput captures a single participant's vote payload.
type VoteInput struct {
	DisplayName string
	Email       string
	UserID      string // optional hex
	OptionVotes map[string]string
	Note        string
}

func (s *PollService) Vote(ctx context.Context, slug string, in VoteInput) (*models.AvailabilityPoll, error) {
	poll, err := s.repo.FindBySlug(ctx, slug)
	if err != nil {
		return nil, err
	}
	if poll.ClosesAt != nil && time.Now().After(*poll.ClosesAt) {
		return nil, errors.New("this poll is closed")
	}
	if in.DisplayName == "" {
		return nil, errors.New("display name required")
	}
	// Validate that every vote references a real option and uses an allowed
	// value.
	valid := map[string]struct{}{}
	for _, o := range poll.Options {
		valid[o.ID.Hex()] = struct{}{}
	}
	cleaned := make(map[string]string, len(in.OptionVotes))
	for optID, vote := range in.OptionVotes {
		if _, ok := valid[optID]; !ok {
			continue
		}
		vote = strings.ToLower(strings.TrimSpace(vote))
		switch vote {
		case "yes", "maybe", "no":
			cleaned[optID] = vote
		}
	}

	resp := models.PollResponse{
		DisplayName: strings.TrimSpace(in.DisplayName),
		Email:       strings.ToLower(strings.TrimSpace(in.Email)),
		OptionVotes: cleaned,
		Note:        strings.TrimSpace(in.Note),
	}
	if in.UserID != "" {
		oid, err := primitive.ObjectIDFromHex(in.UserID)
		if err == nil {
			resp.UserID = &oid
		}
	}
	if err := s.repo.UpsertResponse(ctx, slug, resp); err != nil {
		return nil, err
	}
	return s.repo.FindBySlug(ctx, slug)
}

// Summarize tallies the current responses per option and returns a ranked
// slice (highest-scoring option first). Score = #yes + 0.5·#maybe.
func (s *PollService) Summarize(poll *models.AvailabilityPoll) []models.PollSummary {
	out := make([]models.PollSummary, 0, len(poll.Options))
	for _, opt := range poll.Options {
		sum := models.PollSummary{Option: opt}
		for _, resp := range poll.Responses {
			switch resp.OptionVotes[opt.ID.Hex()] {
			case "yes":
				sum.Yes++
			case "maybe":
				sum.Maybe++
			case "no":
				sum.No++
			}
		}
		sum.Score = float64(sum.Yes) + 0.5*float64(sum.Maybe)
		out = append(out, sum)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

func (s *PollService) ListForTrip(ctx context.Context, tripIDHex string) ([]models.AvailabilityPoll, error) {
	tid, err := primitive.ObjectIDFromHex(tripIDHex)
	if err != nil {
		return nil, errors.New("invalid trip id")
	}
	return s.repo.ListForTrip(ctx, tid)
}

func (s *PollService) ListForUser(ctx context.Context, userIDHex string) ([]models.AvailabilityPoll, error) {
	uid, err := primitive.ObjectIDFromHex(userIDHex)
	if err != nil {
		return nil, errors.New("invalid user id")
	}
	return s.repo.ListForUser(ctx, uid)
}

func (s *PollService) Delete(ctx context.Context, ownerIDHex, pollIDHex string) error {
	owner, err := primitive.ObjectIDFromHex(ownerIDHex)
	if err != nil {
		return errors.New("invalid user id")
	}
	id, err := primitive.ObjectIDFromHex(pollIDHex)
	if err != nil {
		return errors.New("invalid poll id")
	}
	return s.repo.Delete(ctx, owner, id)
}

func randomPollSlug() (string, error) {
	raw := make([]byte, 10)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)), nil
}
