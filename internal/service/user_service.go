package service

import (
	"context"

	"github.com/realestayer/v3/internal/models"
	"github.com/realestayer/v3/internal/repository"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

// UserService handles user-related business logic
type UserService struct {
	userRepo *repository.UserRepository
}

// NewUserService creates a new user service
func NewUserService(userRepo *repository.UserRepository) *UserService {
	return &UserService{
		userRepo: userRepo,
	}
}

// GetByID retrieves a user by ID
func (s *UserService) GetByID(ctx context.Context, id string) (*models.User, error) {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, repository.ErrUserNotFound
	}
	return s.userRepo.FindByID(ctx, objID)
}

// Update updates a user's profile
func (s *UserService) Update(ctx context.Context, id string, req models.UpdateUserRequest) (*models.User, error) {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return nil, repository.ErrUserNotFound
	}

	user, err := s.userRepo.FindByID(ctx, objID)
	if err != nil {
		return nil, err
	}

	if req.Name != "" {
		user.Name = req.Name
	}
	if req.AvatarURL != "" {
		user.AvatarURL = req.AvatarURL
	}
	if req.Preferences != nil {
		user.Preferences = *req.Preferences
	}

	if err := s.userRepo.Update(ctx, user); err != nil {
		return nil, err
	}

	return user, nil
}

// List returns paginated users (admin only)
func (s *UserService) List(ctx context.Context, page, limit int) ([]models.User, int64, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return s.userRepo.List(ctx, page, limit)
}

// GetStats returns user statistics
func (s *UserService) GetStats(ctx context.Context) (map[string]interface{}, error) {
	count, err := s.userRepo.Count(ctx)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"total_users": count,
	}, nil
}

// UpdateRole updates a user's role (admin only)
func (s *UserService) UpdateRole(ctx context.Context, id string, role string) error {
	objID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		return repository.ErrUserNotFound
	}
	return s.userRepo.UpdateRole(ctx, objID, role)
}
